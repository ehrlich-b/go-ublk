package ctrl

import (
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/constants"
	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// sizeToShift computes the log2 shift SetParams applies to LogicalBlockSize
// when filling LogicalBSShift / PhysicalBSShift / IOMinShift. These are the
// block sizes the kernel accepts (one sector up to a page), each shift
// hand-derived from log2 and asserted as an integer literal.
func TestSizeToShiftPowerOfTwo(t *testing.T) {
	tests := []struct {
		name string
		size int
		want int
	}{
		{"512 bytes (2^9)", 512, 9},
		{"1024 bytes (2^10)", 1024, 10},
		{"2048 bytes (2^11)", 2048, 11},
		{"4096 bytes (2^12)", 4096, 12},
		{"65536 bytes (2^16)", 65536, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sizeToShift(tt.size); got != tt.want {
				t.Errorf("sizeToShift(%d) = %d, want %d", tt.size, got, tt.want)
			}
		})
	}
}

// The public API's validateParams (backend.go) rejects non-powers-of-two
// logical block sizes before a Controller is ever built, but sizeToShift
// itself has no such guard: a caller driving the ctrl package directly could
// feed it anything. These expectations are hand-traced from the loop body
// below, not copied from test output.
//
//	for s := size; s > 1; s >>= 1 { shift++ }
func TestSizeToShiftDegenerate(t *testing.T) {
	tests := []struct {
		name string
		size int
		want int
	}{
		// 0: s := 0; the loop condition 0 > 1 is false immediately, so no
		// iterations run and shift stays at its zero value.
		{"zero", 0, 0},
		// 1: s := 1; 1 > 1 is false, so again no iterations -> 0.
		{"one", 1, 0},
		// 500 (not a power of two): the loop runs while s > 1, halving each
		// pass: 500 -> 250 -> 125 -> 62 -> 31 -> 15 -> 7 -> 3 -> 1. That is 8
		// passes, so 8. Note this is floor(log2(500)) = 8 — the shift a 256-byte
		// block would have — for a size that is not even a valid logical block
		// size, i.e. a silent misreport the ctrl package does not defend
		// against on its own.
		{"non-power-of-two 500", 500, 8},
		// 100000: 100000 -> 50000 -> 25000 -> 12500 -> 6250 -> 3125 -> 1562 ->
		// 781 -> 390 -> 195 -> 97 -> 48 -> 24 -> 12 -> 6 -> 3 -> 1. 16 passes,
		// so 16. 2^16 = 65536 <= 100000 < 2^17 = 131072, consistent with
		// floor(log2(100000)) = 16.
		{"large non-power-of-two 100000", 100000, 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sizeToShift(tt.size); got != tt.want {
				t.Errorf("sizeToShift(%d) = %d, want %d", tt.size, got, tt.want)
			}
		})
	}
}

// buildFeatureFlags only reads its params argument, so a zero-value
// Controller is enough to exercise it — no /dev/ublk-control needed.

// Every boolean feature flag off: the result must be exactly the unconditional
// base flag and nothing else.
func TestBuildFeatureFlagsAllFalse(t *testing.T) {
	got := (&Controller{}).buildFeatureFlags(&DeviceParams{})
	want := uint64(uapi.UBLK_F_URING_CMD_COMP_IN_TASK)
	if got != want {
		t.Errorf("buildFeatureFlags() = %#x, want %#x", got, want)
	}
}

func TestBuildFeatureFlagsEachFlagAlone(t *testing.T) {
	base := uint64(uapi.UBLK_F_URING_CMD_COMP_IN_TASK)

	tests := []struct {
		name   string
		params DeviceParams
		want   uint64
	}{
		{
			"zero copy",
			DeviceParams{EnableZeroCopy: true},
			base | uint64(uapi.UBLK_F_SUPPORT_ZERO_COPY),
		},
		{
			"unprivileged",
			DeviceParams{EnableUnprivileged: true},
			base | uint64(uapi.UBLK_F_UNPRIVILEGED_DEV),
		},
		{
			"user copy",
			DeviceParams{EnableUserCopy: true},
			base | uint64(uapi.UBLK_F_USER_COPY),
		},
		{
			"ioctl encode",
			DeviceParams{EnableIoctlEncode: true},
			base | uint64(uapi.UBLK_F_CMD_IOCTL_ENCODE),
		},
		{
			// SUSPECTED DEFECT — PINNED, NOT FIXED. DeviceParams carries an
			// EnableZoned bool (types.go) and uapi declares UBLK_F_ZONED = 1<<8
			// (uapi/constants.go), but buildFeatureFlags has no branch for it,
			// so a caller asking for zoned-storage support at ADD_DEV gets
			// silently ignored: no feature bit is ORed in and no error is
			// raised anywhere. The expected value here is just the base flag.
			// Do not "fix" this by adding the missing if params.EnableZoned
			// branch; report it.
			"zoned (PINNED: unwired)",
			DeviceParams{EnableZoned: true},
			base,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := (&Controller{}).buildFeatureFlags(&tt.params); got != tt.want {
				t.Errorf("buildFeatureFlags() = %#x, want %#x", got, tt.want)
			}
		})
	}
}

func TestBuildFeatureFlagsAllTrue(t *testing.T) {
	got := (&Controller{}).buildFeatureFlags(&DeviceParams{
		EnableZeroCopy:     true,
		EnableUnprivileged: true,
		EnableUserCopy:     true,
		EnableZoned:        true,
		EnableIoctlEncode:  true,
	})
	// The full OR of every bit buildFeatureFlags actually sets. EnableZoned is
	// included in the input but contributes nothing — see the pinned finding in
	// TestBuildFeatureFlagsEachFlagAlone.
	want := uint64(uapi.UBLK_F_URING_CMD_COMP_IN_TASK |
		uapi.UBLK_F_SUPPORT_ZERO_COPY |
		uapi.UBLK_F_UNPRIVILEGED_DEV |
		uapi.UBLK_F_USER_COPY |
		uapi.UBLK_F_CMD_IOCTL_ENCODE)
	if got != want {
		t.Errorf("buildFeatureFlags() = %#x, want %#x", got, want)
	}
}

// ctrl.DefaultDeviceParams hard-codes its defaults as literals instead of
// referencing internal/constants the way the public API's DefaultParams does
// (backend.go). These two independently-maintained copies must not drift; each
// row pins one literal against its canonical constant.
func TestDefaultDeviceParamsMatchConstants(t *testing.T) {
	p := DefaultDeviceParams(nil)

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"QueueDepth", p.QueueDepth, constants.DefaultQueueDepth},
		{"LogicalBlockSize", p.LogicalBlockSize, constants.DefaultLogicalBlockSize},
		{"MaxIOSize", p.MaxIOSize, constants.DefaultMaxIOSize},
		{"DiscardAlignment", p.DiscardAlignment, uint32(constants.DefaultDiscardAlignment)},
		{"DiscardGranularity", p.DiscardGranularity, uint32(constants.DefaultDiscardGranularity)},
		{"MaxDiscardSectors", p.MaxDiscardSectors, uint32(constants.DefaultMaxDiscardSectors)},
		{"MaxDiscardSegments", p.MaxDiscardSegments, uint16(constants.DefaultMaxDiscardSegments)},
		{"DeviceID", p.DeviceID, int32(constants.AutoAssignDeviceID)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s = %v (%T), want %v (%T)", tt.name, tt.got, tt.got, tt.want, tt.want)
			}
		})
	}
}

// DefaultDeviceParams must tolerate a nil backend (the value a caller uses
// when only reading defaults) and must not stash any backend on the result.
func TestDefaultDeviceParamsNilBackend(t *testing.T) {
	p := DefaultDeviceParams(nil)
	if p.Backend != nil {
		t.Errorf("Backend = %v, want nil", p.Backend)
	}
}

// DeviceInfo.Size() converts dev_sectors to bytes: int64(d.DevSectors) *
// uapi.SectorSize, where a sector is always 512 bytes regardless of logical
// block size. The field is uint64 and the multiplication happens on int64, so
// large counts must not wrap (a 4e9-sector value would overflow int32 if the
// arithmetic were done in 32 bits).
func TestDeviceInfoSize(t *testing.T) {
	tests := []struct {
		name       string
		devSectors uint64
		want       int64
	}{
		{"zero sectors", 0, 0},
		{"one sector", 1, 512},
		{"ten sectors", 10, 5 * 1024},
		{"one billion sectors", 1_000_000_000, 512_000_000_000},
		// 4e9 sectors * 512 = 2,048,000,000,000 bytes. 4e9 > int32 max, so this
		// exercises the 64-bit arithmetic honestly.
		{"4e9 sectors (would overflow int32)", 4_000_000_000, 2_048_000_000_000},
		{"2^32 sectors", 1 << 32, (1 << 32) * 512},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := &DeviceInfo{DevSectors: tt.devSectors}
			if got := d.Size(); got != tt.want {
				t.Errorf("Size() = %d, want %d", got, tt.want)
			}
		})
	}
}
