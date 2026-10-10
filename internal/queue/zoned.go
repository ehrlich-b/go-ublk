package queue

import (
	"encoding/binary"
	"fmt"
	"syscall"
)

// BlkZoneSize is the size of struct blk_zone (include/uapi/linux/blkzoned.h),
// the entry format of a zone report.
const BlkZoneSize = 64

// Zone types and conditions (enum blk_zone_type, enum blk_zone_cond).
const (
	ZoneTypeConventional  = 0x1
	ZoneTypeSeqWriteReq   = 0x2
	ZoneTypeSeqWritePref  = 0x3
	ZoneCondNotWP         = 0x0
	ZoneCondEmpty         = 0x1
	ZoneCondImpOpen       = 0x2
	ZoneCondExpOpen       = 0x3
	ZoneCondClosed        = 0x4
	ZoneCondReadOnly      = 0xd
	ZoneCondFull          = 0xe
	ZoneCondOffline       = 0xf
	zoneFlagCapacityIsSet = 0 // the capacity field is valid when non-zero (BLK_ZONE_REP_CAPACITY is a report flag)
)

// BlkZone is one zone of a zone report. Start, Len, WritePointer and
// Capacity are in bytes here; they are converted to 512-byte sectors in the
// report.
type BlkZone struct {
	Start        int64
	Len          int64
	WritePointer int64
	Type         uint8
	Cond         uint8
	NonSeq       bool
	Reset        bool
	Capacity     int64 // 0 means Len
}

// ReportZones fills an OpReportZones request's buffer with zones and
// completes it. Fewer zones than NrZones end the report.
//
// Deprecated: use RequestHandle.ReportZones for generation validation.
func (r *Request) ReportZones(zones []BlkZone) {
	if r.Op != OpReportZones {
		r.Complete(fmt.Errorf("ReportZones on a %s request: %w", r.Op, syscall.EINVAL))
		return
	}
	if err := r.Handle().ReportZones(zones); err != nil {
		panic(err)
	}
}

func fillZones(data []byte, zones []BlkZone) {
	max := len(data) / BlkZoneSize
	if len(zones) > max {
		zones = zones[:max]
	}
	for i, z := range zones {
		b := data[i*BlkZoneSize : (i+1)*BlkZoneSize]
		clear(b)
		capacity := z.Capacity
		if capacity == 0 {
			capacity = z.Len
		}
		binary.LittleEndian.PutUint64(b[0:], uint64(z.Start>>9))
		binary.LittleEndian.PutUint64(b[8:], uint64(z.Len>>9))
		binary.LittleEndian.PutUint64(b[16:], uint64(z.WritePointer>>9))
		b[24] = z.Type
		b[25] = z.Cond
		if z.NonSeq {
			b[26] = 1
		}
		if z.Reset {
			b[27] = 1
		}
		binary.LittleEndian.PutUint64(b[32:], uint64(capacity>>9))
	}
}
