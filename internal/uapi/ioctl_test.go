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

// TestUnitUblkCtrlCmd checks every control command number against the
// header's encoding. typ=117 ('u'=0x75), size=32, nr = the command literal,
// and dir = 2 (_IOR) for the read-only commands, 3 (_IOWR) for the rest:
//
//	_IOR:  (2<<30) | (32<<16) | (0x75<<8) | nr = 0x80207500 | nr
//	_IOWR: (3<<30) | (32<<16) | (0x75<<8) | nr = 0xC0207500 | nr
//
// The direction matters for GET_FEATURES, which the driver compares in full.
func TestUnitUblkCtrlCmd(t *testing.T) {
	cases := []struct {
		name    string
		cmd     uint32
		encoded uint32 // the UBLK_U_CMD_* constant
		want    uint32
	}{
		{"GET_QUEUE_AFFINITY", UBLK_CMD_GET_QUEUE_AFFINITY, UBLK_U_CMD_GET_QUEUE_AFFINITY, 0x80207501},
		{"GET_DEV_INFO", UBLK_CMD_GET_DEV_INFO, UBLK_U_CMD_GET_DEV_INFO, 0x80207502},
		{"ADD_DEV", UBLK_CMD_ADD_DEV, UBLK_U_CMD_ADD_DEV, 0xC0207504},
		{"DEL_DEV", UBLK_CMD_DEL_DEV, UBLK_U_CMD_DEL_DEV, 0xC0207505},
		{"START_DEV", UBLK_CMD_START_DEV, UBLK_U_CMD_START_DEV, 0xC0207506},
		{"STOP_DEV", UBLK_CMD_STOP_DEV, UBLK_U_CMD_STOP_DEV, 0xC0207507},
		{"SET_PARAMS", UBLK_CMD_SET_PARAMS, UBLK_U_CMD_SET_PARAMS, 0xC0207508},
		{"GET_PARAMS", UBLK_CMD_GET_PARAMS, UBLK_U_CMD_GET_PARAMS, 0x80207509},
		{"START_USER_RECOVERY", UBLK_CMD_START_USER_RECOVERY, UBLK_U_CMD_START_USER_RECOVERY, 0xC0207510},
		{"END_USER_RECOVERY", UBLK_CMD_END_USER_RECOVERY, UBLK_U_CMD_END_USER_RECOVERY, 0xC0207511},
		{"GET_DEV_INFO2", UBLK_CMD_GET_DEV_INFO2, UBLK_U_CMD_GET_DEV_INFO2, 0x80207512},
		{"GET_FEATURES", UBLK_CMD_GET_FEATURES, UBLK_U_CMD_GET_FEATURES, 0x80207513},
		{"DEL_DEV_ASYNC", UBLK_CMD_DEL_DEV_ASYNC, UBLK_U_CMD_DEL_DEV_ASYNC, 0x80207514},
		{"UPDATE_SIZE", UBLK_CMD_UPDATE_SIZE, UBLK_U_CMD_UPDATE_SIZE, 0xC0207515},
		{"QUIESCE_DEV", UBLK_CMD_QUIESCE_DEV, UBLK_U_CMD_QUIESCE_DEV, 0xC0207516},
		{"TRY_STOP_DEV", UBLK_CMD_TRY_STOP_DEV, UBLK_U_CMD_TRY_STOP_DEV, 0xC0207517},
		{"REG_BUF", UBLK_CMD_REG_BUF, UBLK_U_CMD_REG_BUF, 0xC0207518},
		{"UNREG_BUF", UBLK_CMD_UNREG_BUF, UBLK_U_CMD_UNREG_BUF, 0xC0207519},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := UblkCtrlCmd(c.cmd); got != c.want {
				t.Errorf("UblkCtrlCmd(%#x) = %#x, want %#x", c.cmd, got, c.want)
			}
			if c.encoded != c.want {
				t.Errorf("UBLK_U_CMD_%s = %#x, want %#x", c.name, c.encoded, c.want)
			}
		})
	}
}

// TestUnitUblkIOCmd checks every I/O command number. All are _IOWR with
// size 16 (sizeof ublksrv_io_cmd == sizeof ublk_batch_io):
//
//	(3<<30) | (16<<16) | (0x75<<8) | nr = 0xC0107500 | nr
func TestUnitUblkIOCmd(t *testing.T) {
	cases := []struct {
		name    string
		cmd     uint32
		encoded uint32 // the UBLK_U_IO_* constant
		want    uint32
	}{
		{"FETCH_REQ", UBLK_IO_FETCH_REQ, UBLK_U_IO_FETCH_REQ, 0xC0107520},
		{"COMMIT_AND_FETCH_REQ", UBLK_IO_COMMIT_AND_FETCH_REQ, UBLK_U_IO_COMMIT_AND_FETCH_REQ, 0xC0107521},
		{"NEED_GET_DATA", UBLK_IO_NEED_GET_DATA, UBLK_U_IO_NEED_GET_DATA, 0xC0107522},
		{"REGISTER_IO_BUF", UBLK_IO_REGISTER_IO_BUF, UBLK_U_IO_REGISTER_IO_BUF, 0xC0107523},
		{"UNREGISTER_IO_BUF", UBLK_IO_UNREGISTER_IO_BUF, UBLK_U_IO_UNREGISTER_IO_BUF, 0xC0107524},
		{"PREP_IO_CMDS", UBLK_IO_PREP_IO_CMDS, UBLK_U_IO_PREP_IO_CMDS, 0xC0107525},
		{"COMMIT_IO_CMDS", UBLK_IO_COMMIT_IO_CMDS, UBLK_U_IO_COMMIT_IO_CMDS, 0xC0107526},
		{"FETCH_IO_CMDS", UBLK_IO_FETCH_IO_CMDS, UBLK_U_IO_FETCH_IO_CMDS, 0xC0107527},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := UblkIOCmd(c.cmd); got != c.want {
				t.Errorf("UblkIOCmd(%#x) = %#x, want %#x", c.cmd, got, c.want)
			}
			if c.encoded != c.want {
				t.Errorf("UBLK_U_IO_%s = %#x, want %#x", c.name, c.encoded, c.want)
			}
		})
	}
}

// TestUnitUblkIoctlDistinctness asserts that no two encoded values collide.
func TestUnitUblkIoctlDistinctness(t *testing.T) {
	seen := make(map[uint32]string)
	all := []struct {
		name string
		v    uint32
	}{
		{"UBLK_U_CMD_GET_QUEUE_AFFINITY", UBLK_U_CMD_GET_QUEUE_AFFINITY},
		{"UBLK_U_CMD_GET_DEV_INFO", UBLK_U_CMD_GET_DEV_INFO},
		{"UBLK_U_CMD_ADD_DEV", UBLK_U_CMD_ADD_DEV},
		{"UBLK_U_CMD_DEL_DEV", UBLK_U_CMD_DEL_DEV},
		{"UBLK_U_CMD_START_DEV", UBLK_U_CMD_START_DEV},
		{"UBLK_U_CMD_STOP_DEV", UBLK_U_CMD_STOP_DEV},
		{"UBLK_U_CMD_SET_PARAMS", UBLK_U_CMD_SET_PARAMS},
		{"UBLK_U_CMD_GET_PARAMS", UBLK_U_CMD_GET_PARAMS},
		{"UBLK_U_CMD_START_USER_RECOVERY", UBLK_U_CMD_START_USER_RECOVERY},
		{"UBLK_U_CMD_END_USER_RECOVERY", UBLK_U_CMD_END_USER_RECOVERY},
		{"UBLK_U_CMD_GET_DEV_INFO2", UBLK_U_CMD_GET_DEV_INFO2},
		{"UBLK_U_CMD_GET_FEATURES", UBLK_U_CMD_GET_FEATURES},
		{"UBLK_U_CMD_DEL_DEV_ASYNC", UBLK_U_CMD_DEL_DEV_ASYNC},
		{"UBLK_U_CMD_UPDATE_SIZE", UBLK_U_CMD_UPDATE_SIZE},
		{"UBLK_U_CMD_QUIESCE_DEV", UBLK_U_CMD_QUIESCE_DEV},
		{"UBLK_U_CMD_TRY_STOP_DEV", UBLK_U_CMD_TRY_STOP_DEV},
		{"UBLK_U_CMD_REG_BUF", UBLK_U_CMD_REG_BUF},
		{"UBLK_U_CMD_UNREG_BUF", UBLK_U_CMD_UNREG_BUF},
		{"UBLK_U_IO_FETCH_REQ", UBLK_U_IO_FETCH_REQ},
		{"UBLK_U_IO_COMMIT_AND_FETCH_REQ", UBLK_U_IO_COMMIT_AND_FETCH_REQ},
		{"UBLK_U_IO_NEED_GET_DATA", UBLK_U_IO_NEED_GET_DATA},
		{"UBLK_U_IO_REGISTER_IO_BUF", UBLK_U_IO_REGISTER_IO_BUF},
		{"UBLK_U_IO_UNREGISTER_IO_BUF", UBLK_U_IO_UNREGISTER_IO_BUF},
		{"UBLK_U_IO_PREP_IO_CMDS", UBLK_U_IO_PREP_IO_CMDS},
		{"UBLK_U_IO_COMMIT_IO_CMDS", UBLK_U_IO_COMMIT_IO_CMDS},
		{"UBLK_U_IO_FETCH_IO_CMDS", UBLK_U_IO_FETCH_IO_CMDS},
	}
	for _, c := range all {
		if prev, ok := seen[c.v]; ok {
			t.Errorf("%s = %#x collides with %s", c.name, c.v, prev)
		}
		seen[c.v] = c.name
	}
}
