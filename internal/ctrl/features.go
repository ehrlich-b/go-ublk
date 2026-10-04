package ctrl

import (
	"context"
	"errors"
	"fmt"
	"math/bits"
	"strings"
	"syscall"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// featureNames names every UBLK_F_* bit of the v7.3-rc5 header.
var featureNames = [...]string{
	"SUPPORT_ZERO_COPY", "URING_CMD_COMP_IN_TASK", "NEED_GET_DATA", "USER_RECOVERY",
	"USER_RECOVERY_REISSUE", "UNPRIVILEGED_DEV", "CMD_IOCTL_ENCODE", "USER_COPY",
	"ZONED", "USER_RECOVERY_FAIL_IO", "UPDATE_SIZE", "AUTO_BUF_REG", "QUIESCE",
	"PER_IO_DAEMON", "BUF_REG_OFF_DAEMON", "BATCH_IO", "INTEGRITY", "SAFE_STOP_DEV",
	"NO_AUTO_PART_SCAN", "SHMEM_ZC", "IO_DESC_SIZE",
}

// FeatureNames renders a UBLK_F_* set as "USER_COPY|ZONED"; unknown bits
// appear as "bit21".
func FeatureNames(flags uint64) string {
	var parts []string
	for flags != 0 {
		bit := bits.TrailingZeros64(flags)
		flags &^= 1 << bit
		if bit < len(featureNames) {
			parts = append(parts, featureNames[bit])
		} else {
			parts = append(parts, fmt.Sprintf("bit%d", bit))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "|")
}

// BaseFeatures is what Features assumes when GET_FEATURES is missing
// (kernels before v6.5): the v6.0 set, minus SUPPORT_ZERO_COPY, which every
// kernel before v6.15 accepted in UBLK_F_ALL but silently cleared.
const BaseFeatures = uapi.UBLK_F_URING_CMD_COMP_IN_TASK | uapi.UBLK_F_NEED_GET_DATA

// FeatureSet is what the running kernel reports it supports.
type FeatureSet struct {
	Flags uint64 // UBLK_F_* bits (GET_FEATURES result, or BaseFeatures)
	Known bool   // false when GET_FEATURES is missing and Flags is a guess
}

// Has reports whether every bit of flags is known to be supported.
func (fs FeatureSet) Has(flags uint64) bool { return fs.Flags&flags == flags }

// Features returns the kernel's feature set, cached after the first
// successful probe. GET_FEATURES is absent before v6.5; those kernels answer
// ENODEV (v6.3/v6.4 look up dev_id -1 before dispatching; v6.0-v6.2 fall
// through their opcode switch with ret still -ENODEV). That — or
// EOPNOTSUPP/ENOTSUPP/ENOTTY/EINVAL from anything stranger — yields
// FeatureSet{BaseFeatures, Known: false} and no error: the kernel is too old
// to say, so AddDev sends the request and checks what ADD_DEV returns.
// Transport errors are returned and not cached.
func (c *Controller) Features(ctx context.Context) (FeatureSet, error) {
	c.featMu.Lock()
	defer c.featMu.Unlock()
	if c.features != nil {
		return *c.features, nil
	}
	flags, err := c.GetFeatures(ctx)
	var fs FeatureSet
	switch {
	case err == nil:
		fs = FeatureSet{Flags: flags, Known: true}
	case errors.Is(err, syscall.ENODEV), IsUnsupported(err),
		errors.Is(err, syscall.ENOTTY), errors.Is(err, syscall.EINVAL):
		fs = FeatureSet{Flags: BaseFeatures}
	default:
		return FeatureSet{}, err
	}
	c.features = &fs
	return fs, nil
}

// FeatureConflictError is a requested flag combination the kernel rejects
// (EINVAL) or silently rewrites. Rule cites the ublk_ctrl_add_dev check.
type FeatureConflictError struct {
	Flags uint64
	Rule  string
}

func (e *FeatureConflictError) Error() string {
	return fmt.Sprintf("ublk features %s: %s", FeatureNames(e.Flags), e.Rule)
}

func (e *FeatureConflictError) Unwrap() error { return syscall.EINVAL }

// MissingFeaturesError lists requested features the running kernel lacks.
type MissingFeaturesError struct {
	Requested uint64
	Missing   uint64
	Supported uint64
	Known     bool   // false: Supported is BaseFeatures, a guess
	Source    string // "GET_FEATURES" or "ADD_DEV" (flags the kernel cleared)
}

func (e *MissingFeaturesError) Error() string {
	return fmt.Sprintf("kernel lacks ublk features %s (requested %s; %s reports %s)",
		FeatureNames(e.Missing), FeatureNames(e.Requested), e.Source, FeatureNames(e.Supported))
}

func (e *MissingFeaturesError) Unwrap() error { return syscall.EOPNOTSUPP }

var paramTypeNames = [...]string{"BASIC", "DISCARD", "DEVT", "ZONED", "DMA_ALIGN", "SEGMENT", "INTEGRITY"}

// ParamTypeNames renders a UBLK_PARAM_TYPE_* set as "DMA_ALIGN|SEGMENT".
func ParamTypeNames(types uint32) string {
	var parts []string
	for types != 0 {
		bit := bits.TrailingZeros32(types)
		types &^= 1 << bit
		if bit < len(paramTypeNames) {
			parts = append(parts, paramTypeNames[bit])
		} else {
			parts = append(parts, fmt.Sprintf("bit%d", bit))
		}
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, "|")
}

// UnsupportedParamsError reports parameter types SET_PARAMS accepted but the
// kernel silently masked off because it predates them (ZONED before v6.6,
// DMA_ALIGN and SEGMENT before v6.15, INTEGRITY before v7.0). The other types
// were applied.
type UnsupportedParamsError struct {
	DevID     uint32
	Requested uint32
	Dropped   uint32
}

func (e *UnsupportedParamsError) Error() string {
	return fmt.Sprintf("ublk SET_PARAMS dev %d: kernel ignored param types %s (requested %s)",
		e.DevID, ParamTypeNames(e.Dropped), ParamTypeNames(e.Requested))
}

func (e *UnsupportedParamsError) Unwrap() error { return syscall.EOPNOTSUPP }

// featureRule is one ublk_ctrl_add_dev constraint (v7.3-rc5).
type featureRule struct {
	when    uint64 // applies if all of these are requested
	needAny uint64 // ...and none of these is (0: no requirement)
	forbid  uint64 // ...or any of these is
	rule    string
}

var featureRules = []featureRule{
	// switch (info.flags & UBLK_F_ALL_RECOVERY_FLAGS): only 0, RECOVERY,
	// RECOVERY|REISSUE, RECOVERY|FAIL_IO pass; anything else is -EINVAL.
	{when: uapi.UBLK_F_USER_RECOVERY_REISSUE, needAny: uapi.UBLK_F_USER_RECOVERY,
		rule: "USER_RECOVERY_REISSUE requires USER_RECOVERY (EINVAL)"},
	{when: uapi.UBLK_F_USER_RECOVERY_FAIL_IO, needAny: uapi.UBLK_F_USER_RECOVERY,
		rule: "USER_RECOVERY_FAIL_IO requires USER_RECOVERY (EINVAL)"},
	{when: uapi.UBLK_F_USER_RECOVERY_REISSUE, forbid: uapi.UBLK_F_USER_RECOVERY_FAIL_IO,
		rule: "USER_RECOVERY_REISSUE and USER_RECOVERY_FAIL_IO are exclusive (EINVAL)"},
	// "UBLK_F_QUIESCE requires UBLK_F_USER_RECOVERY" -> -EINVAL.
	{when: uapi.UBLK_F_QUIESCE, needAny: uapi.UBLK_F_USER_RECOVERY,
		rule: "QUIESCE requires USER_RECOVERY (EINVAL)"},
	// Unprivileged: USER_COPY, SUPPORT_ZERO_COPY and AUTO_BUF_REG -> -EINVAL
	// (the server could leak uninitialized kernel memory).
	{when: uapi.UBLK_F_UNPRIVILEGED_DEV,
		forbid: uapi.UBLK_F_USER_COPY | uapi.UBLK_F_SUPPORT_ZERO_COPY | uapi.UBLK_F_AUTO_BUF_REG,
		rule:   "UNPRIVILEGED_DEV excludes USER_COPY, SUPPORT_ZERO_COPY and AUTO_BUF_REG (EINVAL)"},
	// Unprivileged: USER_RECOVERY and REISSUE are silently cleared, which
	// strands FAIL_IO/QUIESCE too.
	{when: uapi.UBLK_F_UNPRIVILEGED_DEV,
		forbid: uapi.UBLK_F_USER_RECOVERY | uapi.UBLK_F_USER_RECOVERY_REISSUE |
			uapi.UBLK_F_USER_RECOVERY_FAIL_IO | uapi.UBLK_F_QUIESCE,
		rule: "UNPRIVILEGED_DEV excludes user recovery (the kernel clears it silently)"},
	// "User copy is required to access integrity buffer" -> -EINVAL.
	{when: uapi.UBLK_F_INTEGRITY, needAny: uapi.UBLK_F_USER_COPY,
		rule: "INTEGRITY requires USER_COPY (EINVAL)"},
	// Zoned reuses ublksrv_io_cmd.addr for the append LBA, "only allowed in
	// case of user copy or zero copy" -> -EINVAL.
	{when: uapi.UBLK_F_ZONED, needAny: uapi.UBLK_F_USER_COPY | uapi.UBLK_F_SUPPORT_ZERO_COPY,
		rule: "ZONED requires USER_COPY or SUPPORT_ZERO_COPY (EINVAL)"},
	// "GET_DATA isn't needed any more with USER_COPY or ZERO COPY" and
	// "UBLK_F_BATCH_IO doesn't support GET_DATA": silently cleared.
	{when: uapi.UBLK_F_NEED_GET_DATA,
		forbid: uapi.UBLK_F_USER_COPY | uapi.UBLK_F_SUPPORT_ZERO_COPY | uapi.UBLK_F_AUTO_BUF_REG | uapi.UBLK_F_BATCH_IO,
		rule:   "NEED_GET_DATA excludes USER_COPY, SUPPORT_ZERO_COPY, AUTO_BUF_REG and BATCH_IO (the kernel clears it silently)"},
	// "So far, UBLK_F_PER_IO_DAEMON won't be exposed for BATCH_IO".
	{when: uapi.UBLK_F_PER_IO_DAEMON, forbid: uapi.UBLK_F_BATCH_IO,
		rule: "PER_IO_DAEMON excludes BATCH_IO (the kernel clears it silently)"},
}

// ValidateFeatures checks a requested UBLK_F_* set against the constraints
// ublk_ctrl_add_dev enforces (v7.3-rc5), rejecting both combinations it fails
// with EINVAL and ones it silently rewrites. ioDescSize is the dev_info
// io_desc_size field: with UBLK_F_IO_DESC_SIZE it must be 24..256 and a
// multiple of 8 (alignof ublksrv_io_desc); without it, 0.
func ValidateFeatures(flags uint64, ioDescSize uint16) error {
	if err := validateFlagRules(flags); err != nil {
		return err
	}
	if flags&uapi.UBLK_F_IO_DESC_SIZE != 0 {
		if ioDescSize < 24 || ioDescSize > 256 || ioDescSize%8 != 0 {
			return &FeatureConflictError{Flags: flags, Rule: fmt.Sprintf("IO_DESC_SIZE needs io_desc_size 24..256, multiple of 8 (got %d) (EINVAL)", ioDescSize)}
		}
	} else if ioDescSize != 0 {
		return &FeatureConflictError{Flags: flags, Rule: fmt.Sprintf("io_desc_size %d without IO_DESC_SIZE would be ignored", ioDescSize)}
	}
	return nil
}

func validateFlagRules(flags uint64) error {
	for _, r := range featureRules {
		if flags&r.when != r.when {
			continue
		}
		if (r.needAny != 0 && flags&r.needAny == 0) || flags&r.forbid != 0 {
			return &FeatureConflictError{Flags: flags, Rule: r.rule}
		}
	}
	return nil
}

// Negotiate computes the ADD_DEV flags for a requested set: the request plus
// UBLK_F_CMD_IOCTL_ENCODE (we only send ioctl-encoded commands; the kernel
// has forced the bit on since v6.4 and never consults it). With a known
// feature set it returns *MissingFeaturesError for requested bits the kernel
// does not list; with an unknown one (pre-v6.5) it lets ADD_DEV decide.
// URING_CMD_COMP_IN_TASK is never added: the kernel forces it on and ignores
// it since v6.5 (and on v6.4 it only mattered for a modular build).
func Negotiate(requested uint64, fs FeatureSet) (uint64, error) {
	if err := validateFlagRules(requested); err != nil {
		return 0, err
	}
	if fs.Known {
		if missing := requested &^ fs.Flags; missing != 0 {
			return 0, &MissingFeaturesError{Requested: requested, Missing: missing, Supported: fs.Flags, Known: true, Source: "GET_FEATURES"}
		}
	}
	return requested | uapi.UBLK_F_CMD_IOCTL_ENCODE, nil
}
