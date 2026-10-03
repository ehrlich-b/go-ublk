package uapi

import "testing"

// ---------------------------------------------------------------------------
// ioctl encoding tests.
//
// These verify IoctlEncode / UblkCtrlCmd / UblkIOCmd against the Linux
// <asm-generic/ioctl.h> _IOC macro, which these functions port directly:
//
//	#define _IOC(dir,type,nr,size) \
//		(((dir)  << _IOC_DIRSHIFT) | ((type) << _IOC_TYPESHIFT) | \
//		 ((nr)   << _IOC_NRSHIFT)  | ((size) << _IOC_SIZESHIFT))
//
// with _IOC_NRBITS=8, _IOC_TYPEBITS=8, _IOC_SIZEBITS=14, _IOC_DIRBITS=2.
// Hand-summimg the derived shifts from the bit widths:
//
//	_IOC_NRSHIFT   = 0
//	_IOC_TYPESHIFT = _IOC_NRSHIFT   + _IOC_NRBITS   = 0  + 8  = 8
//	_IOC_SIZESHIFT = _IOC_TYPESHIFT + _IOC_TYPEBITS = 8  + 8  = 16
//	_IOC_DIRSHIFT  = _IOC_SIZESHIFT + _IOC_SIZEBITS = 16 + 14 = 30
//
// Every expected value below is an explicit integer literal with the
// arithmetic shown; none are copied from the functions' output.
// ---------------------------------------------------------------------------

// TestUnitIoctlShiftConstants pins the four shift constants individually.
// This is the highest-value assertion: if the kernel's ioctl bit-width
// allocation ever changes without a matching change here, the EINVAL would
// only surface at the syscall boundary three frames away.
func TestUnitIoctlShiftConstants(t *testing.T) {
	if _IOC_NRSHIFT != 0 {
		t.Errorf("_IOC_NRSHIFT = %d, want 0", _IOC_NRSHIFT)
	}
	if _IOC_TYPESHIFT != 8 { // _IOC_NRSHIFT + _IOC_NRBITS = 0 + 8
		t.Errorf("_IOC_TYPESHIFT = %d, want 8", _IOC_TYPESHIFT)
	}
	if _IOC_SIZESHIFT != 16 { // _IOC_TYPESHIFT + _IOC_TYPEBITS = 8 + 8
		t.Errorf("_IOC_SIZESHIFT = %d, want 16", _IOC_SIZESHIFT)
	}
	if _IOC_DIRSHIFT != 30 { // _IOC_SIZESHIFT + _IOC_SIZEBITS = 16 + 14
		t.Errorf("_IOC_DIRSHIFT = %d, want 30", _IOC_DIRSHIFT)
	}
}

// TestUnitIoctlEncode exercises IoctlEncode's four fields independently,
// including each field's maximum legal value for its bit width, plus tuples
// with every field non-zero simultaneously (to catch a shift landing in the
// wrong position, which only fails when fields corrupt each other).
func TestUnitIoctlEncode(t *testing.T) {
	cases := []struct {
		name string
		dir  uint32
		typ  uint32
		nr   uint32
		size uint32
		want uint32
	}{
		// All zero: every field empty.
		{"all-zero", 0, 0, 0, 0, 0x00000000},

		// dir alone: dir << 30.
		// dir = 1 -> 1<<30 = 0x40000000
		{"dir=1", 1, 0, 0, 0, 0x40000000},
		// dir = 2 -> 2<<30 = 0x80000000
		{"dir=2", 2, 0, 0, 0, 0x80000000},
		// dir = 3 (max, both READ|WRITE) -> 3<<30 = 0xC0000000
		{"dir=3-max", 3, 0, 0, 0, 0xC0000000},

		// typ alone: typ << 8.
		// typ = 1 -> 1<<8 = 0x00000100
		{"typ=1", 0, 1, 0, 0, 0x00000100},
		// typ = 0x75 ('u' = 117 decimal) -> 0x75<<8 = 0x00007500
		{"typ=u", 0, 0x75, 0, 0, 0x00007500},
		// typ = 0x80 (high edge of the byte) -> 0x80<<8 = 0x00008000
		{"typ=0x80", 0, 0x80, 0, 0, 0x00008000},
		// typ = 0xFF (max) -> 0xFF<<8 = 0x0000FF00
		{"typ=0xff-max", 0, 0xFF, 0, 0, 0x0000FF00},

		// nr alone: nr << 0 (it's the low byte).
		// nr = 1 -> 0x00000001
		{"nr=1", 0, 0, 1, 0, 0x00000001},
		// nr = 0x80 -> 0x00000080
		{"nr=0x80", 0, 0, 0x80, 0, 0x00000080},
		// nr = 0xFF (max) -> 0x000000FF
		{"nr=0xff-max", 0, 0, 0xFF, 0, 0x000000FF},

		// size alone: size << 16.
		// size = 1 -> 1<<16 = 0x00010000
		{"size=1", 0, 0, 0, 1, 0x00010000},
		// size = 16 -> 16<<16 = 0x00100000
		{"size=16", 0, 0, 0, 16, 0x00100000},
		// size = 32 -> 32<<16 = 0x00200000
		{"size=32", 0, 0, 0, 32, 0x00200000},
		// size = 0x3FFF (max 14 bits) -> 0x3FFF<<16 = 0x3FFF0000
		{"size=0x3fff-max", 0, 0, 0, 0x3FFF, 0x3FFF0000},

		// Every field non-zero simultaneously: must not corrupt each other.
		// dir=3<<30 | size=32<<16 | typ=0x75('u')<<8 | nr=0x04
		//   = 0xC0000000 | 0x00200000 | 0x00007500 | 0x00000004
		//   = 0xC0207504
		{"all-fields-ctrl-like", 3, 0x75, 0x04, 32, 0xC0207504},

		// All fields at their maximum simultaneously.
		// dir=3<<30 | size=0x3FFF<<16 | typ=0xFF<<8 | nr=0xFF
		//   = 0xC0000000 | 0x3FFF0000 | 0x0000FF00 | 0x000000FF
		//   = 0xFFFFFFFF
		{"all-fields-max", 3, 0xFF, 0xFF, 0x3FFF, 0xFFFFFFFF},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := IoctlEncode(c.dir, c.typ, c.nr, c.size); got != c.want {
				t.Errorf("IoctlEncode(dir=%x, typ=%x, nr=%x, size=%x) = %#x, want %#x",
					c.dir, c.typ, c.nr, c.size, got, c.want)
			}
		})
	}
}

// TestUnitUblkCtrlCmd checks every UBLK_CMD_* constant.
// dir=3, typ=117 ('u'=0x75), size=32, nr = the command literal.
// Encoding: (3<<30) | (32<<16) | (0x75<<8) | nr
//
//	= 0xC0000000 | 0x00200000 | 0x00007500 | nr = 0xC0207500 | nr
func TestUnitUblkCtrlCmd(t *testing.T) {
	cases := []struct {
		name string
		cmd  uint32
		nr   uint32
		want uint32
	}{
		// (3<<30)|(32<<16)|(117<<8)|0x01 = 0xC0207500 | 0x01 = 0xC0207501
		{"GET_QUEUE_AFFINITY", UBLK_CMD_GET_QUEUE_AFFINITY, 0x01, 0xC0207501},
		// ... | 0x02 = 0xC0207502
		{"GET_DEV_INFO", UBLK_CMD_GET_DEV_INFO, 0x02, 0xC0207502},
		// ... | 0x04 = 0xC0207504
		{"ADD_DEV", UBLK_CMD_ADD_DEV, 0x04, 0xC0207504},
		// ... | 0x05 = 0xC0207505
		{"DEL_DEV", UBLK_CMD_DEL_DEV, 0x05, 0xC0207505},
		// ... | 0x06 = 0xC0207506
		{"START_DEV", UBLK_CMD_START_DEV, 0x06, 0xC0207506},
		// ... | 0x07 = 0xC0207507
		{"STOP_DEV", UBLK_CMD_STOP_DEV, 0x07, 0xC0207507},
		// ... | 0x08 = 0xC0207508
		{"SET_PARAMS", UBLK_CMD_SET_PARAMS, 0x08, 0xC0207508},
		// ... | 0x09 = 0xC0207509
		{"GET_PARAMS", UBLK_CMD_GET_PARAMS, 0x09, 0xC0207509},
		// ... | 0x10 = 0xC0207510
		{"START_USER_RECOVERY", UBLK_CMD_START_USER_RECOVERY, 0x10, 0xC0207510},
		// ... | 0x11 = 0xC0207511
		{"END_USER_RECOVERY", UBLK_CMD_END_USER_RECOVERY, 0x11, 0xC0207511},
		// ... | 0x12 = 0xC0207512
		{"GET_DEV_INFO2", UBLK_CMD_GET_DEV_INFO2, 0x12, 0xC0207512},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := UblkCtrlCmd(c.cmd); got != c.want {
				t.Errorf("UblkCtrlCmd(%#x) = %#x, want %#x (nr=%#x)", c.cmd, got, c.want, c.nr)
			}
		})
	}
}

// TestUnitUblkIOCmd checks every UBLK_IO_* constant.
// dir=3, typ=117 ('u'=0x75), size=16, nr = the command literal.
// Encoding: (3<<30) | (16<<16) | (0x75<<8) | nr
//
//	= 0xC0000000 | 0x00100000 | 0x00007500 | nr = 0xC0107500 | nr
func TestUnitUblkIOCmd(t *testing.T) {
	cases := []struct {
		name string
		cmd  uint32
		nr   uint32
		want uint32
	}{
		// (3<<30)|(16<<16)|(117<<8)|0x20 = 0xC0107500 | 0x20 = 0xC0107520
		{"FETCH_REQ", UBLK_IO_FETCH_REQ, 0x20, 0xC0107520},
		// ... | 0x21 = 0xC0107521
		{"COMMIT_AND_FETCH_REQ", UBLK_IO_COMMIT_AND_FETCH_REQ, 0x21, 0xC0107521},
		// ... | 0x22 = 0xC0107522
		{"NEED_GET_DATA", UBLK_IO_NEED_GET_DATA, 0x22, 0xC0107522},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := UblkIOCmd(c.cmd); got != c.want {
				t.Errorf("UblkIOCmd(%#x) = %#x, want %#x (nr=%#x)", c.cmd, got, c.want, c.nr)
			}
		})
	}
}

// TestUnitUblkIoctlDistinctness asserts that no two encoded values collide.
// Ctrl values carry size=32 (bit 0x00200000) in the size field while IO values
// carry size=16 (bit 0x00100000); those bits can never overlap, and within
// each group nr occupies a distinct distinct low byte. Still, the cross-check
// is asserted explicitly rather than assumed.
func TestUnitUblkIoctlDistinctness(t *testing.T) {
	seen := make(map[uint32]string)

	ctrlCmds := []struct {
		name string
		cmd  uint32
	}{
		{"UBLK_CMD_GET_QUEUE_AFFINITY", UBLK_CMD_GET_QUEUE_AFFINITY},
		{"UBLK_CMD_GET_DEV_INFO", UBLK_CMD_GET_DEV_INFO},
		{"UBLK_CMD_ADD_DEV", UBLK_CMD_ADD_DEV},
		{"UBLK_CMD_DEL_DEV", UBLK_CMD_DEL_DEV},
		{"UBLK_CMD_START_DEV", UBLK_CMD_START_DEV},
		{"UBLK_CMD_STOP_DEV", UBLK_CMD_STOP_DEV},
		{"UBLK_CMD_SET_PARAMS", UBLK_CMD_SET_PARAMS},
		{"UBLK_CMD_GET_PARAMS", UBLK_CMD_GET_PARAMS},
		{"UBLK_CMD_START_USER_RECOVERY", UBLK_CMD_START_USER_RECOVERY},
		{"UBLK_CMD_END_USER_RECOVERY", UBLK_CMD_END_USER_RECOVERY},
		{"UBLK_CMD_GET_DEV_INFO2", UBLK_CMD_GET_DEV_INFO2},
	}
	for _, c := range ctrlCmds {
		v := UblkCtrlCmd(c.cmd)
		if prev, ok := seen[v]; ok {
			t.Errorf("UblkCtrlCmd(%s) = %#x collides with %s", c.name, v, prev)
		}
		seen[v] = "UblkCtrlCmd(" + c.name + ")"
	}

	ioCmds := []struct {
		name string
		cmd  uint32
	}{
		{"UBLK_IO_FETCH_REQ", UBLK_IO_FETCH_REQ},
		{"UBLK_IO_COMMIT_AND_FETCH_REQ", UBLK_IO_COMMIT_AND_FETCH_REQ},
		{"UBLK_IO_NEED_GET_DATA", UBLK_IO_NEED_GET_DATA},
	}
	for _, c := range ioCmds {
		v := UblkIOCmd(c.cmd)
		if prev, ok := seen[v]; ok {
			t.Errorf("UblkIOCmd(%s) = %#x collides with %s", c.name, v, prev)
		}
		seen[v] = "UblkIOCmd(" + c.name + ")"
	}
}
