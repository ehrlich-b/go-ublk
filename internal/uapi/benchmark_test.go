package uapi

import "testing"

func benchmarkCtrlCmd() *UblksrvCtrlCmd {
	return &UblksrvCtrlCmd{
		DevID:      0xDEADBEEF,
		QueueID:    0x07FF,
		Len:        0x0102,
		Addr:       0x0102030405060708,
		Data:       0xF0F1F2F3F4F5F6F7,
		DevPathLen: 0x0007,
		Pad:        0x0A0B,
		Reserved:   0xC0FFEE00,
	}
}

func benchmarkIOCmd() *UblksrvIOCmd {
	return &UblksrvIOCmd{
		QID:    0x0003,
		Tag:    0x00FF,
		Result: 0x11223344,
		Addr:   0x1020304050607080,
	}
}

func benchmarkDevInfo() *UblksrvCtrlDevInfo {
	return &UblksrvCtrlDevInfo{
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
	}
}

func benchmarkParams() *UblkParams {
	return &UblkParams{
		Types: UBLK_PARAM_TYPE_BASIC | UBLK_PARAM_TYPE_DISCARD |
			UBLK_PARAM_TYPE_DEVT | UBLK_PARAM_TYPE_ZONED,
		Basic: UblkParamBasic{
			Attrs:            UBLK_ATTR_ROTATIONAL | UBLK_ATTR_VOLATILE_CACHE,
			LogicalBSShift:   9,
			PhysicalBSShift:  12,
			IOOptShift:       12,
			IOMinShift:       9,
			MaxSectors:       8192,
			ChunkSectors:     4096,
			DevSectors:       0x0001000000000000,
			VirtBoundaryMask: 0xFFFFFFFFFFFFFFFF,
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
			DiskMajor: 259,
		},
		Zoned: UblkParamZoned{
			MaxOpenZones:         16,
			MaxActiveZones:       32,
			MaxZoneAppendSectors: 512,
			Reserved:             [20]uint8{0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A, 0x5A},
		},
	}
}

// Benchmarks of the current allocating Marshal API. MarshalInto variants are
// added alongside once the zero-allocation path exists; these establish the
// allocs/op baseline the rider asks us to prove.
func BenchmarkMarshalCtrlCmd(b *testing.B) {
	cmd := benchmarkCtrlCmd()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Marshal(cmd)
	}
}

func BenchmarkMarshalIOCmd(b *testing.B) {
	cmd := benchmarkIOCmd()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Marshal(cmd)
	}
}

func BenchmarkMarshalCtrlDevInfo(b *testing.B) {
	info := benchmarkDevInfo()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Marshal(info)
	}
}

func BenchmarkMarshalParams(b *testing.B) {
	params := benchmarkParams()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Marshal(params)
	}
}

func BenchmarkMarshalIODescFallback(b *testing.B) {
	desc := &UblksrvIODesc{OpFlags: 0x101, NrSectors: 8, StartSector: 0, Addr: 0x4000}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = Marshal(desc)
	}
}

// Zero-allocation variants: the buffer is allocated once up front and reused
// across iterations, so allocs/op must drop to 0.
func BenchmarkMarshalIntoCtrlCmd(b *testing.B) {
	cmd := benchmarkCtrlCmd()
	buf := make([]byte, 32)
	b.ReportAllocs()
	b.SetBytes(32)
	for i := 0; i < b.N; i++ {
		if _, err := MarshalInto(cmd, buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalIntoIOCmd(b *testing.B) {
	cmd := benchmarkIOCmd()
	buf := make([]byte, 16)
	b.ReportAllocs()
	b.SetBytes(16)
	for i := 0; i < b.N; i++ {
		if _, err := MarshalInto(cmd, buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalIntoCtrlDevInfo(b *testing.B) {
	info := benchmarkDevInfo()
	buf := make([]byte, 64)
	b.ReportAllocs()
	b.SetBytes(64)
	for i := 0; i < b.N; i++ {
		if _, err := MarshalInto(info, buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalIntoParams(b *testing.B) {
	params := benchmarkParams()
	buf := make([]byte, paramsSize(params))
	b.ReportAllocs()
	b.SetBytes(108)
	for i := 0; i < b.N; i++ {
		if _, err := MarshalInto(params, buf); err != nil {
			b.Fatal(err)
		}
	}
}

func BenchmarkMarshalIntoIODescFallback(b *testing.B) {
	desc := &UblksrvIODesc{OpFlags: 0x101, NrSectors: 8, StartSector: 0, Addr: 0x4000}
	buf := make([]byte, 24)
	b.ReportAllocs()
	b.SetBytes(24)
	for i := 0; i < b.N; i++ {
		if _, err := MarshalInto(desc, buf); err != nil {
			b.Fatal(err)
		}
	}
}
