package ctrl

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

// FuzzCtrlDecoders feeds arbitrary "kernel" replies (synthetic bytes only, no
// syscalls) through the full command path of every reply-carrying command and
// checks the decoders against independent readings of the same bytes.
func FuzzCtrlDecoders(f *testing.F) {
	f.Add([]byte{}, int32(0), uint32(0))
	f.Add(bytes.Repeat([]byte{0xff}, 600), int32(0), uint32(0x7f))
	f.Add([]byte{1, 2, 3, 4, 5, 6, 7, 8}, int32(-22), uint32(1))
	f.Fuzz(func(t *testing.T, reply []byte, res int32, types uint32) {
		if len(reply) > 4096 {
			reply = reply[:4096]
		}
		ring := &controlTestRing{submit: func(op uint32, cmd *uapi.UblksrvCtrlCmd) (uring.Result, error) {
			buf := controlTestBuffer(cmd)
			copy(buf, reply)
			if op == uapi.UBLK_U_CMD_GET_PARAMS && len(buf) >= 8 {
				binary.LittleEndian.PutUint32(buf[4:8], types)
			}
			return controlTestResult(res), nil
		}}
		c := newTestController(ring)
		padded := make([]byte, 4096)
		copy(padded, reply)

		if info, err := c.GetDevInfo(bg, 1); err == nil {
			var want uapi.UblksrvCtrlDevInfo
			_ = uapi.Unmarshal(padded[:64], &want)
			if res < 0 || *info != want {
				t.Fatalf("GET_DEV_INFO decode %+v want %+v (res %d)", info, want, res)
			}
		} else if res >= 0 {
			t.Fatalf("GET_DEV_INFO failed on res %d: %v", res, err)
		}

		if fs, err := c.GetFeatures(bg); err == nil && fs != binary.LittleEndian.Uint64(padded) {
			t.Fatalf("GET_FEATURES %#x", fs)
		}

		if p, err := c.GetParams(bg, 1); err == nil {
			// What the buffer holds: our capacity header, overwritten by the reply.
			image := make([]byte, getParamsCapacity)
			binary.LittleEndian.PutUint32(image, getParamsCapacity)
			copy(image, reply)
			binary.LittleEndian.PutUint32(image[4:8], types)
			if res < 0 || p.Types != types || p.Len != binary.LittleEndian.Uint32(image) {
				t.Fatalf("GET_PARAMS header %+v", p)
			}
			canonical := uapi.Marshal(p)
			for _, b := range []struct {
				bit        uint32
				start, end int
			}{{1, 8, 40}, {2, 40, 60}, {4, 60, 76}, {8, 76, 108}, {16, 108, 116}, {32, 120, 136}, {64, 136, 152}} {
				if types&b.bit != 0 && !bytes.Equal(canonical[b.start:b.end], image[b.start:b.end]) {
					t.Fatalf("GET_PARAMS block %#x moved", b.bit)
				}
			}
		}

		if cpus, err := c.GetQueueAffinity(bg, 1, 0); err == nil {
			mask := padded[:affinityBufSize]
			n := 0
			for i, cpu := range cpus {
				if i > 0 && cpu <= cpus[i-1] {
					t.Fatal("affinity not ascending")
				}
				if mask[cpu/8]&(1<<(cpu%8)) == 0 {
					t.Fatalf("cpu %d not in mask", cpu)
				}
			}
			for _, v := range mask {
				for ; v != 0; v &= v - 1 {
					n++
				}
			}
			if n != len(cpus) {
				t.Fatalf("decoded %d cpus, mask has %d", len(cpus), n)
			}
		}
		_ = FeatureNames(binary.LittleEndian.Uint64(padded))
		_ = ParamTypeNames(types)
	})
}
