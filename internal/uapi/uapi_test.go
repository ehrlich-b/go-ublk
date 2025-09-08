package uapi

import (
	"testing"
	"unsafe"
)

// Test structure sizes match kernel expectations
func TestStructSizes(t *testing.T) {
	tests := []struct {
		name string
		size uintptr
		expected int
	}{
		{"UblksrvCtrlCmd", unsafe.Sizeof(UblksrvCtrlCmd{}), 48},
		{"UblksrvCtrlDevInfo", unsafe.Sizeof(UblksrvCtrlDevInfo{}), 80},
		{"UblksrvIODesc", unsafe.Sizeof(UblksrvIODesc{}), 32},
		{"UblksrvIOCmd", unsafe.Sizeof(UblksrvIOCmd{}), 16},
	}
	
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if int(tt.size) != tt.expected {
				t.Errorf("%s size = %d, want %d", tt.name, tt.size, tt.expected)
			}
		})
	}
}

// Test UblksrvIODesc helper methods
func TestIODescHelpers(t *testing.T) {
	desc := &UblksrvIODesc{
		OpFlags: (UBLK_IO_F_FUA << 8) | UBLK_IO_OP_WRITE,
	}
	
	if desc.GetOp() != UBLK_IO_OP_WRITE {
		t.Errorf("GetOp() = %d, want %d", desc.GetOp(), UBLK_IO_OP_WRITE)
	}
	
	if desc.GetFlags() != UBLK_IO_F_FUA {
		t.Errorf("GetFlags() = %d, want %d", desc.GetFlags(), UBLK_IO_F_FUA)
	}
}

// Test UblkParams helper methods
func TestParamsHelpers(t *testing.T) {
	params := &UblkParams{}
	
	// Test initial state
	if params.HasBasic() {
		t.Error("HasBasic() should be false initially")
	}
	
	// Test setting basic
	params.SetBasic()
	if !params.HasBasic() {
		t.Error("HasBasic() should be true after SetBasic()")
	}
	
	// Test multiple types
	params.SetDiscard()
	if !params.HasDiscard() {
		t.Error("HasDiscard() should be true after SetDiscard()")
	}
	
	if params.Types != (UBLK_PARAM_TYPE_BASIC | UBLK_PARAM_TYPE_DISCARD) {
		t.Errorf("Types = %d, want %d", params.Types, UBLK_PARAM_TYPE_BASIC|UBLK_PARAM_TYPE_DISCARD)
	}
}

// Test marshaling and unmarshaling
func TestMarshalUnmarshal(t *testing.T) {
	t.Run("UblksrvCtrlCmd", func(t *testing.T) {
		original := &UblksrvCtrlCmd{
			DevID:      42,
			QueueID:    0xFFFF, // -1 as uint16
			Len:        100,
			Addr:       0x123456789ABCDEF0,
			Data:       [1]uint64{0xDEADBEEF},
			DevPathLen: 0,
			Pad:        0,
			Reserved:   0,
		}
		
		data := Marshal(original)
		if len(data) != 48 {
			t.Errorf("Marshal length = %d, want 48", len(data))
		}
		
		var unmarshaled UblksrvCtrlCmd
		if err := Unmarshal(data, &unmarshaled); err != nil {
			t.Errorf("Unmarshal failed: %v", err)
		}
		
		if unmarshaled.DevID != original.DevID {
			t.Errorf("DevID = %d, want %d", unmarshaled.DevID, original.DevID)
		}
		if unmarshaled.QueueID != original.QueueID {
			t.Errorf("QueueID = %d, want %d", unmarshaled.QueueID, original.QueueID)
		}
		if unmarshaled.Addr != original.Addr {
			t.Errorf("Addr = %x, want %x", unmarshaled.Addr, original.Addr)
		}
	})
	
	t.Run("UblksrvIOCmd", func(t *testing.T) {
		original := &UblksrvIOCmd{
			QID:    1,
			Tag:    42,
			Result: -5, // -EIO
			Addr:   0x1000000000000000,
		}
		
		data := Marshal(original)
		if len(data) != 16 {
			t.Errorf("Marshal length = %d, want 16", len(data))
		}
		
		var unmarshaled UblksrvIOCmd
		if err := Unmarshal(data, &unmarshaled); err != nil {
			t.Errorf("Unmarshal failed: %v", err)
		}
		
		if unmarshaled.QID != original.QID {
			t.Errorf("QID = %d, want %d", unmarshaled.QID, original.QID)
		}
		if unmarshaled.Tag != original.Tag {
			t.Errorf("Tag = %d, want %d", unmarshaled.Tag, original.Tag)
		}
		if unmarshaled.Result != original.Result {
			t.Errorf("Result = %d, want %d", unmarshaled.Result, original.Result)
		}
		if unmarshaled.Addr != original.Addr {
			t.Errorf("Addr = %x, want %x", unmarshaled.Addr, original.Addr)
		}
	})
}

// Test ioctl encoding
func TestIoctlEncoding(t *testing.T) {
	// Test basic encoding
	cmd := IoctlEncode(_IOC_READ|_IOC_WRITE, 'u', UBLK_CMD_ADD_DEV, 48)
	if cmd == 0 {
		t.Error("IoctlEncode returned 0")
	}
	
	// Test helper functions
	ctrlCmd := UblkCtrlCmd(UBLK_CMD_ADD_DEV)
	if ctrlCmd == UBLK_CMD_ADD_DEV {
		t.Error("UblkCtrlCmd should encode the command")
	}
	
	ioCmd := UblkIOCmd(UBLK_IO_FETCH_REQ)
	if ioCmd == UBLK_IO_FETCH_REQ {
		t.Error("UblkIOCmd should encode the command")
	}
}

// Test device path helpers
func TestDevicePaths(t *testing.T) {
	if UblkDevicePath(0) != "/dev/ublkc0" {
		t.Errorf("UblkDevicePath(0) = %s, want /dev/ublkc0", UblkDevicePath(0))
	}
	
	if UblkBlockDevicePath(42) != "/dev/ublkb42" {
		t.Errorf("UblkBlockDevicePath(42) = %s, want /dev/ublkb42", UblkBlockDevicePath(42))
	}
}

// Test constants are in valid ranges
func TestConstants(t *testing.T) {
	// Test queue/tag limits are powers of 2 or reasonable
	if UBLK_MAX_QUEUE_DEPTH != 4096 {
		t.Errorf("UBLK_MAX_QUEUE_DEPTH = %d, want 4096", UBLK_MAX_QUEUE_DEPTH)
	}
	
	if UBLK_MAX_NR_QUEUES != 4096 {
		t.Errorf("UBLK_MAX_NR_QUEUES = %d, want 4096", UBLK_MAX_NR_QUEUES)
	}
	
	// Test mmap offset
	if UBLKSRV_IO_BUF_OFFSET != 0x80000000 {
		t.Errorf("UBLKSRV_IO_BUF_OFFSET = %x, want 0x80000000", UBLKSRV_IO_BUF_OFFSET)
	}
}

// Benchmark marshaling performance
func BenchmarkMarshal(b *testing.B) {
	cmd := &UblksrvCtrlCmd{
		DevID:      42,
		QueueID:    0,
		Len:        100,
		Addr:       0x123456789ABCDEF0,
		Data:       [1]uint64{0xDEADBEEF},
	}
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = Marshal(cmd)
	}
}

func BenchmarkUnmarshal(b *testing.B) {
	cmd := &UblksrvCtrlCmd{
		DevID:      42,
		QueueID:    0,
		Len:        100,
		Addr:       0x123456789ABCDEF0,
		Data:       [1]uint64{0xDEADBEEF},
	}
	data := Marshal(cmd)
	
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var unmarshaled UblksrvCtrlCmd
		_ = Unmarshal(data, &unmarshaled)
	}
}