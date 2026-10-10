package queue

import (
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

type boundsBackend struct{ t *testing.T }

func (b boundsBackend) ReadAt([]byte, int64) (int, error) {
	b.t.Fatal("invalid read reached backend")
	return 0, nil
}
func (b boundsBackend) WriteAt([]byte, int64) (int, error) {
	b.t.Fatal("invalid write reached backend")
	return 0, nil
}
func (b boundsBackend) Flush() error { b.t.Fatal("invalid flush reached backend"); return nil }
func (b boundsBackend) Size() int64  { return 8192 }
func (b boundsBackend) Close() error { return nil }

func TestEngineRequestBoundsBeforeBackend(t *testing.T) {
	for _, mode := range []string{"copy-inline", "copy-async", "user-copy", "batch-copy", "batch-user-copy", "shared-memory", "zero-copy-manual", "zero-copy-auto", "batch-zero-copy"} {
		for _, tc := range []struct {
			name    string
			start   uint64
			sectors uint32
		}{
			{"last-block-end", 8, 16}, {"start-at-capacity", 16, 8}, {"start-alignment", 1, 8},
			{"length-alignment", 0, 1}, {"payload-limit", 0, 16}, {"empty-payload", 0, 0},
			{"signed-sector-edge", 1 << 54, 8}, {"unsigned-sector-edge", ^uint64(0), 8},
			{"maximum-sector-count", 0, ^uint32(0)},
		} {
			t.Run(mode+"/"+tc.name, func(t *testing.T) {
				k := newFakeKernel(t, 1, 4096)
				cfg := engineConfig{capacity: func() int64 { return 8192 }, logicalBlockSize: 4096,
					tagHi: 1, charFd: -1, desc: unsafe.Pointer(&k.desc[0]), descStride: 24,
					bufs: unsafe.Pointer(&k.bufs[0]), bufSize: 4096,
					handler: BackendHandler(boundsBackend{t}, nil), inline: mode != "copy-async"}
				cfg.userCopy = strings.Contains(mode, "user-copy")
				cfg.batch = strings.HasPrefix(mode, "batch")
				if strings.Contains(mode, "zero-copy") {
					cfg.zeroCopy = &zeroCopyConfig{fd: 77, base: 4096, auto: mode != "zero-copy-manual"}
				}
				d := (*uapi.UblksrvIODesc)(cfg.desc)
				*d = uapi.UblksrvIODesc{OpFlags: uapi.UBLK_IO_OP_READ, StartSector: tc.start, NrSectors: tc.sectors}
				if mode == "shared-memory" {
					d.OpFlags |= uint32(FlagSharedMemory)
				}
				e := newEngine(cfg)
				e.ring = k
				e.dispatch(0)
				r := e.head.Load()
				if r == nil || r.result != -int32(syscall.EIO) || r.Data != nil {
					t.Fatalf("invalid range was not rejected before dispatch: %+v", r)
				}
				if k.sqLen != 0 || len(k.fileOps) != 0 || len(k.registered) != 0 {
					t.Fatal("invalid range submitted backend or registration I/O")
				}
			})
		}
	}
}

func TestQueueMappingBoundsBeforeMmap(t *testing.T) {
	base := QueueConfig{QueueID: 0, NumQueues: 1, Capacity: 8192, LogicalBlockSize: 4096,
		Depth: 1, MaxIOSize: 4096, CharFd: -1, DescSize: 24, ZeroCopyFile: -1, Handler: HandlerFunc(func(*Request) {})}
	for _, tc := range []struct {
		name   string
		change func(*QueueConfig)
		prefix string
	}{
		{"queue-id", func(c *QueueConfig) { c.QueueID = 1 }, "queue descriptor layout:"},
		{"short-stride", func(c *QueueConfig) { c.DescSize = 16 }, "queue descriptor layout:"},
		{"unaligned-stride", func(c *QueueConfig) { c.DescSize = 25 }, "queue descriptor layout:"},
		{"large-stride", func(c *QueueConfig) { c.DescSize = 264 }, "queue descriptor layout:"},
		{"returned-zero-stride", func(c *QueueConfig) { c.DescSize = 0; c.Flags = uapi.UBLK_F_IO_DESC_SIZE }, "queue descriptor layout:"},
		{"capacity-end", func(c *QueueConfig) { c.Capacity = 8193 }, "queue request limits:"},
		{"integrity-product", func(c *QueueConfig) { c.IntegrityInterval = 1; c.IntegrityMetadata = int(^uint(0) >> 1) }, "integrity tag layout:"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := base
			tc.change(&c)
			q, err := NewQueue(c)
			if q != nil || err == nil || !strings.HasPrefix(err.Error(), tc.prefix) {
				t.Fatalf("expected rejection before mmap, got queue %v, error %v", q, err)
			}
		})
	}
}

func TestEngineCapacityBoundsAfterResize(t *testing.T) {
	k := newFakeKernel(t, 1, 4096)
	q := &Queue{}
	q.SetCapacity(8192)
	calls := 0
	cfg := engineConfig{capacity: q.capacity.Load, logicalBlockSize: 4096,
		tagHi: 1, charFd: -1, desc: unsafe.Pointer(&k.desc[0]), descStride: 24,
		bufs: unsafe.Pointer(&k.bufs[0]), bufSize: 4096, inline: true,
		handler: HandlerFunc(func(r *Request) {
			calls++
			if r.Offset != 8192 || r.Length != 4096 {
				t.Fatalf("incorrect range: %+v", r)
			}
		}),
	}
	*(*uapi.UblksrvIODesc)(cfg.desc) = uapi.UblksrvIODesc{OpFlags: uapi.UBLK_IO_OP_READ, StartSector: 16, NrSectors: 8}
	for _, step := range []struct {
		size  int64
		valid bool
	}{{8192, false}, {12288, true}, {8192, false}} {
		q.SetCapacity(step.size)
		e := newEngine(cfg)
		e.dispatch(0)
		if (e.head.Load() == nil) != step.valid {
			t.Fatalf("capacity %d did not govern the range", step.size)
		}
	}
	if calls != 1 {
		t.Fatalf("backend calls=%d want 1", calls)
	}
}
