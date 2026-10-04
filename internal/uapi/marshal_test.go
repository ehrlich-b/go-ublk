package uapi

import (
	"bytes"
	"reflect"
	"testing"
	"unsafe"
)

// ---------------------------------------------------------------------------
// Size assertions: every struct must match the kernel UAPI size it mirrors.
// The sizes below are the canonical Linux ABI sizes (include/uapi/linux/ublk_cmd.h).
// ---------------------------------------------------------------------------

func TestUnitUAPISizes(t *testing.T) {
	ctrlCmdSize := unsafe.Sizeof(UblksrvCtrlCmd{})
	if ctrlCmdSize != 32 {
		t.Errorf("sizeof(ublksrv_ctrl_cmd) = %d, want 32 (must fit in the io_uring SQE cmd area)", ctrlCmdSize)
	}
	ioCmdSize := unsafe.Sizeof(UblksrvIOCmd{})
	if ioCmdSize != 16 {
		t.Errorf("sizeof(ublksrv_io_cmd) = %d, want 16", ioCmdSize)
	}
	devInfoSize := unsafe.Sizeof(UblksrvCtrlDevInfo{})
	if devInfoSize != 64 {
		t.Errorf("sizeof(ublksrv_ctrl_dev_info) = %d, want 64 (kernel 6.6+)", devInfoSize)
	}
	ioDescSize := unsafe.Sizeof(UblksrvIODesc{})
	if ioDescSize != 24 {
		t.Errorf("sizeof(ublksrv_io_desc) = %d, want 24", ioDescSize)
	}
	if got := unsafe.Sizeof(UblkParamBasic{}); got != 32 {
		t.Errorf("sizeof(ublk_param_basic) = %d, want 32", got)
	}
	if got := unsafe.Sizeof(UblkParamDiscard{}); got != 20 {
		t.Errorf("sizeof(ublk_param_discard) = %d, want 20", got)
	}
	if got := unsafe.Sizeof(UblkParamDevt{}); got != 16 {
		t.Errorf("sizeof(ublk_param_devt) = %d, want 16", got)
	}
	if got := unsafe.Sizeof(UblkParamZoned{}); got != 32 {
		t.Errorf("sizeof(ublk_param_zoned) = %d, want 32", got)
	}

	if got := unsafe.Sizeof(UblkParamDMAAlign{}); got != 8 {
		t.Errorf("sizeof(ublk_param_dma_align) = %d, want 8", got)
	}
	if got := unsafe.Sizeof(UblkParamSegment{}); got != 16 {
		t.Errorf("sizeof(ublk_param_segment) = %d, want 16", got)
	}
	if got := unsafe.Sizeof(UblkParamIntegrity{}); got != 16 {
		t.Errorf("sizeof(ublk_param_integrity) = %d, want 16", got)
	}

	// v7.3 ublk_params is 152 bytes on LP64 with no tail padding, but a
	// 4-byte hole at 116 before the 8-byte-aligned segment block. The Go
	// struct only matches that on 64-bit targets; the wire format never
	// depends on it (marshal.go copies blocks to fixed offsets).
	if unsafe.Sizeof(uintptr(0)) == 8 {
		if got := unsafe.Sizeof(UblkParams{}); got != 152 {
			t.Errorf("sizeof(UblkParams) = %d, want 152", got)
		}
	}
	full := &UblkParams{
		Types: UBLK_PARAM_TYPE_BASIC | UBLK_PARAM_TYPE_DISCARD |
			UBLK_PARAM_TYPE_DEVT | UBLK_PARAM_TYPE_ZONED,
	}
	if got := len(Marshal(full)); got != 108 {
		t.Errorf("marshaled pre-v6.15 ublk_params wire size = %d, want 108", got)
	}
	full.Types |= UBLK_PARAM_TYPE_DMA_ALIGN | UBLK_PARAM_TYPE_SEGMENT | UBLK_PARAM_TYPE_INTEGRITY
	if got := len(Marshal(full)); got != UblkParamsSize || UblkParamsSize != 152 {
		t.Errorf("marshaled v7.3 ublk_params wire size = %d (UblkParamsSize %d), want 152", got, UblkParamsSize)
	}
	for _, s := range []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"ublk_elem_header", unsafe.Sizeof(UblkElemHeader{}), 8},
		{"ublk_batch_io", unsafe.Sizeof(UblkBatchIO{}), 16},
		{"ublk_auto_buf_reg", unsafe.Sizeof(UblkAutoBufReg{}), 8},
		{"ublk_shmem_buf_reg", unsafe.Sizeof(UblkShmemBufReg{}), 24},
	} {
		if s.got != s.want {
			t.Errorf("sizeof(%s) = %d, want %d", s.name, s.got, s.want)
		}
	}
}

// TestUnitParamBlockOffsets pins every field of the UblkParams payload to the
// byte offset where the kernel reads it. ublk_param_basic fields are read by
// the block layer directly out of the SET_PARAMS buffer.
func TestUnitParamBlockOffsets(t *testing.T) {
	b := UblkParamBasic{}
	basic := []struct {
		name   string
		offset uintptr
		want   uintptr
	}{
		{"Attrs", unsafe.Offsetof(b.Attrs), 0},
		{"LogicalBSShift", unsafe.Offsetof(b.LogicalBSShift), 4},
		{"PhysicalBSShift", unsafe.Offsetof(b.PhysicalBSShift), 5},
		{"IOOptShift", unsafe.Offsetof(b.IOOptShift), 6},
		{"IOMinShift", unsafe.Offsetof(b.IOMinShift), 7},
		{"MaxSectors", unsafe.Offsetof(b.MaxSectors), 8},
		{"ChunkSectors", unsafe.Offsetof(b.ChunkSectors), 12},
		{"DevSectors", unsafe.Offsetof(b.DevSectors), 16},
		{"VirtBoundaryMask", unsafe.Offsetof(b.VirtBoundaryMask), 24},
	}
	for _, f := range basic {
		if f.offset != f.want {
			t.Errorf("ublk_param_basic.%s offset = %d, want %d", f.name, f.offset, f.want)
		}
	}

	d := UblkParamDiscard{}
	discard := []struct {
		name   string
		offset uintptr
		want   uintptr
	}{
		{"DiscardAlignment", unsafe.Offsetof(d.DiscardAlignment), 0},
		{"DiscardGranularity", unsafe.Offsetof(d.DiscardGranularity), 4},
		{"MaxDiscardSectors", unsafe.Offsetof(d.MaxDiscardSectors), 8},
		{"MaxWriteZeroesSectors", unsafe.Offsetof(d.MaxWriteZeroesSectors), 12},
		{"MaxDiscardSegments", unsafe.Offsetof(d.MaxDiscardSegments), 16},
		{"Reserved0", unsafe.Offsetof(d.Reserved0), 18},
	}
	for _, f := range discard {
		if f.offset != f.want {
			t.Errorf("ublk_param_discard.%s offset = %d, want %d", f.name, f.offset, f.want)
		}
	}

	p := UblkParams{}
	params := []struct {
		name   string
		offset uintptr
		want   uintptr
	}{
		{"Len", unsafe.Offsetof(p.Len), 0},
		{"Types", unsafe.Offsetof(p.Types), 4},
		{"Basic", unsafe.Offsetof(p.Basic), 8},
		{"Discard", unsafe.Offsetof(p.Discard), 40},
		{"Devt", unsafe.Offsetof(p.Devt), 60},
		{"Zoned", unsafe.Offsetof(p.Zoned), 76},
		{"DMA", unsafe.Offsetof(p.DMA), 108},
		{"Seg", unsafe.Offsetof(p.Seg), 120},
		{"Integrity", unsafe.Offsetof(p.Integrity), 136},
	}
	for _, f := range params {
		if unsafe.Sizeof(uintptr(0)) != 8 && f.want > 108 {
			continue // 32-bit Go aligns uint64 to 4; the wire offsets are fixed constants
		}
		if f.offset != f.want {
			t.Errorf("ublk_params.%s offset = %d, want %d", f.name, f.offset, f.want)
		}
	}
	// The marshaler's wire offsets, independent of the Go layout.
	wire := []struct {
		name       string
		start, end int
		want       [2]int
	}{
		{"basic", paramsBasicOffset, paramsBasicEnd, [2]int{8, 40}},
		{"discard", paramsDiscardOffset, paramsDiscardEnd, [2]int{40, 60}},
		{"devt", paramsDevtOffset, paramsDevtEnd, [2]int{60, 76}},
		{"zoned", paramsZonedOffset, paramsZonedEnd, [2]int{76, 108}},
		{"dma", paramsDMAOffset, paramsDMAEnd, [2]int{108, 116}},
		{"seg", paramsSegOffset, paramsSegEnd, [2]int{120, 136}},
		{"integrity", paramsIntegrityOffset, paramsIntegrityEnd, [2]int{136, 152}},
	}
	for _, w := range wire {
		if [2]int{w.start, w.end} != w.want {
			t.Errorf("wire %s block = [%d,%d), want %v", w.name, w.start, w.end, w.want)
		}
	}
}

// TestGoldenUblkParamsV73Blocks pins the three v6.15+/v7.0 blocks, including
// the zero hole at 116..120 that the C compiler inserts before seg.
func TestGoldenUblkParamsV73Blocks(t *testing.T) {
	p := &UblkParams{
		Types:     UBLK_PARAM_TYPE_DMA_ALIGN | UBLK_PARAM_TYPE_SEGMENT | UBLK_PARAM_TYPE_INTEGRITY,
		Basic:     UblkParamBasic{Attrs: 0xffffffff}, // not selected: must not appear
		DMA:       UblkParamDMAAlign{Alignment: 0x1ff, Pad: [4]uint8{1, 2, 3, 4}},
		Seg:       UblkParamSegment{SegBoundaryMask: 0x0102030405060708, MaxSegmentSize: 0x11223344, MaxSegments: 0x5566, Pad: [2]uint8{7, 8}},
		Integrity: UblkParamIntegrity{Flags: LBMD_PI_CAP_INTEGRITY | LBMD_PI_CAP_REFTAG, MaxIntegritySegments: 0x0a0b, IntervalExp: 12, MetadataSize: 8, PIOffset: 0, CsumType: LBMD_PI_CSUM_CRC16_T10DIF, TagSize: 2, Pad: [5]uint8{9, 9, 9, 9, 9}},
	}
	want := make([]byte, 152)
	want[0] = 152 // Len
	want[4] = 0x70
	// dma @108: alignment LE, then pad bytes.
	copy(want[108:116], []byte{0xff, 0x01, 0, 0, 1, 2, 3, 4})
	// 116..120 stays zero.
	// seg @120: mask LE, max_segment_size LE, max_segments LE, pad.
	copy(want[120:136], []byte{8, 7, 6, 5, 4, 3, 2, 1, 0x44, 0x33, 0x22, 0x11, 0x66, 0x55, 7, 8})
	// integrity @136: flags LE, max_integrity_segments LE, interval_exp,
	// metadata_size, pi_offset, csum_type, tag_size, pad[5].
	copy(want[136:152], []byte{3, 0, 0, 0, 0x0b, 0x0a, 12, 8, 0, 2, 2, 9, 9, 9, 9, 9})
	got := Marshal(p)
	if !bytes.Equal(got, want) {
		t.Fatalf("Marshal = %x\nwant      %x", got, want)
	}
	assertMarshalIntoMatches(t, p, got)
	var back UblkParams
	if err := Unmarshal(got, &back); err != nil {
		t.Fatal(err)
	}
	p.Basic = UblkParamBasic{}
	p.Len = 152
	if back != *p {
		t.Fatalf("round trip = %+v, want %+v", back, *p)
	}
}

// ---------------------------------------------------------------------------
// Golden-byte tests. Expected bytes are written literally and derived from the
// struct layouts in structs.go: every offset, size and endianness below is
// annotated. All UAPI wire data is little-endian.
// ---------------------------------------------------------------------------

func TestGoldenUblksrvCtrlCmd(t *testing.T) {
	tests := []struct {
		name string
		cmd  *UblksrvCtrlCmd
		want []byte
	}{
		{
			name: "distinct values",
			cmd: &UblksrvCtrlCmd{
				DevID:      0xDEADBEEF,
				QueueID:    0x07FF,
				Len:        0x0102,
				Addr:       0x0102030405060708,
				Data:       0xF0F1F2F3F4F5F6F7,
				DevPathLen: 0x0007,
				Pad:        0x0A0B,
				Reserved:   0xC0FFEE00,
			},
			want: []byte{
				// DevID uint32 @0 (LE)
				0xEF, 0xBE, 0xAD, 0xDE,
				// QueueID uint16 @4 (LE)
				0xFF, 0x07,
				// Len uint16 @6 (LE)
				0x02, 0x01,
				// Addr uint64 @8 (LE)
				0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01,
				// Data uint64 @16 (LE)
				0xF7, 0xF6, 0xF5, 0xF4, 0xF3, 0xF2, 0xF1, 0xF0,
				// DevPathLen uint16 @24 (LE)
				0x07, 0x00,
				// Pad uint16 @26 (LE)
				0x0B, 0x0A,
				// Reserved uint32 @28 (LE)
				0x00, 0xEE, 0xFF, 0xC0,
			},
		},
		{
			name: "zero value",
			cmd:  &UblksrvCtrlCmd{},
			want: make([]byte, 32),
		},
		{
			name: "max value",
			cmd: &UblksrvCtrlCmd{
				DevID:      ^uint32(0),
				QueueID:    ^uint16(0),
				Len:        ^uint16(0),
				Addr:       ^uint64(0),
				Data:       ^uint64(0),
				DevPathLen: ^uint16(0),
				Pad:        ^uint16(0),
				Reserved:   ^uint32(0),
			},
			want: bytes.Repeat([]byte{0xFF}, 32),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Marshal(tt.cmd)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("Marshal(CtrlCmd) =\n  %x\nwant\n  %x", got, tt.want)
			}
			assertMarshalIntoMatches(t, tt.cmd, got)
		})
	}
}

func TestGoldenUblksrvIOCmd(t *testing.T) {
	tests := []struct {
		name string
		cmd  *UblksrvIOCmd
		want []byte
	}{
		{
			name: "distinct values",
			cmd: &UblksrvIOCmd{
				QID:    0x0003,
				Tag:    0x00FF,
				Result: 0x11223344,
				Addr:   0x1020304050607080,
			},
			want: []byte{
				// QID uint16 @0 (LE)
				0x03, 0x00,
				// Tag uint16 @2 (LE)
				0xFF, 0x00,
				// Result int32 @4 (LE)
				0x44, 0x33, 0x22, 0x11,
				// Addr uint64 @8 (LE)
				0x80, 0x70, 0x60, 0x50, 0x40, 0x30, 0x20, 0x10,
			},
		},
		{
			name: "zero value",
			cmd:  &UblksrvIOCmd{},
			want: make([]byte, 16),
		},
		{
			name: "max value",
			cmd: &UblksrvIOCmd{
				QID:    ^uint16(0),
				Tag:    ^uint16(0),
				Result: 0x7FFFFFFF,
				Addr:   ^uint64(0),
			},
			want: []byte{
				// QID uint16 @0 (LE)
				0xFF, 0xFF,
				// Tag uint16 @2 (LE)
				0xFF, 0xFF,
				// Result int32 @4 (LE), int32 max 0x7FFFFFFF
				0xFF, 0xFF, 0xFF, 0x7F,
				// Addr uint64 @8 (LE)
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Marshal(tt.cmd)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("Marshal(IOCmd) =\n  %x\nwant\n  %x", got, tt.want)
			}
			assertMarshalIntoMatches(t, tt.cmd, got)
		})
	}
}

func TestGoldenUblksrvCtrlDevInfo(t *testing.T) {
	tests := []struct {
		name string
		info *UblksrvCtrlDevInfo
		want []byte
	}{
		{
			name: "distinct values",
			info: &UblksrvCtrlDevInfo{
				NrHwQueues:    4,
				QueueDepth:    256,
				State:         1,
				MaxIOBufBytes: 0x00100000,
				DevID:         9,
				UblksrvPID:    12345,
				Flags:         0x42,
				UblksrvFlags:  0x0A0B0C0D0E0F1011,
				OwnerUID:      1000,
				OwnerGID:      1001,
				Reserved1:     0xFEEDFACECAFEBEEF,
				Reserved2:     0x0123456789ABCDEF,
			},
			want: []byte{
				// NrHwQueues uint16 @0 (LE)
				0x04, 0x00,
				// QueueDepth uint16 @2 (LE), 256 = 0x0100
				0x00, 0x01,
				// State uint16 @4 (LE), UBLK_S_DEV_LIVE = 1
				0x01, 0x00,
				// IODescSize uint16 @6 (LE), formerly pad0
				0x00, 0x00,
				// MaxIOBufBytes uint32 @8 (LE)
				0x00, 0x00, 0x10, 0x00,
				// DevID uint32 @12 (LE)
				0x09, 0x00, 0x00, 0x00,
				// UblksrvPID int32 @16 (LE), 12345 = 0x00003039
				0x39, 0x30, 0x00, 0x00,
				// Pad1 uint32 @20 (LE)
				0x00, 0x00, 0x00, 0x00,
				// Flags uint64 @24 (LE)
				0x42, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
				// UblksrvFlags uint64 @32 (LE)
				0x11, 0x10, 0x0F, 0x0E, 0x0D, 0x0C, 0x0B, 0x0A,
				// OwnerUID uint32 @40 (LE), 1000 = 0x3E8
				0xE8, 0x03, 0x00, 0x00,
				// OwnerGID uint32 @44 (LE), 1001 = 0x3E9
				0xE9, 0x03, 0x00, 0x00,
				// Reserved1 uint64 @48 (LE)
				0xEF, 0xBE, 0xFE, 0xCA, 0xCE, 0xFA, 0xED, 0xFE,
				// Reserved2 uint64 @56 (LE)
				0xEF, 0xCD, 0xAB, 0x89, 0x67, 0x45, 0x23, 0x01,
			},
		},
		{
			name: "zero value",
			info: &UblksrvCtrlDevInfo{},
			want: make([]byte, 64),
		},
		{
			name: "max value",
			info: &UblksrvCtrlDevInfo{
				NrHwQueues:    ^uint16(0),
				QueueDepth:    ^uint16(0),
				State:         ^uint16(0),
				IODescSize:    ^uint16(0),
				MaxIOBufBytes: ^uint32(0),
				DevID:         ^uint32(0),
				UblksrvPID:    0x7FFFFFFF,
				Pad1:          ^uint32(0),
				Flags:         ^uint64(0),
				UblksrvFlags:  ^uint64(0),
				OwnerUID:      ^uint32(0),
				OwnerGID:      ^uint32(0),
				Reserved1:     ^uint64(0),
				Reserved2:     ^uint64(0),
			},
			want: []byte{
				// NrHwQueues..IODescSize @0..8 (4x uint16 LE, all max)
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				// MaxIOBufBytes uint32 @8, DevID uint32 @12
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				// UblksrvPID int32 @16, int32 max 0x7FFFFFFF
				0xFF, 0xFF, 0xFF, 0x7F,
				// Pad1 uint32 @20
				0xFF, 0xFF, 0xFF, 0xFF,
				// Flags uint64 @24
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				// UblksrvFlags uint64 @32
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				// OwnerUID uint32 @40, OwnerGID uint32 @44
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				// Reserved1 uint64 @48
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				// Reserved2 uint64 @56
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Marshal(tt.info)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("Marshal(CtrlDevInfo) =\n  %x\nwant\n  %x", got, tt.want)
			}
			assertMarshalIntoMatches(t, tt.info, got)
		})
	}
}

func TestGoldenUblkParams(t *testing.T) {
	const fullTypes = UBLK_PARAM_TYPE_BASIC | UBLK_PARAM_TYPE_DISCARD |
		UBLK_PARAM_TYPE_DEVT | UBLK_PARAM_TYPE_ZONED

	tests := []struct {
		name   string
		params *UblkParams
		want   []byte
	}{
		{
			name: "distinct values all blocks",
			params: &UblkParams{
				Types: fullTypes,
				Basic: UblkParamBasic{
					Attrs:            UBLK_ATTR_ROTATIONAL | UBLK_ATTR_VOLATILE_CACHE,
					LogicalBSShift:   9,
					PhysicalBSShift:  12,
					IOOptShift:       12,
					IOMinShift:       9,
					MaxSectors:       8192,
					ChunkSectors:     4096,
					DevSectors:       0x0001000000000000,
					VirtBoundaryMask: ^uint64(0),
				},
				Discard: UblkParamDiscard{
					DiscardAlignment:      0x0200,
					DiscardGranularity:    4096,
					MaxDiscardSectors:     1048576,
					MaxWriteZeroesSectors: 1048576,
					MaxDiscardSegments:    1,
					Reserved0:             0xABCD,
				},
				Devt: UblkParamDevt{
					CharMajor: 259,
					CharMinor: 0,
					DiskMajor: 259,
					DiskMinor: 0,
				},
				Zoned: UblkParamZoned{
					MaxOpenZones:         16,
					MaxActiveZones:       32,
					MaxZoneAppendSectors: 512,
					Reserved:             [20]uint8{0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A},
				},
			},
			want: []byte{
				// Len uint32 @0 (LE) = wire size 108 (0x6C)
				0x6C, 0x00, 0x00, 0x00,
				// Types uint32 @4 (LE) = BASIC|DISCARD|DEVT|ZONED (0x0F)
				0x0F, 0x00, 0x00, 0x00,
				// ublk_param_basic @8..40 (32 bytes, native LE)
				//   Attrs uint32 @+0
				0x06, 0x00, 0x00, 0x00,
				//   LogicalBSShift uint8 @+4
				0x09,
				//   PhysicalBSShift uint8 @+5
				0x0C,
				//   IOOptShift uint8 @+6
				0x0C,
				//   IOMinShift uint8 @+7
				0x09,
				//   MaxSectors uint32 @+8, 8192 = 0x2000
				0x00, 0x20, 0x00, 0x00,
				//   ChunkSectors uint32 @+12, 4096 = 0x1000
				0x00, 0x10, 0x00, 0x00,
				//   DevSectors uint64 @+16
				0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x00,
				//   VirtBoundaryMask uint64 @+24
				0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF,
				// ublk_param_discard @40..60 (20 bytes, native LE)
				//   DiscardAlignment uint32 @+0
				0x00, 0x02, 0x00, 0x00,
				//   DiscardGranularity uint32 @+4, 4096 = 0x1000
				0x00, 0x10, 0x00, 0x00,
				//   MaxDiscardSectors uint32 @+8, 1048576 = 0x100000
				0x00, 0x00, 0x10, 0x00,
				//   MaxWriteZeroesSectors uint32 @+12, 1048576 = 0x100000
				0x00, 0x00, 0x10, 0x00,
				//   MaxDiscardSegments uint16 @+16
				0x01, 0x00,
				//   Reserved0 uint16 @+18
				0xCD, 0xAB,
				// ublk_param_devt @60..76 (16 bytes, native LE)
				//   CharMajor uint32 @+0, 259 = 0x0103
				0x03, 0x01, 0x00, 0x00,
				//   CharMinor uint32 @+4
				0x00, 0x00, 0x00, 0x00,
				//   DiskMajor uint32 @+8, 259 = 0x0103
				0x03, 0x01, 0x00, 0x00,
				//   DiskMinor uint32 @+12
				0x00, 0x00, 0x00, 0x00,
				// ublk_param_zoned @76..108 (32 bytes, native LE)
				//   MaxOpenZones uint32 @+0
				0x10, 0x00, 0x00, 0x00,
				//   MaxActiveZones uint32 @+4
				0x20, 0x00, 0x00, 0x00,
				//   MaxZoneAppendSectors uint32 @+8, 512 = 0x0200
				0x00, 0x02, 0x00, 0x00,
				//   Reserved [20]uint8 @+12
				0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A,
				0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A,
				0x5A, 0x5A, 0x5A, 0x5A,
			},
		},
		{
			name:   "zero value no blocks",
			params: &UblkParams{},
			want: []byte{
				// Len uint32 @0 (LE) = 8 (len + types only)
				0x08, 0x00, 0x00, 0x00,
				// Types uint32 @4 (LE) = 0
				0x00, 0x00, 0x00, 0x00,
			},
		},
		{
			name: "max value all blocks",
			params: &UblkParams{
				Types: fullTypes,
				Basic: UblkParamBasic{
					Attrs:            ^uint32(0),
					LogicalBSShift:   0xFF,
					PhysicalBSShift:  0xFF,
					IOOptShift:       0xFF,
					IOMinShift:       0xFF,
					MaxSectors:       ^uint32(0),
					ChunkSectors:     ^uint32(0),
					DevSectors:       ^uint64(0),
					VirtBoundaryMask: ^uint64(0),
				},
				Discard: UblkParamDiscard{
					DiscardAlignment:      ^uint32(0),
					DiscardGranularity:    ^uint32(0),
					MaxDiscardSectors:     ^uint32(0),
					MaxWriteZeroesSectors: ^uint32(0),
					MaxDiscardSegments:    ^uint16(0),
					Reserved0:             ^uint16(0),
				},
				Devt: UblkParamDevt{
					CharMajor: ^uint32(0),
					CharMinor: ^uint32(0),
					DiskMajor: ^uint32(0),
					DiskMinor: ^uint32(0),
				},
				Zoned: UblkParamZoned{
					MaxOpenZones:         ^uint32(0),
					MaxActiveZones:       ^uint32(0),
					MaxZoneAppendSectors: ^uint32(0),
					Reserved:             [20]uint8{0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF, 0xFF},
				},
			},
			want: append(
				// Len uint32 @0 (LE) = wire size 108 (0x6C)
				[]byte{0x6C, 0x00, 0x00, 0x00, 0x0F, 0x00, 0x00, 0x00},
				// basic (32), discard (20), devt (16), zoned (32): all max == 0xFF
				bytes.Repeat([]byte{0xFF}, 100)...,
			),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Marshal(tt.params)
			if !bytes.Equal(got, tt.want) {
				t.Errorf("Marshal(Params) =\n  %x\nwant\n  %x", got, tt.want)
			}
			assertMarshalIntoMatches(t, tt.params, got)
		})
	}
}

// TestGoldenUblksrvIODesc pins the raw memory layout of ublksrv_io_desc (24
// bytes) via the direct-copy marshal path. UblksrvIODesc has no dedicated
// marshal/unmarshal pair in this package, so this asserts its layout as the
// kernel sees it in shared memory.
func TestGoldenUblksrvIODesc(t *testing.T) {
	tests := []struct {
		name string
		desc *UblksrvIODesc
		want []byte
	}{
		{
			name: "distinct values",
			desc: &UblksrvIODesc{
				OpFlags:     0x88000105,
				NrSectors:   0x00020000,
				StartSector: 0x0102030405060708,
				Addr:        0xF0F1F2F3F4F5F6F7,
			},
			want: []byte{
				// OpFlags uint32 @0 (LE); op 5 = WRITE_ZEROES, flags 0x880001
				0x05, 0x01, 0x00, 0x88,
				// NrSectors uint32 @4 (LE)
				0x00, 0x00, 0x02, 0x00,
				// StartSector uint64 @8 (LE)
				0x08, 0x07, 0x06, 0x05, 0x04, 0x03, 0x02, 0x01,
				// Addr uint64 @16 (LE)
				0xF7, 0xF6, 0xF5, 0xF4, 0xF3, 0xF2, 0xF1, 0xF0,
			},
		},
		{
			name: "zero value",
			desc: &UblksrvIODesc{},
			want: make([]byte, 24),
		},
		{
			name: "max value",
			desc: &UblksrvIODesc{
				OpFlags:     ^uint32(0),
				NrSectors:   ^uint32(0),
				StartSector: ^uint64(0),
				Addr:        ^uint64(0),
			},
			want: bytes.Repeat([]byte{0xFF}, 24),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Marshal(tt.desc) // direct-copy fallback
			if !bytes.Equal(got, tt.want) {
				t.Errorf("Marshal(IODesc) =\n  %x\nwant\n  %x", got, tt.want)
			}
			assertMarshalIntoMatches(t, tt.desc, got)
		})
	}
}

// assertMarshalIntoMatches proves the zero-allocation path writes byte-for-byte
// the same output as Marshal, and that a short buffer returns ErrBufferTooSmall
// without touching the buffer.
func assertMarshalIntoMatches(t *testing.T, v interface{}, want []byte) {
	t.Helper()
	buf := make([]byte, len(want)+8)
	n, err := MarshalInto(v, buf)
	if err != nil {
		t.Fatalf("MarshalInto(%T) error: %v", v, err)
	}
	if n != len(want) {
		t.Errorf("MarshalInto(%T) wrote %d bytes, want %d", v, n, len(want))
	}
	if !bytes.Equal(buf[:n], want) {
		t.Errorf("MarshalInto(%T) =\n  %x\nwant\n  %x", v, buf[:n], want)
	}

	tooSmall := make([]byte, len(want)/2)
	for i := range tooSmall {
		tooSmall[i] = 0xAA
	}
	before := append([]byte(nil), tooSmall...)
	if _, err := MarshalInto(v, tooSmall); err != ErrBufferTooSmall {
		t.Errorf("MarshalInto(%T) short buffer error = %v, want ErrBufferTooSmall", v, err)
	}
	if !bytes.Equal(tooSmall, before) {
		t.Errorf("MarshalInto(%T) modified the buffer on a short-buffer error", v)
	}
}

// ---------------------------------------------------------------------------
// Round-trip tests: Marshal then Unmarshal must reproduce the original struct.
// ---------------------------------------------------------------------------

func TestRoundTripUblksrvCtrlCmd(t *testing.T) {
	tests := []struct {
		name string
		cmd  *UblksrvCtrlCmd
	}{
		{"distinct", &UblksrvCtrlCmd{DevID: 0xDEADBEEF, QueueID: 0x07FF, Len: 0x0102, Addr: 0x0102030405060708, Data: 0xF0F1F2F3F4F5F6F7, DevPathLen: 7, Pad: 0x0A0B, Reserved: 0xC0FFEE00}},
		{"zero", &UblksrvCtrlCmd{}},
		{"max", &UblksrvCtrlCmd{DevID: ^uint32(0), QueueID: ^uint16(0), Len: ^uint16(0), Addr: ^uint64(0), Data: ^uint64(0), DevPathLen: ^uint16(0), Pad: ^uint16(0), Reserved: ^uint32(0)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			back := &UblksrvCtrlCmd{}
			if err := Unmarshal(Marshal(tt.cmd), back); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			if !reflect.DeepEqual(tt.cmd, back) {
				t.Errorf("round-trip mismatch:\n  got  %+v\n  want %+v", back, tt.cmd)
			}
		})
	}
}

func TestRoundTripUblksrvIOCmd(t *testing.T) {
	tests := []struct {
		name string
		cmd  *UblksrvIOCmd
	}{
		{"distinct", &UblksrvIOCmd{QID: 0x0003, Tag: 0x00FF, Result: 0x11223344, Addr: 0x1020304050607080}},
		{"zero", &UblksrvIOCmd{}},
		{"max", &UblksrvIOCmd{QID: ^uint16(0), Tag: ^uint16(0), Result: 0x7FFFFFFF, Addr: ^uint64(0)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			back := &UblksrvIOCmd{}
			if err := Unmarshal(Marshal(tt.cmd), back); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			if !reflect.DeepEqual(tt.cmd, back) {
				t.Errorf("round-trip mismatch:\n  got  %+v\n  want %+v", back, tt.cmd)
			}
		})
	}
}

func TestRoundTripUblksrvCtrlDevInfo(t *testing.T) {
	distinct := &UblksrvCtrlDevInfo{
		NrHwQueues: 4, QueueDepth: 256, State: 1, MaxIOBufBytes: 0x00100000,
		DevID: 9, UblksrvPID: 12345, Flags: 0x42, UblksrvFlags: 0x0A0B0C0D0E0F1011,
		OwnerUID: 1000, OwnerGID: 1001, Reserved1: 0xFEEDFACECAFEBEEF, Reserved2: 0x0123456789ABCDEF,
	}
	tests := []struct {
		name string
		info *UblksrvCtrlDevInfo
	}{
		{"distinct", distinct},
		{"zero", &UblksrvCtrlDevInfo{}},
		{"max", &UblksrvCtrlDevInfo{NrHwQueues: ^uint16(0), QueueDepth: ^uint16(0), State: ^uint16(0), IODescSize: ^uint16(0), MaxIOBufBytes: ^uint32(0), DevID: ^uint32(0), UblksrvPID: 0x7FFFFFFF, Pad1: ^uint32(0), Flags: ^uint64(0), UblksrvFlags: ^uint64(0), OwnerUID: ^uint32(0), OwnerGID: ^uint32(0), Reserved1: ^uint64(0), Reserved2: ^uint64(0)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			back := &UblksrvCtrlDevInfo{}
			if err := Unmarshal(Marshal(tt.info), back); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			if !reflect.DeepEqual(tt.info, back) {
				t.Errorf("round-trip mismatch:\n  got  %+v\n  want %+v", back, tt.info)
			}
		})
	}
}

func TestRoundTripUblkParams(t *testing.T) {
	const fullTypes = UBLK_PARAM_TYPE_BASIC | UBLK_PARAM_TYPE_DISCARD |
		UBLK_PARAM_TYPE_DEVT | UBLK_PARAM_TYPE_ZONED

	tests := []struct {
		name   string
		params *UblkParams
	}{
		{
			"all blocks",
			&UblkParams{
				Types:   fullTypes,
				Basic:   UblkParamBasic{Attrs: UBLK_ATTR_ROTATIONAL, LogicalBSShift: 9, MaxSectors: 8192, DevSectors: 1 << 30},
				Discard: UblkParamDiscard{DiscardGranularity: 4096, MaxDiscardSectors: 2048, MaxWriteZeroesSectors: 2048, MaxDiscardSegments: 1},
				Devt:    UblkParamDevt{CharMajor: 259, DiskMajor: 259},
				Zoned:   UblkParamZoned{MaxOpenZones: 16, MaxActiveZones: 32, MaxZoneAppendSectors: 512, Reserved: [20]uint8{1, 2, 3}},
			},
		},
		{"basic only", &UblkParams{Types: UBLK_PARAM_TYPE_BASIC, Basic: UblkParamBasic{Attrs: UBLK_ATTR_READ_ONLY, LogicalBSShift: 12}}},
		{"no blocks", &UblkParams{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			buf := Marshal(tt.params)
			back := &UblkParams{}
			if err := Unmarshal(buf, back); err != nil {
				t.Fatalf("Unmarshal error: %v", err)
			}
			want := *tt.params
			// Marshal recomputes Len as the wire size; Unmarshal restores it.
			want.Len = uint32(len(buf))
			if !reflect.DeepEqual(*back, want) {
				t.Errorf("round-trip mismatch:\n  got  %+v\n  want %+v", *back, want)
			}
		})
	}
}

// TestUnitUnmarshalErrors checks the error paths of Unmarshal and the
// MarshalInto buffer contract.
func TestUnitUnmarshalErrors(t *testing.T) {
	t.Run("ctrl cmd short buffer", func(t *testing.T) {
		cmd := &UblksrvCtrlCmd{}
		if err := Unmarshal(make([]byte, 16), cmd); err != ErrInsufficientData {
			t.Errorf("err = %v, want ErrInsufficientData", err)
		}
	})
	t.Run("io cmd short buffer", func(t *testing.T) {
		cmd := &UblksrvIOCmd{}
		if err := Unmarshal(make([]byte, 8), cmd); err != ErrInsufficientData {
			t.Errorf("err = %v, want ErrInsufficientData", err)
		}
	})
	t.Run("params short buffer", func(t *testing.T) {
		params := &UblkParams{}
		if err := Unmarshal(make([]byte, 4), params); err != ErrInsufficientData {
			t.Errorf("err = %v, want ErrInsufficientData", err)
		}
	})
	t.Run("dev info short buffer", func(t *testing.T) {
		info := &UblksrvCtrlDevInfo{}
		if err := Unmarshal(make([]byte, 40), info); err != ErrInsufficientData {
			t.Errorf("err = %v, want ErrInsufficientData", err)
		}
	})
	t.Run("marshal into nil buffer", func(t *testing.T) {
		if _, err := MarshalInto(&UblksrvCtrlCmd{}, nil); err != ErrBufferTooSmall {
			t.Errorf("err = %v, want ErrBufferTooSmall", err)
		}
	})
}
