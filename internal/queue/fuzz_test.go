package queue

import (
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// FuzzEngine drives the real engine against the fake kernel with a script
// decoded from the fuzz input: which tags get which requests, how each is
// completed (inline, from another goroutine, after a delay, with an error,
// partially), and whether the run ends with STOP_DEV-style aborts or an
// abandon. Invariants: the fake kernel sees no protocol violation, every
// delivered request is committed exactly once with a result the kernel would
// accept for its operation, and the engine exits with no handler holding a
// request.
func FuzzEngine(f *testing.F) {
	f.Add([]byte{0, 4, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11})
	f.Add([]byte{1, 2, 0, 0, 0, 0, 0, 0, 255, 255})
	f.Add([]byte{3, 8, 7, 6, 5, 4, 3, 2, 1, 0, 9, 9, 9, 9, 9})
	f.Add([]byte{8, 6, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10})
	f.Add([]byte{25, 8, 31, 17, 3, 200, 5, 9, 11, 13})
	f.Fuzz(func(t *testing.T, script []byte) {
		if len(script) < 2 {
			return
		}
		mode, depth := script[0], 1+int(script[1]%8)
		script = script[2:]
		if len(script) > 64 {
			script = script[:64]
		}
		inline := mode&1 != 0
		stopKind := (mode >> 1) & 1 // 0: abort like STOP_DEV, 1: abandon
		batch := mode&8 != 0
		k := newFakeKernel(t, depth, testBufSize)
		k.needGetData = mode&4 != 0 && !batch // the kernel clears NEED_GET_DATA under BATCH_IO
		if batch && mode&16 != 0 {
			k.commitLimit = 1 + int(mode>>5)%3 // force partial commits
		}

		h := HandlerFunc(func(r *Request) {
			b := byte(r.Offset >> 9) // the script byte that created it
			complete := func() {
				switch b % 5 {
				case 0, 1:
					r.Complete(nil)
				case 2:
					r.Complete(syscall.Errno(1 + b%40))
				case 3:
					r.CompleteN(int(r.Length)/2, nil)
				default:
					r.CompleteN(int(r.Length), nil)
				}
			}
			switch {
			case inline && b&8 == 0:
				complete()
			case b&16 != 0:
				go func() { runtime.Gosched(); complete() }()
			default:
				go func() { time.Sleep(time.Duration(b%7) * 50 * time.Microsecond); complete() }()
			}
		})
		e := newEngine(engineConfig{
			tagLo: 0, tagHi: depth, charFd: -1,
			desc: unsafe.Pointer(&k.desc[0]), descStride: 24,
			bufs: unsafe.Pointer(&k.bufs[0]), bufSize: testBufSize,
			handler: h, inline: inline, batch: batch, cpu: -1, waitInterval: 5 * time.Millisecond,
			newRing: func(uint32) (ring, error) { return k, nil },
		})
		if err := e.start(); err != nil {
			t.Fatal(err)
		}

		ops := []uint8{uapi.UBLK_IO_OP_READ, uapi.UBLK_IO_OP_WRITE, uapi.UBLK_IO_OP_FLUSH,
			uapi.UBLK_IO_OP_DISCARD, uapi.UBLK_IO_OP_WRITE_ZEROES}
		for i, b := range script {
			op := ops[int(b)%len(ops)]
			nr := uint32(1 + int(b)%16)
			if op == uapi.UBLK_IO_OP_FLUSH {
				nr = 0
			}
			var data []byte
			if op == uapi.UBLK_IO_OP_WRITE {
				data = make([]byte, nr<<9)
			}
			// The handler recovers the script byte from the sector.
			k.inject(int(b)%depth, fkReq{op: op, sector: uint64(b), nr: nr, data: data, id: i})
		}
		time.Sleep(time.Duration(len(script)%5) * time.Millisecond)
		if stopKind == 0 {
			k.stop()
		} else {
			e.abandon()
		}
		select {
		case <-e.done:
		case <-time.After(10 * time.Second):
			t.Fatalf("engine did not exit (stop kind %d)", stopKind)
		}
		if e.handlers.Load() != 0 {
			t.Fatalf("engine exited with %d requests still held by handlers", e.handlers.Load())
		}

		commits, violations := k.snapshot()
		if len(violations) > 0 {
			t.Fatalf("protocol violations: %v", violations)
		}
		seen := map[int]bool{}
		for _, c := range commits {
			if seen[c.id] {
				t.Fatalf("request %d committed twice", c.id)
			}
			seen[c.id] = true
			b := script[c.id]
			op := ops[int(b)%len(ops)]
			length := int32(1+int(b)%16) << 9
			switch {
			case c.result < 0:
				if c.result < -4095 {
					t.Fatalf("request %d: result %d is not an errno", c.id, c.result)
				}
			case op == uapi.UBLK_IO_OP_READ:
				if c.result == 0 || c.result > length {
					t.Fatalf("read %d committed %d of %d bytes", c.id, c.result, length)
				}
			case op == uapi.UBLK_IO_OP_WRITE:
				if c.result != length {
					t.Fatalf("write %d committed %d of %d bytes; a short write must fail", c.id, c.result, length)
				}
			default:
				if c.result != 0 {
					t.Fatalf("%d (op %d) committed %d; range ops and flush commit 0", c.id, op, c.result)
				}
			}
		}
		// Under an abort every delivered request must have been committed
		// before the engine exited (the kernel waits for them in STOP_DEV).
		if stopKind == 0 {
			k.mu.Lock()
			for tag := range k.tags {
				if k.tags[tag].state == fkOwned || k.tags[tag].state == fkGetData {
					k.mu.Unlock()
					t.Fatalf("tag %d left owned by the server after STOP_DEV drained", tag)
				}
			}
			k.mu.Unlock()
		}
	})
}
