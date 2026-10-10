// Package completion contains the portable ownership checks used by the queue
// engine. It never follows an address supplied by a completion.
package completion

import (
	"encoding/binary"
	"fmt"
)

// FetchTags validates the selected buffer and byte count before slicing. A
// payload-free terminal CQE is valid; a positive count needs a selected buffer.
// The returned tags are a snapshot, so recycling the buffer cannot change them.
func FetchTags(buffers []byte, count, size int, bid uint16, res int32, hasBuffer bool) ([]uint16, error) {
	if count <= 0 || size <= 0 || count > len(buffers)/size {
		return nil, fmt.Errorf("batch tag buffer layout is invalid")
	}
	if !hasBuffer {
		if res > 0 {
			return nil, fmt.Errorf("batch fetch returned %d bytes without a buffer", res)
		}
		return nil, nil
	}
	if int(bid) >= count {
		return nil, fmt.Errorf("batch fetch buffer ID %d outside [0,%d)", bid, count)
	}
	if res <= 0 {
		return nil, nil
	}
	if int64(res) > int64(size) || res%2 != 0 {
		return nil, fmt.Errorf("batch fetch byte count %d exceeds or misaligns buffer size %d", res, size)
	}
	buf := buffers[int(bid)*size : int(bid)*size+int(res)]
	tags := make([]uint16, int(res)/2)
	for i := range tags {
		tags[i] = binary.LittleEndian.Uint16(buf[2*i:])
	}
	return tags, nil
}

// Token identifies one delivered tag generation. Tokens are internal to the
// batch command ledger; the public Request pointer still has no generation.
type Token struct {
	Tag        uint16
	Generation uint64
}

const (
	returned uint8 = iota
	handling
	queued
)

type tag struct {
	generation uint64
	phase      uint8
}

type command struct {
	id     uint64
	tokens []Token
}

// Batch tracks submitted command identities and tag generations independently
// of mutable Request objects. A consumed tag can be fetched again before its
// commit CQE; an unconsumed suffix must still belong to the submitted generation.
// All methods run on the engine's owner thread.
type Batch struct {
	lo    int
	tags  []tag
	slots []command
	next  uint64
}

func NewBatch(lo, hi, slots int) *Batch {
	if lo < 0 || hi <= lo || hi > 65536 || slots <= 0 || slots > 256 {
		panic("invalid internal batch geometry")
	}
	return &Batch{lo: lo, tags: make([]tag, hi-lo), slots: make([]command, slots)}
}

func (b *Batch) index(token Token, phase uint8) (int, error) {
	i := int(token.Tag) - b.lo
	if i < 0 || i >= len(b.tags) || b.tags[i].generation != token.Generation || b.tags[i].phase != phase {
		return 0, fmt.Errorf("batch tag %d generation %d has no ownership in phase %d", token.Tag, token.Generation, phase)
	}
	return i, nil
}

// Deliver validates the entire list before handing any request to a handler.
func (b *Batch) Deliver(tags []uint16) ([]Token, error) {
	for k, t := range tags {
		i := int(t) - b.lo
		if i < 0 || i >= len(b.tags) || b.tags[i].phase != returned || b.tags[i].generation == ^uint64(0) {
			return nil, fmt.Errorf("batch fetch delivered unowned tag %d", t)
		}
		for _, prev := range tags[:k] {
			if prev == t {
				return nil, fmt.Errorf("batch fetch delivered duplicate tag %d", t)
			}
		}
	}
	tokens := make([]Token, len(tags))
	for k, t := range tags {
		i := int(t) - b.lo
		b.tags[i].generation++
		b.tags[i].phase = handling
		tokens[k] = Token{t, b.tags[i].generation}
	}
	return tokens, nil
}

func (b *Batch) Queue(token Token) error {
	i, err := b.index(token, handling)
	if err != nil {
		return err
	}
	b.tags[i].phase = queued
	return nil
}

// NextID reserves no state. Its low byte is the slot; the remaining low 56
// bits identify the submission. The engine stores the kind in the top byte.
func (b *Batch) NextID(slot int) (uint64, error) {
	if slot < 0 || slot >= len(b.slots) || b.slots[slot].tokens != nil || b.next == (1<<48)-1 {
		return 0, fmt.Errorf("batch commit slot %d unavailable", slot)
	}
	return (b.next+1)<<8 | uint64(slot), nil
}

func (b *Batch) Submit(id uint64, tokens []Token) error {
	slot := int(id & 0xff)
	want, err := b.NextID(slot)
	if err != nil || want != id || len(tokens) == 0 {
		return fmt.Errorf("batch commit identity %#x is not the next submission", id)
	}
	for k, t := range tokens {
		if _, err := b.index(t, queued); err != nil {
			return err
		}
		for _, prev := range tokens[:k] {
			if prev == t {
				return fmt.Errorf("batch commit repeats tag %d", t.Tag)
			}
		}
	}
	b.next++
	b.slots[slot] = command{id, append([]Token(nil), tokens...)}
	for _, t := range tokens {
		b.tags[int(t.Tag)-b.lo].phase = returned
	}
	return nil
}

// Complete returns exactly the unconsumed suffix. Invalid CQEs have no effect
// on ownership; the caller must fail the engine rather than retry uncertain IO.
func (b *Batch) Complete(id uint64, res int32, elemBytes int) ([]Token, error) {
	slot := int(id & 0xff)
	if id>>56 != 0 || slot >= len(b.slots) || b.slots[slot].tokens == nil || b.slots[slot].id != id {
		return nil, fmt.Errorf("batch completion identity %#x has no submitted command", id)
	}
	sent := b.slots[slot].tokens
	if elemBytes <= 0 || res > 0 && (int64(res)%int64(elemBytes) != 0 || int64(res)/int64(elemBytes) > int64(len(sent))) {
		return nil, fmt.Errorf("batch commit byte count %d invalid for %d elements of %d bytes", res, len(sent), elemBytes)
	}
	done := 0
	if res > 0 {
		done = int(int64(res) / int64(elemBytes))
	}
	retry := sent[done:]
	for _, t := range retry {
		if _, err := b.index(t, returned); err != nil {
			return nil, fmt.Errorf("batch partial commit returned a reused tag: %w", err)
		}
	}
	for _, t := range retry {
		b.tags[int(t.Tag)-b.lo].phase = queued
	}
	b.slots[slot] = command{}
	return retry, nil
}

// Forget retires only the command named by a malformed final CQE, allowing
// fatal drain to close the ring without retransmitting an uncertain result.
func (b *Batch) Forget(id uint64) bool {
	slot := int(id & 0xff)
	if slot >= len(b.slots) || b.slots[slot].tokens == nil || b.slots[slot].id != id {
		return false
	}
	b.slots[slot] = command{}
	return true
}
