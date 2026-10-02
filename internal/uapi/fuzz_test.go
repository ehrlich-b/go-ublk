package uapi

import (
	"bytes"
	"encoding/binary"
	"reflect"
	"testing"
)

// The fuzz inputs are synthetic bytes only: no syscalls, device data, pointer
// dereferences from the payload, or unbounded sizes. Both targets cap at 512 B.
func FuzzFixedUAPI(f *testing.F) {
	for kind := uint8(0); kind < 4; kind++ {
		f.Add(kind, []byte{}, uint16(0))
		f.Add(kind, bytes.Repeat([]byte{0xff}, 80), uint16(80))
		f.Add(kind, []byte{0, 1, 2, 3, 4, 5, 6, 7}, uint16(16))
	}
	f.Fuzz(func(t *testing.T, kind uint8, data []byte, capacity uint16) {
		if len(data) > 512 {
			data = data[:512]
		}
		values := []interface{}{
			&UblksrvCtrlCmd{DevID: 42}, &UblksrvIOCmd{Result: -5},
			&UblksrvCtrlDevInfo{DevID: 42}, &UblksrvIODesc{NrSectors: 8},
		}
		value := values[int(kind)%len(values)]
		before := reflect.ValueOf(value).Elem().Interface()
		input := bytes.Clone(data)
		size := len(Marshal(value))
		err := Unmarshal(data, value)
		if len(data) < size {
			if err != ErrInsufficientData || !reflect.DeepEqual(before, reflect.ValueOf(value).Elem().Interface()) {
				t.Fatalf("short input: err=%v", err)
			}
		} else if err != nil || !bytes.Equal(Marshal(value), data[:size]) {
			t.Fatalf("fixed bytes changed: err=%v", err)
		}
		if !bytes.Equal(data, input) {
			t.Fatal("Unmarshal modified input")
		}
		assertFuzzMarshalBounds(t, value, int(capacity)%513)
	})
}

func FuzzParamsUAPI(f *testing.F) {
	for types := uint32(0); types < 16; types++ {
		p := fixtureParams(types)
		data := Marshal(&p)
		f.Add(data, uint16(len(data)))
		short := bytes.Clone(data)
		binary.LittleEndian.PutUint32(short, 7)
		f.Add(short, uint16(0))
	}
	f.Add([]byte{}, uint16(0))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff}, uint16(512))
	f.Fuzz(func(t *testing.T, data []byte, capacity uint16) {
		if len(data) > 512 {
			data = data[:512]
		}
		before := fixtureParams(15)
		got := before
		input := bytes.Clone(data)
		err := Unmarshal(data, &got)
		valid := false
		if len(data) >= 8 {
			length := binary.LittleEndian.Uint32(data[:4])
			types := binary.LittleEndian.Uint32(data[4:8])
			valid = uint64(length) <= uint64(len(data)) && length >= uint32(paramPrefix(types))
		}
		if !valid {
			if err != ErrInsufficientData || got != before {
				t.Fatalf("invalid input: err=%v mutated=%v", err, got != before)
			}
		} else {
			if err != nil {
				t.Fatal(err)
			}
			canonical := Marshal(&got)
			var decoded UblkParams
			if err := Unmarshal(canonical, &decoded); err != nil {
				t.Fatal(err)
			}
			want := got
			want.Len = uint32(len(canonical))
			if decoded != want {
				t.Fatal("canonical round trip changed known fields")
			}
			for _, b := range paramBlocks {
				if got.Types&b.bit != 0 && !bytes.Equal(canonical[b.start:b.end], data[b.start:b.end]) {
					t.Fatal("selected block moved or changed")
				}
			}
		}
		if !bytes.Equal(data, input) {
			t.Fatal("Unmarshal modified input")
		}
		assertFuzzMarshalBounds(t, &got, int(capacity)%513)
	})
}

func assertFuzzMarshalBounds(t *testing.T, value interface{}, capacity int) {
	t.Helper()
	want := Marshal(value)
	buf := bytes.Repeat([]byte{0xa5}, capacity)
	before := bytes.Clone(buf)
	n, err := MarshalInto(value, buf)
	if capacity < len(want) {
		if n != 0 || err != ErrBufferTooSmall || !bytes.Equal(buf, before) {
			t.Fatalf("short marshal: n=%d err=%v", n, err)
		}
	} else if err != nil || n != len(want) || !bytes.Equal(buf[:n], want) || !bytes.Equal(buf[n:], before[n:]) {
		t.Fatalf("marshal bounds: n=%d err=%v", n, err)
	}
}
