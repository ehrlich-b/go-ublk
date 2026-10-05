package uapi

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"reflect"
	"testing"
)

// These are Linux UAPI offsets, independent of paramsSize and the Go layout.
var paramBlocks = []struct {
	bit        uint32
	start, end int
}{
	{UBLK_PARAM_TYPE_BASIC, 8, 40}, {UBLK_PARAM_TYPE_DISCARD, 40, 60},
	{UBLK_PARAM_TYPE_DEVT, 60, 76}, {UBLK_PARAM_TYPE_ZONED, 76, 108},
}

func fixtureParams(types uint32) UblkParams {
	return UblkParams{
		Len: 123, Types: types,
		Basic:   UblkParamBasic{Attrs: 0x12345678, LogicalBSShift: 9, PhysicalBSShift: 12, MaxSectors: 0x87654321, DevSectors: 0x1122334455667788},
		Discard: UblkParamDiscard{DiscardAlignment: 0x11223344, DiscardGranularity: 4096, MaxDiscardSegments: 1},
		Devt:    UblkParamDevt{CharMajor: 0x11223344, CharMinor: 0x55667788, DiskMajor: 0x99aabbcc, DiskMinor: 0xddeeff00},
		Zoned:   UblkParamZoned{MaxOpenZones: 0x12345678, Reserved: [20]uint8{1, 2, 3}},
	}
}

func paramPrefix(types uint32) int {
	size := 8
	for _, b := range paramBlocks {
		if types&b.bit != 0 {
			size = b.end
		}
	}
	return size
}

func TestSparseParamsFixedOffsets(t *testing.T) {
	full := fixtureParams(15)
	fullBytes := Marshal(&full)
	for types := uint32(0); types < 16; types++ {
		t.Run(fmt.Sprintf("types_%x", types), func(t *testing.T) {
			p := fixtureParams(types)
			size := paramPrefix(types)
			want := make([]byte, size)
			binary.LittleEndian.PutUint32(want[:4], uint32(size))
			binary.LittleEndian.PutUint32(want[4:8], types)
			for _, b := range paramBlocks {
				if types&b.bit != 0 {
					copy(want[b.start:b.end], fullBytes[b.start:b.end])
				}
			}
			if got := Marshal(&p); !bytes.Equal(got, want) {
				t.Fatalf("Marshal = %x, want %x", got, want)
			}
			buf := bytes.Repeat([]byte{0xa5}, size+17)
			n, err := MarshalInto(&p, buf)
			if err != nil || n != size || !bytes.Equal(buf[:n], want) || !bytes.Equal(buf[n:], bytes.Repeat([]byte{0xa5}, 17)) {
				t.Fatalf("MarshalInto prefix/tail contract: n=%d err=%v buf=%x", n, err, buf)
			}
			// Short buffers must not change either input or output, at any size.
			for length := 0; length < size; length++ {
				buf := bytes.Repeat([]byte{0xa5}, length)
				if n, err := MarshalInto(&p, buf); n != 0 || err != ErrBufferTooSmall || !bytes.Equal(buf, bytes.Repeat([]byte{0xa5}, length)) {
					t.Fatalf("MarshalInto short size %d: n=%d err=%v", length, n, err)
				}
			}
			var decoded UblkParams
			if err := Unmarshal(want, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Len != uint32(size) || decoded.Types != types {
				t.Fatalf("bad header: %+v", decoded)
			}
			if types&1 == 0 {
				p.Basic = UblkParamBasic{}
			}
			if types&2 == 0 {
				p.Discard = UblkParamDiscard{}
			}
			if types&4 == 0 {
				p.Devt = UblkParamDevt{}
			}
			if types&8 == 0 {
				p.Zoned = UblkParamZoned{}
			}
			p.Len = uint32(size)
			if decoded != p {
				t.Fatalf("fixed-offset decode = %+v, want %+v", decoded, p)
			}
		})
	}
}

func TestParamsDeclaredLengthAndAtomicFailure(t *testing.T) {
	for types := uint32(0); types < 16; types++ {
		for length := uint32(0); length <= 128; length++ {
			data := bytes.Repeat([]byte{0xa5}, 128)
			binary.LittleEndian.PutUint32(data[:4], length)
			binary.LittleEndian.PutUint32(data[4:8], types)
			before := fixtureParams(15)
			got := before
			err := Unmarshal(data, &got)
			if length < uint32(paramPrefix(types)) {
				if err != ErrInsufficientData || got != before {
					t.Fatalf("types=%x Len=%d: err=%v mutated=%v", types, length, err, got != before)
				}
			} else if err != nil {
				t.Fatalf("types=%x Len=%d: %v", types, length, err)
			}
		}
	}
	for _, length := range []uint32{129, 0x80000000, 0xffffffff} {
		data := make([]byte, 128)
		binary.LittleEndian.PutUint32(data, length)
		got := fixtureParams(15)
		before := got
		if err := Unmarshal(data, &got); err != ErrInsufficientData || got != before {
			t.Fatalf("Len=%d: err=%v mutated=%v", length, err, got != before)
		}
	}
}

func TestParamsFutureTailAndDestinationReuse(t *testing.T) {
	p := fixtureParams(UBLK_PARAM_TYPE_BASIC)
	data := make([]byte, 160) // newer kernels append fields after the known prefix
	copy(data, Marshal(&p))
	binary.LittleEndian.PutUint32(data[:4], uint32(len(data)))
	binary.LittleEndian.PutUint32(data[4:8], p.Types|1<<30)
	got := fixtureParams(15)
	if err := Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got.Len != 160 || got.Types != p.Types|1<<30 || got.Basic != p.Basic || got.Discard != (UblkParamDiscard{}) || got.Devt != (UblkParamDevt{}) || got.Zoned != (UblkParamZoned{}) {
		t.Fatalf("unknown tail or reused destination: %+v", got)
	}
}

func TestParamsKernelResponseUsesCapacity(t *testing.T) {
	for _, reportedLen := range []uint32{0, 40, 60, 4096} {
		data := make([]byte, 112)
		binary.LittleEndian.PutUint32(data, reportedLen)
		binary.LittleEndian.PutUint32(data[4:8], UBLK_PARAM_TYPE_BASIC|UBLK_PARAM_TYPE_DEVT)
		binary.LittleEndian.PutUint32(data[60:64], 241)
		binary.LittleEndian.PutUint32(data[68:72], 259)
		var got UblkParams
		if err := UnmarshalParamsResponse(data, &got); err != nil || got.Len != reportedLen || got.Devt.CharMajor != 241 || got.Devt.DiskMajor != 259 {
			t.Fatalf("kernel retained Len=%d: %+v err=%v", reportedLen, got, err)
		}
		for size := 0; size < 76; size++ {
			before := fixtureParams(15)
			got := before
			if err := UnmarshalParamsResponse(data[:size], &got); err != ErrInsufficientData || got != before {
				t.Fatalf("response truncated at %d: %+v err=%v", size, got, err)
			}
		}
	}
}

func TestFixedUnmarshalAtomicTruncation(t *testing.T) {
	values := []interface{}{
		&UblksrvCtrlCmd{DevID: 42, Reserved: 123}, &UblksrvIOCmd{QID: 3, Result: -5},
		&UblksrvCtrlDevInfo{DevID: 42, UblksrvPID: -1}, &UblksrvIODesc{OpFlags: 0x1234, NrSectors: 8},
	}
	for _, value := range values {
		data := Marshal(value)
		for size := 0; size < len(data); size++ {
			before := reflect.ValueOf(value).Elem().Interface()
			if err := Unmarshal(data[:size], value); err != ErrInsufficientData || !reflect.DeepEqual(before, reflect.ValueOf(value).Elem().Interface()) {
				t.Fatalf("%T truncation at %d: err=%v", value, size, err)
			}
		}
	}
}
