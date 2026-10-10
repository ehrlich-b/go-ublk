package completion

import (
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"
)

func TestFetchTagsChecksBeforeDelivery(t *testing.T) {
	buf := make([]byte, 16*256)
	binary.LittleEndian.PutUint16(buf, 7)
	for _, tc := range []struct {
		name string
		bid  uint16
		res  int32
		has  bool
		bad  bool
	}{
		{"valid", 0, 2, true, false},
		{"last buffer", 15, 256, true, false},
		{"buffer ID", 16, 2, true, true},
		{"full buffer ID", 65535, 2, true, true},
		{"odd length", 0, 3, true, true},
		{"oversized length", 0, 258, true, true},
		{"missing buffer", 0, 2, false, true},
		{"terminal", 0, -125, false, false},
		{"empty selected buffer", 0, 0, true, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tags, err := FetchTags(buf, 16, 256, tc.bid, tc.res, tc.has)
			if (err != nil) != tc.bad {
				t.Fatalf("tags %v, error %v", tags, err)
			}
		})
	}
	if _, err := FetchTags(buf[:256], 16, 256, 0, 2, true); err == nil {
		t.Fatal("accepted a buffer layout outside its allocation")
	}
}

func submit(t testing.TB, b *Batch, slot int, tokens []Token) uint64 {
	t.Helper()
	for _, token := range tokens {
		if err := b.Queue(token); err != nil {
			t.Fatal(err)
		}
	}
	id, err := b.NextID(slot)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Submit(id, tokens); err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPartialCommitKeepsOnlyUnconsumedGeneration(t *testing.T) {
	b := NewBatch(0, 3, 4)
	tokens, err := b.Deliver([]uint16{0, 1, 2})
	if err != nil {
		t.Fatal(err)
	}
	id := submit(t, b, 0, tokens)
	// The consumed prefix can be reused before this commit CQE is delivered.
	next, err := b.Deliver([]uint16{0})
	if err != nil {
		t.Fatal(err)
	}
	if next[0].Generation == tokens[0].Generation {
		t.Fatal("generation did not advance")
	}
	retry, err := b.Complete(id, 16, 16)
	if err != nil || !reflect.DeepEqual(retry, tokens[1:]) {
		t.Fatalf("partial commit retry = %v, %v; want %v", retry, err, tokens[1:])
	}
	newID, err := b.NextID(0)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Submit(newID, retry); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Complete(id, 48, 16); err == nil {
		t.Fatal("duplicate old CQE consumed a reused commit slot")
	}
	if retry, err := b.Complete(newID, 32, 16); err != nil || len(retry) != 0 {
		t.Fatalf("suffix commit = %v, %v", retry, err)
	}
}

func TestBatchRejectsDuplicateAndContradictoryPartialCommit(t *testing.T) {
	b := NewBatch(0, 2, 4)
	if _, err := b.Deliver([]uint16{0, 0}); err == nil {
		t.Fatal("accepted duplicate delivery")
	}
	tokens, err := b.Deliver([]uint16{0, 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.Deliver([]uint16{0}); err == nil {
		t.Fatal("accepted delivery while handling")
	}
	id := submit(t, b, 0, tokens)
	for _, res := range []int32{1, 17, 48} {
		if _, err := b.Complete(id, res, 16); err == nil {
			t.Fatalf("accepted invalid commit count %d", res)
		}
	}
	if _, err := b.Complete(255, 0, 16); err == nil {
		t.Fatal("accepted out-of-range commit slot")
	}
	if _, err := b.Deliver([]uint16{1}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Complete(id, 16, 16); err == nil {
		t.Fatal("partial commit reclaimed a tag already delivered as a new generation")
	}
}

// schedulePeer is a deterministic fake completion source. It owns fixed-sized
// tag-list buffers and records commands independently of Batch's state. There
// are no sleeps, goroutines or input-sized allocations in schedule replay.
type schedulePeer struct {
	b               *Batch
	buf             []byte
	owner           [4]string
	gen             [4]uint64
	ids             [4]uint64
	sent            [4][]Token
	history         []uint64
	commits         map[Token]int
	requests        [4]Ownership
	payload         [4][16]byte
	metadata        [4][8]byte
	backendResults  [4]int32
	backendCopies   [4]int
	backendEnqueues [4]int
}

func newSchedulePeer() *schedulePeer {
	p := &schedulePeer{b: NewBatch(0, 4, 4), buf: make([]byte, 16*256), commits: map[Token]int{}}
	for i := range p.owner {
		p.owner[i] = "kernel"
	}
	return p
}

func (p *schedulePeer) check() error {
	for i, owner := range p.owner {
		if p.requests[i].Generation() != p.gen[i] {
			return fmt.Errorf("request %d lost delivery generation", i)
		}
		want := map[string]uint8{"kernel": returned, "backend": handling, "finished": queued}[owner]
		if p.b.tags[i].generation != p.gen[i] || p.b.tags[i].phase != want {
			return fmt.Errorf("tag %d: oracle owner=%s generation=%d; ledger=%+v", i, owner, p.gen[i], p.b.tags[i])
		}
	}
	for i := range p.ids {
		if p.b.slots[i].id != p.ids[i] || !reflect.DeepEqual(p.b.slots[i].tokens, p.sent[i]) {
			return fmt.Errorf("slot %d: command differs from submitted snapshot", i)
		}
	}
	for token, n := range p.commits {
		if n > 1 {
			return fmt.Errorf("generation %+v committed %d times", token, n)
		}
	}
	return nil
}

// Each 12-byte event carries a kind, a raw buffer ID/slot/tag, a signed CQE
// result, flags, and two raw uint16 tags. Hostile fields are never modulo
// normalized. Kinds: fetch, handler completion, submit, CQE, old CQE, retained
// completion handle, retained buffer handle. The latter carry a raw generation
// in bytes 8..11; invalid generations are never normalized.
func (p *schedulePeer) step(event []byte) error {
	kind, arg := event[0], int(event[1])
	res := int32(binary.LittleEndian.Uint32(event[2:6]))
	var err error
	bad := false
	switch kind {
	case 0:
		arg |= int(event[7]) << 8 // full uint16 provided-buffer ID, without masking
		clear(p.buf)
		if arg < 16 {
			copy(p.buf[arg*256:], event[8:12])
		}
		has := event[6]&1 != 0
		bad = has && arg >= 16 || res > 0 && (!has || res > 256 || res%2 != 0)
		var want []uint16
		if !bad && has && res > 0 {
			for i := 0; i < int(res); i += 2 {
				tag := binary.LittleEndian.Uint16(p.buf[arg*256+i:])
				if int(tag) >= len(p.owner) || p.owner[tag] != "kernel" {
					bad = true
					break
				}
				for _, prev := range want {
					if prev == tag {
						bad = true
					}
				}
				want = append(want, tag)
			}
		}
		var tags []uint16
		tags, err = FetchTags(p.buf, 16, 256, uint16(arg), res, has)
		if err == nil {
			_, err = p.b.Deliver(tags)
		}
		if !bad {
			for _, tag := range want {
				p.gen[tag]++
				p.owner[tag] = "backend"
				if err := p.requests[tag].Begin(); err != nil {
					return err
				}
				p.requests[tag].Store(Async)
				for i := range p.payload[tag] {
					p.payload[tag][i] = 0xcd
				}
				for i := range p.metadata[tag] {
					p.metadata[tag][i] = 0xd7
				}
				p.backendResults[tag] = 0
			}
		}
	case 1:
		bad = arg >= 4 || p.owner[arg] != "backend"
		token := Token{Tag: uint16(arg)}
		if arg < 4 {
			token.Generation = p.gen[arg]
		}
		if arg >= 4 {
			err = ErrStaleRequest
		} else {
			err = FinishGeneration(&p.requests[arg], token.Generation, func() {
				p.backendResults[arg] = res
				p.backendCopies[arg]++
			}, func() { p.backendEnqueues[arg]++ })
		}
		if err == nil {
			err = p.b.Queue(token)
		}
		if !bad {
			p.owner[arg] = "finished"
		}
	case 2:
		var tokens []Token
		for i, owner := range p.owner {
			if owner == "finished" {
				tokens = append(tokens, Token{uint16(i), p.gen[i]})
			}
		}
		bad = arg >= 4 || p.ids[arg] != 0 || len(tokens) == 0
		var id uint64
		id, err = p.b.NextID(arg)
		if err == nil {
			err = p.b.Submit(id, tokens)
		}
		if !bad {
			p.ids[arg], p.sent[arg] = id, append([]Token(nil), tokens...)
			p.history = append(p.history, id)
			for _, token := range tokens {
				p.owner[token.Tag] = "kernel"
			}
		}
	case 3, 4:
		id := uint64(arg)
		if kind == 3 && arg < 4 && p.ids[arg] != 0 {
			id = p.ids[arg]
		}
		if kind == 4 && arg < len(p.history) {
			id = p.history[arg]
		}
		// Inject a full-identity fault without reducing it to a slot byte.
		id ^= uint64(binary.LittleEndian.Uint16(event[6:8])) << 32
		slot := int(id & 255)
		bad = slot >= 4 || p.ids[slot] == 0 || p.ids[slot] != id
		done := 0
		if !bad && res > 0 {
			bad = res%16 != 0 || int64(res)/16 > int64(len(p.sent[slot]))
			if !bad {
				done = int(res / 16)
			}
		}
		if !bad {
			for _, token := range p.sent[slot][done:] {
				if p.gen[token.Tag] != token.Generation || p.owner[token.Tag] != "kernel" {
					bad = true
				}
			}
		}
		var retry []Token
		retry, err = p.b.Complete(id, res, 16)
		if !bad {
			want := p.sent[slot][done:]
			if !reflect.DeepEqual(retry, want) {
				return fmt.Errorf("partial retry %v; want %v", retry, want)
			}
			for _, token := range p.sent[slot][:done] {
				p.commits[token]++
			}
			for _, token := range want {
				p.owner[token.Tag] = "finished"
			}
			p.ids[slot], p.sent[slot] = 0, nil
		}
	case 5, 6: // retained completion/buffer handle: generation is raw input
		generation := uint64(binary.LittleEndian.Uint32(event[8:12]))
		bad = arg >= 4 || generation == 0 || generation != p.gen[arg] || p.owner[arg] != "backend"
		if arg >= 4 {
			err = ErrStaleRequest
			break
		}
		beforePayload, beforeMetadata := p.payload[arg], p.metadata[arg]
		beforeResult, beforeCopies, beforeEnqueues := p.backendResults[arg], p.backendCopies[arg], p.backendEnqueues[arg]
		if kind == 5 {
			err = FinishGeneration(&p.requests[arg], generation, func() {
				p.backendResults[arg] = res
				p.backendCopies[arg]++
			}, func() { p.backendEnqueues[arg]++ })
			if err == nil {
				if queueErr := p.b.Queue(Token{uint16(arg), generation}); queueErr != nil {
					return queueErr
				}
				p.owner[arg] = "finished"
			}
		} else {
			err = Access(&p.requests[arg], generation, func() error {
				clear(p.payload[arg][:])
				clear(p.metadata[arg][:])
				return nil
			})
		}
		if bad && (beforePayload != p.payload[arg] || beforeMetadata != p.metadata[arg] ||
			beforeResult != p.backendResults[arg] || beforeCopies != p.backendCopies[arg] || beforeEnqueues != p.backendEnqueues[arg]) {
			return fmt.Errorf("rejected handle changed buffers/result/publication for tag %d", arg)
		}
	default:
		return p.check() // unknown schedule instruction has no effect
	}
	if (err != nil) != bad {
		return fmt.Errorf("event %x: rejection=%v, error=%v", event, bad, err)
	}
	return p.check()
}

func scheduleEvent(kind, arg byte, res int32, flags uint16, a, b uint16) []byte {
	e := make([]byte, 12)
	e[0], e[1] = kind, arg
	binary.LittleEndian.PutUint32(e[2:], uint32(res))
	binary.LittleEndian.PutUint16(e[6:], flags)
	binary.LittleEndian.PutUint16(e[8:], a)
	binary.LittleEndian.PutUint16(e[10:], b)
	return e
}

func completionSeeds() [][]byte {
	valid := append(scheduleEvent(0, 0, 4, 1, 0, 1), scheduleEvent(1, 0, 0, 0, 0, 0)...)
	valid = append(valid, scheduleEvent(1, 1, 0, 0, 0, 0)...)
	valid = append(valid, scheduleEvent(2, 0, 0, 0, 0, 0)...)
	partial := append(append([]byte(nil), valid...), scheduleEvent(3, 0, 16, 0, 0, 0)...)
	partial = append(partial, scheduleEvent(2, 0, 0, 0, 0, 0)...)
	partial = append(partial, scheduleEvent(4, 0, 32, 0, 0, 0)...)
	partial = append(partial, scheduleEvent(3, 0, 16, 0, 0, 0)...)
	early := append(append([]byte(nil), valid...), scheduleEvent(0, 1, 2, 1, 0, 0)...)
	early = append(early, scheduleEvent(3, 0, 16, 0, 0, 0)...)
	stale := append(append([]byte(nil), valid...), scheduleEvent(3, 0, 32, 0, 0, 0)...)
	stale = append(stale, scheduleEvent(0, 1, 2, 1, 0, 0)...)
	stale = append(stale, scheduleEvent(5, 0, 512, 0, 1, 0)...)
	stale = append(stale, scheduleEvent(6, 0, 0, 0, 1, 0)...)
	stale = append(stale, scheduleEvent(6, 0, 0, 0, 2, 0)...)
	stale = append(stale, scheduleEvent(5, 0, 512, 0, 2, 0)...)
	stale = append(stale, scheduleEvent(5, 0, -28, 0, 2, 0)...)
	return [][]byte{stale,
		append(valid, scheduleEvent(3, 0, 32, 0, 0, 0)...), partial, early,
		scheduleEvent(0, 16, 2, 1, 0, 0), scheduleEvent(0, 0, 3, 1, 0, 0),
		scheduleEvent(0, 0, 258, 1, 0, 0), scheduleEvent(0, 0, 4, 1, 0, 0),
		append(append([]byte(nil), valid...), scheduleEvent(3, 0, 17, 0, 0, 0)...),
		append(append([]byte(nil), valid...), scheduleEvent(3, 255, 16, 0, 0, 0)...),
	}
}

func TestCompletionSchedulesReplay(t *testing.T) {
	for i, script := range completionSeeds() {
		p := newSchedulePeer()
		for pos := 0; pos+12 <= len(script); pos += 12 {
			if err := p.step(script[pos : pos+12]); err != nil {
				t.Fatalf("seed %d step %d: %v", i, pos/12, err)
			}
		}
	}
}

func TestCompletionScheduleOracleDetectsLostOwnership(t *testing.T) {
	p := newSchedulePeer()
	if err := p.step(scheduleEvent(0, 0, 2, 1, 0, 0)); err != nil {
		t.Fatal(err)
	}
	// Inject a premature tag return, as a broken partial-commit path would.
	p.b.tags[0].phase = returned
	if err := p.check(); err == nil {
		t.Fatal("ownership oracle missed premature tag return")
	}
	p.b.tags[0].phase = handling
	p.commits[Token{0, 1}] = 2
	if err := p.check(); err == nil {
		t.Fatal("commit oracle missed duplicate terminal result")
	}
}

// FuzzCompletionSchedule checks functional completion handling against the
// deterministic fake peer and independent per-generation ownership oracle.
func FuzzCompletionSchedule(f *testing.F) {
	for _, script := range completionSeeds() {
		f.Add(script)
	}
	f.Fuzz(func(t *testing.T, script []byte) {
		if len(script) > 12*64 {
			script = script[:12*64]
		}
		p := newSchedulePeer()
		for pos := 0; pos+12 <= len(script); pos += 12 {
			if err := p.step(script[pos : pos+12]); err != nil {
				t.Fatalf("step %d: %v", pos/12, err)
			}
		}
	})
}
