package uapi

import "testing"

// allOpCodes returns every UBLK_IO_OP_* constant the uapi package exposes.
func allOpCodes() []uint32 {
	return []uint32{
		UBLK_IO_OP_READ,
		UBLK_IO_OP_WRITE,
		UBLK_IO_OP_FLUSH,
		UBLK_IO_OP_DISCARD,
		UBLK_IO_OP_WRITE_SAME,
		UBLK_IO_OP_WRITE_ZEROES,
		UBLK_IO_OP_ZONE_OPEN,
		UBLK_IO_OP_ZONE_CLOSE,
		UBLK_IO_OP_ZONE_FINISH,
		UBLK_IO_OP_ZONE_APPEND,
		UBLK_IO_OP_ZONE_RESET_ALL,
		UBLK_IO_OP_ZONE_RESET,
		UBLK_IO_OP_REPORT_ZONES,
	}
}

// TestUnitIODescGetOpGetFlagsAllOps round-trips the op code through
// UblksrvIODesc.OpFlags for every UBLK_IO_OP_* constant, with flags all zero:
// GetOp must hand back exactly the op code and GetFlags must hand back 0.
func TestUnitIODescGetOpGetFlagsAllOps(t *testing.T) {
	for _, op := range allOpCodes() {
		desc := UblksrvIODesc{OpFlags: op}
		if got := uint32(desc.GetOp()); got != op {
			t.Errorf("GetOp() = %d, want %d (op code round-trip through OpFlags low byte)", got, op)
		}
		if got := desc.GetFlags(); got != 0 {
			t.Errorf("GetFlags() = 0x%x, want 0 for op %d with no flags", got, op)
		}
	}
}

// TestUnitIODescGetOpGetFlagsWithFlags places flags alongside an op code and
// confirms GetOp and GetFlags recover their own half without leaking into the
// other.
//
// OpFlags layout: op in bits 0-7, flags in bits 8-31. GetOp() masks the low
// byte, GetFlags() shifts right by 8. The UBLK_IO_F_* constants are already raw
// bit values within the flags half (e.g. UBLK_IO_F_FUA = 1<<13 = 0x2000), so to
// land one in OpFlags it must be pre-shifted left by 8:
//
//	UBLK_IO_F_FUA << 8 = 0x2000 << 8 = 0x200000
//
// which GetFlags() (» 8) turns back into exactly 0x2000.
func TestUnitIODescGetOpGetFlagsWithFlags(t *testing.T) {
	op := uint32(UBLK_IO_OP_WRITE)

	singleFlags := []uint32{
		UBLK_IO_F_FAILFAST_DEV,
		UBLK_IO_F_FAILFAST_TRANSPORT,
		UBLK_IO_F_FAILFAST_DRIVER,
		UBLK_IO_F_META,
		UBLK_IO_F_FUA,
		UBLK_IO_F_NOUNMAP,
		UBLK_IO_F_SWAP,
	}
	for _, flag := range singleFlags {
		desc := UblksrvIODesc{OpFlags: op | (flag << 8)}
		if got := uint32(desc.GetOp()); got != op {
			t.Errorf("GetOp() = %d, want %d with flag 0x%x also set (flag must not leak into op byte)", got, op, flag)
		}
		if got := desc.GetFlags(); got != flag {
			t.Errorf("GetFlags() = 0x%x, want 0x%x (op must not leak into flags half, and the >>8 must be exact)", got, flag)
		}
	}

	// Two flags at once: FUA and META must coexist without colliding with each
	// other or with the op code in the low byte.
	mixed := uint32(UBLK_IO_F_FUA) | uint32(UBLK_IO_F_META)
	if pre := mixed << 8; pre&0xff != 0 {
		t.Fatalf("pre-shifted flag bits leak into the op byte: 0x%x & 0xff = 0x%x", pre, pre&0xff)
	}
	desc := UblksrvIODesc{OpFlags: op | (mixed << 8)}
	if got := uint32(desc.GetOp()); got != op {
		t.Errorf("GetOp() = %d, want %d with FUA|META set", got, op)
	}
	if got := desc.GetFlags(); got != mixed {
		t.Errorf("GetFlags() = 0x%x, want 0x%x (FUA|META)", got, mixed)
	}

	// Three flags at once: add SWAP on top of FUA|META.
	three := uint32(UBLK_IO_F_FUA) | uint32(UBLK_IO_F_META) | uint32(UBLK_IO_F_SWAP)
	desc3 := UblksrvIODesc{OpFlags: op | (three << 8)}
	if got := uint32(desc3.GetOp()); got != op {
		t.Errorf("GetOp() = %d, want %d with FUA|META|SWAP set", got, op)
	}
	if got := desc3.GetFlags(); got != three {
		t.Errorf("GetFlags() = 0x%x, want 0x%x (FUA|META|SWAP)", got, three)
	}
}

// TestUnitIODescGetOpIgnoresGarbage proves the & 0xff mask in GetOp is doing
// real work: every bit 8-31 is set (bits that belong to no known flag) and the
// op code still comes back intact.
func TestUnitIODescGetOpIgnoresGarbage(t *testing.T) {
	for _, op := range allOpCodes() {
		desc := UblksrvIODesc{OpFlags: 0xFFFFFF00 | op}
		if got := uint32(desc.GetOp()); got != op {
			t.Errorf("GetOp() = %d, want %d despite every bit 8-31 set (0xFFFFFF00|op)", got, op)
		}
	}
}

// TestUnitUblkParamsHasAllFalseWhenZero pins the all-false baseline: a zero
// UblkParams has every parameter-type bit clear.
func TestUnitUblkParamsHasAllFalseWhenZero(t *testing.T) {
	p := UblkParams{}
	if p.HasBasic() {
		t.Error("HasBasic() = true for zero UblkParams")
	}
	if p.HasDiscard() {
		t.Error("HasDiscard() = true for zero UblkParams")
	}
	if p.HasDevt() {
		t.Error("HasDevt() = true for zero UblkParams")
	}
	if p.HasZoned() {
		t.Error("HasZoned() = true for zero UblkParams")
	}
}

// TestUnitUblkParamsHasSetIsolation exercises each Has*/Set* pair in isolation.
// After SetX on a fresh zero params, HasX must be true, the OTHER three Has*
// methods must still be false, and p.Types must contain exactly one bit. This
// catches a Set* that ORs the wrong bit (Types would gain a second bit) or a
// Has* that tests the wrong bit (HasX would stay false while another Has*
// flickers true).
func TestUnitUblkParamsHasSetIsolation(t *testing.T) {
	pairs := []struct {
		name string
		bit  uint32
		set  func(*UblkParams)
		has  func(*UblkParams) bool
	}{
		{"Basic", UBLK_PARAM_TYPE_BASIC, (*UblkParams).SetBasic, (*UblkParams).HasBasic},
		{"Discard", UBLK_PARAM_TYPE_DISCARD, (*UblkParams).SetDiscard, (*UblkParams).HasDiscard},
		{"Devt", UBLK_PARAM_TYPE_DEVT, (*UblkParams).SetDevt, (*UblkParams).HasDevt},
		{"Zoned", UBLK_PARAM_TYPE_ZONED, (*UblkParams).SetZoned, (*UblkParams).HasZoned},
	}
	hasMethods := []struct {
		name string
		bit  uint32
		has  func(*UblkParams) bool
	}{
		{"HasBasic", UBLK_PARAM_TYPE_BASIC, (*UblkParams).HasBasic},
		{"HasDiscard", UBLK_PARAM_TYPE_DISCARD, (*UblkParams).HasDiscard},
		{"HasDevt", UBLK_PARAM_TYPE_DEVT, (*UblkParams).HasDevt},
		{"HasZoned", UBLK_PARAM_TYPE_ZONED, (*UblkParams).HasZoned},
	}

	for _, pair := range pairs {
		p := UblkParams{}
		pair.set(&p)
		if !pair.has(&p) {
			t.Errorf("%s: Has%s() = false after Set%s()", pair.name, pair.name, pair.name)
		}
		if p.Types != pair.bit {
			t.Errorf("%s: Types = 0x%x, want 0x%x (Set%s must OR exactly one bit)", pair.name, p.Types, pair.bit, pair.name)
		}
		for _, other := range hasMethods {
			if other.bit == pair.bit {
				continue
			}
			if other.has(&p) {
				t.Errorf("%s: %s() = true after Set%s() (a Set or Has pins the wrong bit)", pair.name, other.name, pair.name)
			}
		}
	}
}

// TestUnitUblkParamsSetAllTypes sets all four parameter types on one params and
// asserts every Has* reports true and p.Types is exactly
// 1<<0|1<<1|1<<2|1<<3 = 0x1|0x2|0x4|0x8 = 0xF.
func TestUnitUblkParamsSetAllTypes(t *testing.T) {
	p := UblkParams{}
	p.SetBasic()
	p.SetDiscard()
	p.SetDevt()
	p.SetZoned()
	if !p.HasBasic() || !p.HasDiscard() || !p.HasDevt() || !p.HasZoned() {
		t.Fatal("all four Has* must be true after all four Set*")
	}
	// UBLK_PARAM_TYPE_BASIC(0x1) | DISCARD(0x2) | DEVT(0x4) | ZONED(0x8) = 0xF
	if p.Types != 0xF {
		t.Errorf("Types = 0x%x, want 0xf (all four UBLK_PARAM_TYPE_* bits)", p.Types)
	}
}

// TestUnitUblkParamsSetIdempotent asserts the Set* accessors are idempotent:
// OR-ing the same bit twice leaves Types unchanged.
func TestUnitUblkParamsSetIdempotent(t *testing.T) {
	sets := []struct {
		name string
		bit  uint32
		set  func(*UblkParams)
	}{
		{"Basic", UBLK_PARAM_TYPE_BASIC, (*UblkParams).SetBasic},
		{"Discard", UBLK_PARAM_TYPE_DISCARD, (*UblkParams).SetDiscard},
		{"Devt", UBLK_PARAM_TYPE_DEVT, (*UblkParams).SetDevt},
		{"Zoned", UBLK_PARAM_TYPE_ZONED, (*UblkParams).SetZoned},
	}
	for _, s := range sets {
		once := UblkParams{}
		twice := UblkParams{}
		s.set(&once)
		s.set(&twice)
		s.set(&twice)
		if once.Types != twice.Types {
			t.Errorf("%s: Types after one Set = 0x%x, after two Sets = 0x%x (Set%s must be idempotent)", s.name, once.Types, twice.Types, s.name)
		}
		if twice.Types != s.bit {
			t.Errorf("%s: Types after two Sets = 0x%x, want 0x%x", s.name, twice.Types, s.bit)
		}
	}
}

// TestUnitIOCmdZoneAppendLBARoundTrip covers SetZoneAppendLBA/GetZoneAppendLBA.
//
// UblksrvIOCmd.Addr is a union in the kernel's C struct: the same 8 bytes serve
// as a userspace buffer address for the FETCH* commands or as the zone-append
// LBA for UBLK_IO_OP_ZONE_APPEND. This Go struct models that single storage
// location with two named accessors, so a value passed through
// SetZoneAppendLBA must be readable back from both GetZoneAppendLBA and a
// direct read of .Addr, byte for byte.
func TestUnitIOCmdZoneAppendLBARoundTrip(t *testing.T) {
	values := []uint64{
		0,                  // zero value
		12345,              // ordinary LBA
		0x8000000000000000, // high bit of a uint64 set
		^uint64(0),         // all bits set
	}
	for _, lba := range values {
		c := UblksrvIOCmd{}
		c.SetZoneAppendLBA(lba)
		if got := c.GetZoneAppendLBA(); got != lba {
			t.Errorf("GetZoneAppendLBA() = 0x%x, want 0x%x (round trip through SetZoneAppendLBA)", got, lba)
		}
		// Union intent: SetZoneAppendLBA writes the very same 8 bytes that .Addr
		// exposes, so the two must agree exactly (no narrowing en route).
		if c.Addr != lba {
			t.Errorf("Addr = 0x%x, want 0x%x (SetZoneAppendLBA must write .Addr byte-for-byte)", c.Addr, lba)
		}
	}
}
