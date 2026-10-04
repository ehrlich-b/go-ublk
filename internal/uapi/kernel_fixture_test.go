package uapi

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"go/ast"
	"go/parser"
	"go/token"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unsafe"
)

// Embedded so the test does not depend on running from the package directory,
// which the cross-compiled binaries vm-test-unit copies to the VM do not.
//
//go:embed testdata/linux-le-fixtures.txt
var kernelFixtures string

// constants.go itself, so the test can list every constant it declares.
//
//go:embed constants.go
var constantsSource string

// headerConstants maps every numeric macro in ublk_cmd.h (and the LBMD_PI_*
// ones from fs.h) to the Go constant mirroring it. scripts/uapi-fixtures.sh
// refuses to generate fixtures unless the C program emits every macro in the
// header, so "every fixture const is here" means "every header macro is here".
var headerConstants = map[string]int64{
	"UBLK_CMD_GET_QUEUE_AFFINITY":        UBLK_CMD_GET_QUEUE_AFFINITY,
	"UBLK_CMD_GET_DEV_INFO":              UBLK_CMD_GET_DEV_INFO,
	"UBLK_CMD_ADD_DEV":                   UBLK_CMD_ADD_DEV,
	"UBLK_CMD_DEL_DEV":                   UBLK_CMD_DEL_DEV,
	"UBLK_CMD_START_DEV":                 UBLK_CMD_START_DEV,
	"UBLK_CMD_STOP_DEV":                  UBLK_CMD_STOP_DEV,
	"UBLK_CMD_SET_PARAMS":                UBLK_CMD_SET_PARAMS,
	"UBLK_CMD_GET_PARAMS":                UBLK_CMD_GET_PARAMS,
	"UBLK_CMD_START_USER_RECOVERY":       UBLK_CMD_START_USER_RECOVERY,
	"UBLK_CMD_END_USER_RECOVERY":         UBLK_CMD_END_USER_RECOVERY,
	"UBLK_CMD_GET_DEV_INFO2":             UBLK_CMD_GET_DEV_INFO2,
	"UBLK_U_CMD_GET_QUEUE_AFFINITY":      UBLK_U_CMD_GET_QUEUE_AFFINITY,
	"UBLK_U_CMD_GET_DEV_INFO":            UBLK_U_CMD_GET_DEV_INFO,
	"UBLK_U_CMD_ADD_DEV":                 UBLK_U_CMD_ADD_DEV,
	"UBLK_U_CMD_DEL_DEV":                 UBLK_U_CMD_DEL_DEV,
	"UBLK_U_CMD_START_DEV":               UBLK_U_CMD_START_DEV,
	"UBLK_U_CMD_STOP_DEV":                UBLK_U_CMD_STOP_DEV,
	"UBLK_U_CMD_SET_PARAMS":              UBLK_U_CMD_SET_PARAMS,
	"UBLK_U_CMD_GET_PARAMS":              UBLK_U_CMD_GET_PARAMS,
	"UBLK_U_CMD_START_USER_RECOVERY":     UBLK_U_CMD_START_USER_RECOVERY,
	"UBLK_U_CMD_END_USER_RECOVERY":       UBLK_U_CMD_END_USER_RECOVERY,
	"UBLK_U_CMD_GET_DEV_INFO2":           UBLK_U_CMD_GET_DEV_INFO2,
	"UBLK_U_CMD_GET_FEATURES":            UBLK_U_CMD_GET_FEATURES,
	"UBLK_U_CMD_DEL_DEV_ASYNC":           UBLK_U_CMD_DEL_DEV_ASYNC,
	"UBLK_U_CMD_UPDATE_SIZE":             UBLK_U_CMD_UPDATE_SIZE,
	"UBLK_U_CMD_QUIESCE_DEV":             UBLK_U_CMD_QUIESCE_DEV,
	"UBLK_U_CMD_TRY_STOP_DEV":            UBLK_U_CMD_TRY_STOP_DEV,
	"UBLK_U_CMD_REG_BUF":                 UBLK_U_CMD_REG_BUF,
	"UBLK_U_CMD_UNREG_BUF":               UBLK_U_CMD_UNREG_BUF,
	"UBLK_SHMEM_BUF_READ_ONLY":           UBLK_SHMEM_BUF_READ_ONLY,
	"UBLK_FEATURES_LEN":                  UBLK_FEATURES_LEN,
	"UBLK_IO_FETCH_REQ":                  UBLK_IO_FETCH_REQ,
	"UBLK_IO_COMMIT_AND_FETCH_REQ":       UBLK_IO_COMMIT_AND_FETCH_REQ,
	"UBLK_IO_NEED_GET_DATA":              UBLK_IO_NEED_GET_DATA,
	"UBLK_U_IO_FETCH_REQ":                UBLK_U_IO_FETCH_REQ,
	"UBLK_U_IO_COMMIT_AND_FETCH_REQ":     UBLK_U_IO_COMMIT_AND_FETCH_REQ,
	"UBLK_U_IO_NEED_GET_DATA":            UBLK_U_IO_NEED_GET_DATA,
	"UBLK_U_IO_REGISTER_IO_BUF":          UBLK_U_IO_REGISTER_IO_BUF,
	"UBLK_U_IO_UNREGISTER_IO_BUF":        UBLK_U_IO_UNREGISTER_IO_BUF,
	"UBLK_U_IO_PREP_IO_CMDS":             UBLK_U_IO_PREP_IO_CMDS,
	"UBLK_U_IO_COMMIT_IO_CMDS":           UBLK_U_IO_COMMIT_IO_CMDS,
	"UBLK_U_IO_FETCH_IO_CMDS":            UBLK_U_IO_FETCH_IO_CMDS,
	"UBLK_IO_RES_OK":                     UBLK_IO_RES_OK,
	"UBLK_IO_RES_NEED_GET_DATA":          UBLK_IO_RES_NEED_GET_DATA,
	"UBLK_IO_RES_ABORT":                  UBLK_IO_RES_ABORT,
	"UBLKSRV_CMD_BUF_OFFSET":             UBLKSRV_CMD_BUF_OFFSET,
	"UBLKSRV_IO_BUF_OFFSET":              UBLKSRV_IO_BUF_OFFSET,
	"UBLK_MAX_QUEUE_DEPTH":               UBLK_MAX_QUEUE_DEPTH,
	"UBLK_IO_BUF_OFF":                    UBLK_IO_BUF_OFF,
	"UBLK_IO_BUF_BITS":                   UBLK_IO_BUF_BITS,
	"UBLK_IO_BUF_BITS_MASK":              UBLK_IO_BUF_BITS_MASK,
	"UBLK_TAG_OFF":                       UBLK_TAG_OFF,
	"UBLK_TAG_BITS":                      UBLK_TAG_BITS,
	"UBLK_TAG_BITS_MASK":                 UBLK_TAG_BITS_MASK,
	"UBLK_QID_OFF":                       UBLK_QID_OFF,
	"UBLK_QID_BITS":                      UBLK_QID_BITS,
	"UBLK_QID_BITS_MASK":                 UBLK_QID_BITS_MASK,
	"UBLK_MAX_NR_QUEUES":                 UBLK_MAX_NR_QUEUES,
	"UBLKSRV_IO_BUF_TOTAL_BITS":          UBLKSRV_IO_BUF_TOTAL_BITS,
	"UBLKSRV_IO_BUF_TOTAL_SIZE":          UBLKSRV_IO_BUF_TOTAL_SIZE,
	"UBLK_INTEGRITY_FLAG_OFF":            UBLK_INTEGRITY_FLAG_OFF,
	"UBLKSRV_IO_INTEGRITY_FLAG":          UBLKSRV_IO_INTEGRITY_FLAG,
	"UBLK_F_SUPPORT_ZERO_COPY":           UBLK_F_SUPPORT_ZERO_COPY,
	"UBLK_F_URING_CMD_COMP_IN_TASK":      UBLK_F_URING_CMD_COMP_IN_TASK,
	"UBLK_F_NEED_GET_DATA":               UBLK_F_NEED_GET_DATA,
	"UBLK_F_USER_RECOVERY":               UBLK_F_USER_RECOVERY,
	"UBLK_F_USER_RECOVERY_REISSUE":       UBLK_F_USER_RECOVERY_REISSUE,
	"UBLK_F_UNPRIVILEGED_DEV":            UBLK_F_UNPRIVILEGED_DEV,
	"UBLK_F_CMD_IOCTL_ENCODE":            UBLK_F_CMD_IOCTL_ENCODE,
	"UBLK_F_USER_COPY":                   UBLK_F_USER_COPY,
	"UBLK_F_ZONED":                       UBLK_F_ZONED,
	"UBLK_F_USER_RECOVERY_FAIL_IO":       UBLK_F_USER_RECOVERY_FAIL_IO,
	"UBLK_F_UPDATE_SIZE":                 UBLK_F_UPDATE_SIZE,
	"UBLK_F_AUTO_BUF_REG":                UBLK_F_AUTO_BUF_REG,
	"UBLK_F_QUIESCE":                     UBLK_F_QUIESCE,
	"UBLK_F_PER_IO_DAEMON":               UBLK_F_PER_IO_DAEMON,
	"UBLK_F_BUF_REG_OFF_DAEMON":          UBLK_F_BUF_REG_OFF_DAEMON,
	"UBLK_F_BATCH_IO":                    UBLK_F_BATCH_IO,
	"UBLK_F_INTEGRITY":                   UBLK_F_INTEGRITY,
	"UBLK_F_SAFE_STOP_DEV":               UBLK_F_SAFE_STOP_DEV,
	"UBLK_F_NO_AUTO_PART_SCAN":           UBLK_F_NO_AUTO_PART_SCAN,
	"UBLK_F_SHMEM_ZC":                    UBLK_F_SHMEM_ZC,
	"UBLK_F_IO_DESC_SIZE":                UBLK_F_IO_DESC_SIZE,
	"UBLK_S_DEV_DEAD":                    UBLK_S_DEV_DEAD,
	"UBLK_S_DEV_LIVE":                    UBLK_S_DEV_LIVE,
	"UBLK_S_DEV_QUIESCED":                UBLK_S_DEV_QUIESCED,
	"UBLK_S_DEV_FAIL_IO":                 UBLK_S_DEV_FAIL_IO,
	"UBLK_IO_OP_READ":                    UBLK_IO_OP_READ,
	"UBLK_IO_OP_WRITE":                   UBLK_IO_OP_WRITE,
	"UBLK_IO_OP_FLUSH":                   UBLK_IO_OP_FLUSH,
	"UBLK_IO_OP_DISCARD":                 UBLK_IO_OP_DISCARD,
	"UBLK_IO_OP_WRITE_SAME":              UBLK_IO_OP_WRITE_SAME,
	"UBLK_IO_OP_WRITE_ZEROES":            UBLK_IO_OP_WRITE_ZEROES,
	"UBLK_IO_OP_ZONE_OPEN":               UBLK_IO_OP_ZONE_OPEN,
	"UBLK_IO_OP_ZONE_CLOSE":              UBLK_IO_OP_ZONE_CLOSE,
	"UBLK_IO_OP_ZONE_FINISH":             UBLK_IO_OP_ZONE_FINISH,
	"UBLK_IO_OP_ZONE_APPEND":             UBLK_IO_OP_ZONE_APPEND,
	"UBLK_IO_OP_ZONE_RESET_ALL":          UBLK_IO_OP_ZONE_RESET_ALL,
	"UBLK_IO_OP_ZONE_RESET":              UBLK_IO_OP_ZONE_RESET,
	"UBLK_IO_OP_REPORT_ZONES":            UBLK_IO_OP_REPORT_ZONES,
	"UBLK_IO_F_FAILFAST_DEV":             UBLK_IO_F_FAILFAST_DEV,
	"UBLK_IO_F_FAILFAST_TRANSPORT":       UBLK_IO_F_FAILFAST_TRANSPORT,
	"UBLK_IO_F_FAILFAST_DRIVER":          UBLK_IO_F_FAILFAST_DRIVER,
	"UBLK_IO_F_META":                     UBLK_IO_F_META,
	"UBLK_IO_F_FUA":                      UBLK_IO_F_FUA,
	"UBLK_IO_F_NOUNMAP":                  UBLK_IO_F_NOUNMAP,
	"UBLK_IO_F_SWAP":                     UBLK_IO_F_SWAP,
	"UBLK_IO_F_NEED_REG_BUF":             UBLK_IO_F_NEED_REG_BUF,
	"UBLK_IO_F_INTEGRITY":                UBLK_IO_F_INTEGRITY,
	"UBLK_IO_F_SHMEM_ZC":                 UBLK_IO_F_SHMEM_ZC,
	"UBLK_AUTO_BUF_REG_FALLBACK":         UBLK_AUTO_BUF_REG_FALLBACK,
	"UBLK_AUTO_BUF_REG_F_MASK":           UBLK_AUTO_BUF_REG_F_MASK,
	"UBLK_BATCH_F_HAS_ZONE_LBA":          UBLK_BATCH_F_HAS_ZONE_LBA,
	"UBLK_BATCH_F_HAS_BUF_ADDR":          UBLK_BATCH_F_HAS_BUF_ADDR,
	"UBLK_BATCH_F_AUTO_BUF_REG_FALLBACK": UBLK_BATCH_F_AUTO_BUF_REG_FALLBACK,
	"UBLK_ATTR_READ_ONLY":                UBLK_ATTR_READ_ONLY,
	"UBLK_ATTR_ROTATIONAL":               UBLK_ATTR_ROTATIONAL,
	"UBLK_ATTR_VOLATILE_CACHE":           UBLK_ATTR_VOLATILE_CACHE,
	"UBLK_ATTR_FUA":                      UBLK_ATTR_FUA,
	"UBLK_MIN_SEGMENT_SIZE":              UBLK_MIN_SEGMENT_SIZE,
	"UBLK_PARAM_TYPE_BASIC":              UBLK_PARAM_TYPE_BASIC,
	"UBLK_PARAM_TYPE_DISCARD":            UBLK_PARAM_TYPE_DISCARD,
	"UBLK_PARAM_TYPE_DEVT":               UBLK_PARAM_TYPE_DEVT,
	"UBLK_PARAM_TYPE_ZONED":              UBLK_PARAM_TYPE_ZONED,
	"UBLK_PARAM_TYPE_DMA_ALIGN":          UBLK_PARAM_TYPE_DMA_ALIGN,
	"UBLK_PARAM_TYPE_SEGMENT":            UBLK_PARAM_TYPE_SEGMENT,
	"UBLK_PARAM_TYPE_INTEGRITY":          UBLK_PARAM_TYPE_INTEGRITY,
	"UBLK_SHMEM_ZC_OFF_MASK":             UBLK_SHMEM_ZC_OFF_MASK,
	"UBLK_SHMEM_ZC_IDX_OFF":              UBLK_SHMEM_ZC_IDX_OFF,
	"UBLK_SHMEM_ZC_IDX_MASK":             UBLK_SHMEM_ZC_IDX_MASK,
	"LBMD_PI_CAP_INTEGRITY":              LBMD_PI_CAP_INTEGRITY,
	"LBMD_PI_CAP_REFTAG":                 LBMD_PI_CAP_REFTAG,
	"LBMD_PI_CSUM_NONE":                  LBMD_PI_CSUM_NONE,
	"LBMD_PI_CSUM_IP":                    LBMD_PI_CSUM_IP,
	"LBMD_PI_CSUM_CRC16_T10DIF":          LBMD_PI_CSUM_CRC16_T10DIF,
	"LBMD_PI_CSUM_CRC64_NVME":            LBMD_PI_CSUM_CRC64_NVME,
}

// derivedConstants are Go names for values the header only defines
// implicitly: the driver-private command numbers, i.e. _IOC_NR of a
// UBLK_U_* opcode the header does define.
var derivedConstants = map[string]struct {
	value  int64
	opcode string
}{
	"UBLK_CMD_GET_FEATURES":     {UBLK_CMD_GET_FEATURES, "UBLK_U_CMD_GET_FEATURES"},
	"UBLK_CMD_DEL_DEV_ASYNC":    {UBLK_CMD_DEL_DEV_ASYNC, "UBLK_U_CMD_DEL_DEV_ASYNC"},
	"UBLK_CMD_UPDATE_SIZE":      {UBLK_CMD_UPDATE_SIZE, "UBLK_U_CMD_UPDATE_SIZE"},
	"UBLK_CMD_QUIESCE_DEV":      {UBLK_CMD_QUIESCE_DEV, "UBLK_U_CMD_QUIESCE_DEV"},
	"UBLK_CMD_TRY_STOP_DEV":     {UBLK_CMD_TRY_STOP_DEV, "UBLK_U_CMD_TRY_STOP_DEV"},
	"UBLK_CMD_REG_BUF":          {UBLK_CMD_REG_BUF, "UBLK_U_CMD_REG_BUF"},
	"UBLK_CMD_UNREG_BUF":        {UBLK_CMD_UNREG_BUF, "UBLK_U_CMD_UNREG_BUF"},
	"UBLK_IO_REGISTER_IO_BUF":   {UBLK_IO_REGISTER_IO_BUF, "UBLK_U_IO_REGISTER_IO_BUF"},
	"UBLK_IO_UNREGISTER_IO_BUF": {UBLK_IO_UNREGISTER_IO_BUF, "UBLK_U_IO_UNREGISTER_IO_BUF"},
	"UBLK_IO_PREP_IO_CMDS":      {UBLK_IO_PREP_IO_CMDS, "UBLK_U_IO_PREP_IO_CMDS"},
	"UBLK_IO_COMMIT_IO_CMDS":    {UBLK_IO_COMMIT_IO_CMDS, "UBLK_U_IO_COMMIT_IO_CMDS"},
	"UBLK_IO_FETCH_IO_CMDS":     {UBLK_IO_FETCH_IO_CMDS, "UBLK_U_IO_FETCH_IO_CMDS"},
}

// fixtureStructs maps each C struct to its Go mirror, member by member.
// Union alternatives map to the one Go field that holds the union.
var fixtureStructs = map[string]struct {
	typ     reflect.Type
	members map[string]string
}{
	"ublksrv_ctrl_cmd": {reflect.TypeOf(UblksrvCtrlCmd{}), map[string]string{
		"dev_id": "DevID", "queue_id": "QueueID", "len": "Len", "addr": "Addr", "data": "Data",
		"dev_path_len": "DevPathLen", "pad": "Pad", "reserved": "Reserved"}},
	"ublksrv_ctrl_dev_info": {reflect.TypeOf(UblksrvCtrlDevInfo{}), map[string]string{
		"nr_hw_queues": "NrHwQueues", "queue_depth": "QueueDepth", "state": "State",
		"io_desc_size": "IODescSize", "max_io_buf_bytes": "MaxIOBufBytes", "dev_id": "DevID",
		"ublksrv_pid": "UblksrvPID", "pad1": "Pad1", "flags": "Flags", "ublksrv_flags": "UblksrvFlags",
		"owner_uid": "OwnerUID", "owner_gid": "OwnerGID", "reserved1": "Reserved1", "reserved2": "Reserved2"}},
	"ublksrv_io_desc": {reflect.TypeOf(UblksrvIODesc{}), map[string]string{
		"op_flags": "OpFlags", "nr_sectors": "NrSectors", "nr_zones": "NrSectors",
		"start_sector": "StartSector", "addr": "Addr"}},
	"ublksrv_io_cmd": {reflect.TypeOf(UblksrvIOCmd{}), map[string]string{
		"q_id": "QID", "tag": "Tag", "result": "Result", "addr": "Addr", "zone_append_lba": "Addr"}},
	"ublk_elem_header": {reflect.TypeOf(UblkElemHeader{}), map[string]string{
		"tag": "Tag", "buf_index": "BufIndex", "result": "Result"}},
	"ublk_batch_io": {reflect.TypeOf(UblkBatchIO{}), map[string]string{
		"q_id": "QID", "flags": "Flags", "nr_elem": "NrElem", "elem_bytes": "ElemBytes",
		"reserved": "Reserved", "reserved2": "Reserved2"}},
	"ublk_auto_buf_reg": {reflect.TypeOf(UblkAutoBufReg{}), map[string]string{
		"index": "Index", "flags": "Flags", "reserved0": "Reserved0", "reserved1": "Reserved1"}},
	"ublk_shmem_buf_reg": {reflect.TypeOf(UblkShmemBufReg{}), map[string]string{
		"addr": "Addr", "len": "Len", "flags": "Flags", "reserved": "Reserved"}},
	"ublk_param_basic": {reflect.TypeOf(UblkParamBasic{}), map[string]string{
		"attrs": "Attrs", "logical_bs_shift": "LogicalBSShift", "physical_bs_shift": "PhysicalBSShift",
		"io_opt_shift": "IOOptShift", "io_min_shift": "IOMinShift", "max_sectors": "MaxSectors",
		"chunk_sectors": "ChunkSectors", "dev_sectors": "DevSectors", "virt_boundary_mask": "VirtBoundaryMask"}},
	"ublk_param_discard": {reflect.TypeOf(UblkParamDiscard{}), map[string]string{
		"discard_alignment": "DiscardAlignment", "discard_granularity": "DiscardGranularity",
		"max_discard_sectors": "MaxDiscardSectors", "max_write_zeroes_sectors": "MaxWriteZeroesSectors",
		"max_discard_segments": "MaxDiscardSegments", "reserved0": "Reserved0"}},
	"ublk_param_devt": {reflect.TypeOf(UblkParamDevt{}), map[string]string{
		"char_major": "CharMajor", "char_minor": "CharMinor", "disk_major": "DiskMajor", "disk_minor": "DiskMinor"}},
	"ublk_param_zoned": {reflect.TypeOf(UblkParamZoned{}), map[string]string{
		"max_open_zones": "MaxOpenZones", "max_active_zones": "MaxActiveZones",
		"max_zone_append_sectors": "MaxZoneAppendSectors", "reserved": "Reserved"}},
	"ublk_param_dma_align": {reflect.TypeOf(UblkParamDMAAlign{}), map[string]string{
		"alignment": "Alignment", "pad": "Pad"}},
	"ublk_param_segment": {reflect.TypeOf(UblkParamSegment{}), map[string]string{
		"seg_boundary_mask": "SegBoundaryMask", "max_segment_size": "MaxSegmentSize",
		"max_segments": "MaxSegments", "pad": "Pad"}},
	"ublk_param_integrity": {reflect.TypeOf(UblkParamIntegrity{}), map[string]string{
		"flags": "Flags", "max_integrity_segments": "MaxIntegritySegments", "interval_exp": "IntervalExp",
		"metadata_size": "MetadataSize", "pi_offset": "PIOffset", "csum_type": "CsumType",
		"tag_size": "TagSize", "pad": "Pad"}},
	"ublk_params": {reflect.TypeOf(UblkParams{}), map[string]string{
		"len": "Len", "types": "Types", "basic": "Basic", "discard": "Discard", "devt": "Devt",
		"zoned": "Zoned", "dma": "DMA", "seg": "Seg", "integrity": "Integrity"}},
}

// fixtureValues mirrors the struct values filled in by uapi-fixtures.c.
func fixtureValues() map[string]interface{} {
	basic := fixtureParams(UBLK_PARAM_TYPE_BASIC)
	devt := fixtureParams(UBLK_PARAM_TYPE_BASIC | UBLK_PARAM_TYPE_DEVT)
	all := UblkParams{
		Types: allParamTypes,
		Basic: UblkParamBasic{Attrs: 0x0f, LogicalBSShift: 9, PhysicalBSShift: 12, IOOptShift: 16, IOMinShift: 12,
			MaxSectors: 0x800, ChunkSectors: 0x100, DevSectors: 0x1122334455667788, VirtBoundaryMask: 0xfff},
		Discard: UblkParamDiscard{DiscardAlignment: 0x1000, DiscardGranularity: 0x2000, MaxDiscardSectors: 0x30000,
			MaxWriteZeroesSectors: 0x40000, MaxDiscardSegments: 1, Reserved0: 0x5a5a},
		Devt:      UblkParamDevt{CharMajor: 0x11223344, CharMinor: 0x55667788, DiskMajor: 0x99aabbcc, DiskMinor: 0xddeeff00},
		Zoned:     UblkParamZoned{MaxOpenZones: 0x10, MaxActiveZones: 0x20, MaxZoneAppendSectors: 0x30},
		DMA:       UblkParamDMAAlign{Alignment: 0x1ff, Pad: [4]uint8{0xa1, 0xa2, 0xa3, 0xa4}},
		Seg:       UblkParamSegment{SegBoundaryMask: 0xffffffff, MaxSegmentSize: 0x10000, MaxSegments: 0x80, Pad: [2]uint8{0xb1, 0xb2}},
		Integrity: UblkParamIntegrity{Flags: LBMD_PI_CAP_INTEGRITY | LBMD_PI_CAP_REFTAG, MaxIntegritySegments: 0x0102, IntervalExp: 12, MetadataSize: 8, CsumType: LBMD_PI_CSUM_CRC64_NVME, TagSize: 2, Pad: [5]uint8{0xc1, 0xc2, 0xc3, 0xc4, 0xc5}},
	}
	for i := range all.Zoned.Reserved {
		all.Zoned.Reserved[i] = 0x40 + uint8(i)
	}
	return map[string]interface{}{
		"ctrl": &UblksrvCtrlCmd{DevID: 0x12345678, QueueID: 0xabcd, Len: 0x1234, Addr: 0x1122334455667788,
			Data: 0x8877665544332211, DevPathLen: 0x0102, Pad: 0x0304, Reserved: 0x05060708},
		"dev_info": &UblksrvCtrlDevInfo{NrHwQueues: 0x0102, QueueDepth: 0x0304, State: 0x0506, IODescSize: 0x0708,
			MaxIOBufBytes: 0x090a0b0c, DevID: 0x0d0e0f10, UblksrvPID: -2, Pad1: 0x11121314,
			Flags: 0x15161718191a1b1c, UblksrvFlags: 0x1d1e1f2021222324, OwnerUID: 0x25262728, OwnerGID: 0x292a2b2c,
			Reserved1: 0x2d2e2f3031323334, Reserved2: 0x35363738393a3b3c},
		"io_desc":       &UblksrvIODesc{OpFlags: 0x01020304, NrSectors: 0x05060708, StartSector: 0x090a0b0c0d0e0f10, Addr: 0x1112131415161718},
		"io":            &UblksrvIOCmd{QID: 0x0123, Tag: 0xfedc, Result: -5, Addr: 0x8877665544332211},
		"elem_header":   &UblkElemHeader{Tag: 0x0102, BufIndex: 0x0304, Result: -7},
		"batch_io":      &UblkBatchIO{QID: 0x0102, Flags: 0x0304, NrElem: 0x0506, ElemBytes: 0x07, Reserved: 0x08, Reserved2: 0x090a0b0c0d0e0f10},
		"auto_buf_reg":  &UblkAutoBufReg{Index: 0x0102, Flags: 0x03, Reserved0: 0x04, Reserved1: 0x05060708},
		"shmem_buf_reg": &UblkShmemBufReg{Addr: 0x0102030405060708, Len: 0x1112131415161718, Flags: 0x21222324, Reserved: 0x25262728},
		"basic":         &basic, "basic_devt": &devt, "params_all": &all,
	}
}

type fixtureLine struct {
	kind, name string
	args       []string
}

func parseFixtures(t *testing.T) []fixtureLine {
	t.Helper()
	var lines []fixtureLine
	for _, raw := range strings.Split(kernelFixtures, "\n") {
		if raw == "" || strings.HasPrefix(raw, "#") {
			continue
		}
		f := strings.Fields(raw)
		if len(f) < 3 {
			t.Fatalf("bad fixture line %q", raw)
		}
		lines = append(lines, fixtureLine{f[0], f[1], f[2:]})
	}
	return lines
}

func fixtureInt(t *testing.T, s string) int64 {
	t.Helper()
	if v, err := strconv.ParseInt(s, 0, 64); err == nil {
		return v
	}
	v, err := strconv.ParseUint(s, 0, 64)
	if err != nil {
		t.Fatalf("bad fixture number %q", s)
	}
	return int64(v)
}

// TestKernelCFixtures compares every constant, struct size, member offset and
// size, filled-struct byte image, and inline-helper result emitted by
// scripts/uapi-fixtures.c against the Linux v7.3-rc5 header with this package.
func TestKernelCFixtures(t *testing.T) {
	lines := parseFixtures(t)
	values := fixtureValues()
	consts := map[string]int64{}
	seenMembers := map[string]map[string]bool{}
	for _, l := range lines {
		switch l.kind {
		case "const":
			got, ok := headerConstants[l.name]
			want := fixtureInt(t, l.args[0])
			consts[l.name] = want
			if !ok {
				t.Errorf("header constant %s = %d has no Go mirror in headerConstants", l.name, want)
			} else if got != want {
				t.Errorf("%s = %d, header says %d", l.name, got, want)
			}
		case "sizeof":
			s, ok := fixtureStructs[l.name]
			if !ok {
				t.Errorf("header struct %s has no Go mirror", l.name)
				continue
			}
			want := uintptr(fixtureInt(t, l.args[0]))
			if l.name == "ublk_params" && unsafe.Sizeof(uintptr(0)) != 8 {
				continue // the Go struct only matches C on 64-bit; the wire layout is fixed
			}
			if s.typ.Size() != want {
				t.Errorf("sizeof %s: Go %d, C %d", l.name, s.typ.Size(), want)
			}
		case "field":
			structName, member, _ := strings.Cut(l.name, ".")
			s, ok := fixtureStructs[structName]
			if !ok {
				t.Errorf("header struct %s has no Go mirror", structName)
				continue
			}
			goName, ok := s.members[member]
			if !ok {
				t.Errorf("header member %s has no Go mirror", l.name)
				continue
			}
			f, _ := s.typ.FieldByName(goName)
			off, size := uintptr(fixtureInt(t, l.args[0])), uintptr(fixtureInt(t, l.args[1]))
			if f.Type.Size() != size {
				t.Errorf("%s size: Go %s %d, C %d", l.name, goName, f.Type.Size(), size)
			}
			if structName == "ublk_params" {
				checkParamsWireOffset(t, member, int(off), int(size))
				if unsafe.Sizeof(uintptr(0)) != 8 {
					continue
				}
			}
			if f.Offset != off {
				t.Errorf("%s offset: Go %s %d, C %d", l.name, goName, f.Offset, off)
			}
			if seenMembers[structName] == nil {
				seenMembers[structName] = map[string]bool{}
			}
			seenMembers[structName][goName] = true
		case "bytes":
			value, ok := values[l.name]
			if !ok {
				t.Errorf("unknown byte fixture %s", l.name)
				continue
			}
			delete(values, l.name)
			want, err := hex.DecodeString(l.args[0])
			if err != nil {
				t.Fatal(err)
			}
			if got := Marshal(value); !bytes.Equal(got, want) {
				t.Errorf("%s C fixture mismatch:\n got %x\nwant %x", l.name, got, want)
			}
			assertMarshalIntoMatches(t, value, want)
			back := reflect.New(reflect.TypeOf(value).Elem()).Interface()
			if err := Unmarshal(want, back); err != nil {
				t.Errorf("%s: Unmarshal: %v", l.name, err)
			}
			if !bytes.Equal(Marshal(back), want) {
				t.Errorf("%s: Unmarshal of the C bytes does not re-marshal to them", l.name)
			}
		case "helper":
			checkFixtureHelper(t, l)
		default:
			t.Errorf("unknown fixture kind %q", l.kind)
		}
	}
	if len(values) != 0 {
		t.Errorf("byte fixtures missing from the C output: %v", values)
	}
	for name := range headerConstants {
		if _, ok := consts[name]; !ok {
			t.Errorf("headerConstants has %s, which the header fixtures do not define", name)
		}
	}
	for name, d := range derivedConstants {
		op, ok := consts[d.opcode]
		if !ok {
			t.Errorf("%s: opcode %s missing from fixtures", name, d.opcode)
		} else if d.value != op&0xff {
			t.Errorf("%s = %#x, want _IOC_NR(%s) = %#x", name, d.value, d.opcode, op&0xff)
		}
	}
	// Every Go field of every mirrored struct must correspond to a C member.
	for cName, s := range fixtureStructs {
		if cName == "ublk_params" && unsafe.Sizeof(uintptr(0)) != 8 {
			continue
		}
		for i := 0; i < s.typ.NumField(); i++ {
			if !seenMembers[cName][s.typ.Field(i).Name] {
				t.Errorf("Go field %s.%s matches no C member of %s", s.typ.Name(), s.typ.Field(i).Name, cName)
			}
		}
	}
}

// checkParamsWireOffset ties marshal.go's fixed wire offsets to the C layout.
func checkParamsWireOffset(t *testing.T, member string, off, size int) {
	t.Helper()
	bits := map[string]uint32{
		"basic": UBLK_PARAM_TYPE_BASIC, "discard": UBLK_PARAM_TYPE_DISCARD, "devt": UBLK_PARAM_TYPE_DEVT,
		"zoned": UBLK_PARAM_TYPE_ZONED, "dma": UBLK_PARAM_TYPE_DMA_ALIGN, "seg": UBLK_PARAM_TYPE_SEGMENT,
		"integrity": UBLK_PARAM_TYPE_INTEGRITY,
	}
	bit, ok := bits[member]
	if !ok {
		return // len, types: the 8-byte header
	}
	for _, b := range paramBlockTable {
		if b.bit == bit {
			if b.start != off || b.end != off+size {
				t.Errorf("wire block %s = [%d,%d), C [%d,%d)", member, b.start, b.end, off, off+size)
			}
			return
		}
	}
	t.Errorf("no wire block for ublk_params.%s", member)
}

func checkFixtureHelper(t *testing.T, l fixtureLine) {
	t.Helper()
	a := make([]uint64, len(l.args))
	for i, s := range l.args {
		a[i] = uint64(fixtureInt(t, s))
	}
	switch l.name {
	case "ublk_auto_buf_reg_to_sqe_addr":
		r := UblkAutoBufReg{Index: uint16(a[0]), Flags: uint8(a[1]), Reserved0: uint8(a[2]), Reserved1: uint32(a[3])}
		if r.SQEAddr() != a[4] {
			t.Errorf("SQEAddr(%+v) = %#x, C %#x", r, r.SQEAddr(), a[4])
		}
	case "ublk_sqe_addr_to_auto_buf_reg":
		want := UblkAutoBufReg{Index: uint16(a[1]), Flags: uint8(a[2]), Reserved0: uint8(a[3]), Reserved1: uint32(a[4])}
		if got := AutoBufRegFromSQEAddr(a[0]); got != want {
			t.Errorf("AutoBufRegFromSQEAddr(%#x) = %+v, C %+v", a[0], got, want)
		}
	case "ublk_shmem_zc_addr":
		if got := ShmemZCAddr(uint16(a[0]), uint32(a[1])); got != a[2] {
			t.Errorf("ShmemZCAddr = %#x, C %#x", got, a[2])
		}
	case "ublk_shmem_zc_index":
		if got := ShmemZCIndex(a[0]); uint64(got) != a[1] {
			t.Errorf("ShmemZCIndex = %#x, C %#x", got, a[1])
		}
	case "ublk_shmem_zc_offset":
		if got := ShmemZCOffset(a[0]); uint64(got) != a[1] {
			t.Errorf("ShmemZCOffset = %#x, C %#x", got, a[1])
		}
	case "ublksrv_get_op_flags":
		d := UblksrvIODesc{OpFlags: uint32(a[0])}
		if uint64(d.GetOp()) != a[1] || uint64(d.GetFlags()) != a[2] {
			t.Errorf("GetOp/GetFlags(%#x) = %#x/%#x, C %#x/%#x", a[0], d.GetOp(), d.GetFlags(), a[1], a[2])
		}
	case "user_copy_offset":
		pos, ok := UserCopyOffset(uint16(a[0]), uint16(a[1]), uint32(a[2]))
		ipos, iok := UserCopyIntegrityOffset(uint16(a[0]), uint16(a[1]), uint32(a[2]))
		if !ok || !iok || uint64(pos) != a[3] || uint64(ipos) != a[4] {
			t.Errorf("UserCopyOffset = %#x/%#x, C %#x/%#x", pos, ipos, a[3], a[4])
		}
	default:
		t.Errorf("unknown helper fixture %s", l.name)
	}
}

// TestEveryGoConstantIsChecked parses constants.go and fails on any UAPI-style
// constant that neither mirrors a header macro nor is derived from one, so a
// constant added here without a header counterpart is noticed.
func TestEveryGoConstantIsChecked(t *testing.T) {
	file, err := parser.ParseFile(token.NewFileSet(), "constants.go", constantsSource, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range file.Decls {
		gen, ok := decl.(*ast.GenDecl)
		if !ok || gen.Tok != token.CONST {
			continue
		}
		for _, spec := range gen.Specs {
			for _, name := range spec.(*ast.ValueSpec).Names {
				n := name.Name
				if !strings.HasPrefix(n, "UBLK") && !strings.HasPrefix(n, "LBMD_") {
					continue
				}
				if _, ok := headerConstants[n]; ok {
					continue
				}
				if _, ok := derivedConstants[n]; ok {
					continue
				}
				t.Errorf("constant %s is not checked against the header", n)
			}
		}
	}
}
