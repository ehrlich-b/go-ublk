package validation

import (
	"fmt"
	"math"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

type RequestLimits struct {
	Capacity, FileBase           int64
	LogicalBlockSize, MaxPayload uint64
}

// Validate checks the negotiated device geometry independently of a request.
func (l RequestLimits) Validate() error {
	b := l.LogicalBlockSize
	if b < uapi.SectorSize || b&(b-1) != 0 || b > math.MaxInt64 || l.Capacity <= 0 || uint64(l.Capacity)%b != 0 {
		return fmt.Errorf("invalid capacity or logical block size")
	}
	if l.MaxPayload == 0 || l.MaxPayload > math.MaxInt32 || l.MaxPayload%b != 0 {
		return fmt.Errorf("invalid maximum payload")
	}
	if l.FileBase < 0 || l.FileBase > math.MaxInt64-l.Capacity {
		return fmt.Errorf("backing file base plus capacity exceeds signed addressing")
	}
	return nil
}

type RequestParams struct {
	Op                   uint8
	StartSector, Sectors uint64
	Limits               RequestLimits
}

type RequestRange struct {
	Offset, Length int64
}

// ValidateRequestRange converts descriptor sectors only after proving signed
// representability. Range-only operations are bounded by capacity, rather than
// the payload buffer (large DISCARD/WRITE_ZEROES requests move no payload).
// REPORT_ZONES uses nr_sectors as a zone count and caps its report buffer, as
// the engine has always done; it is not a sector range.
func ValidateRequestRange(p RequestParams) (RequestRange, error) {
	if err := p.Limits.Validate(); err != nil {
		return RequestRange{}, err
	}
	if p.StartSector > math.MaxInt64/uapi.SectorSize {
		return RequestRange{}, fmt.Errorf("start sector exceeds signed byte addressing")
	}
	off := p.StartSector * uapi.SectorSize
	capacity := uint64(p.Limits.Capacity)
	if off > capacity || off%p.Limits.LogicalBlockSize != 0 {
		return RequestRange{}, fmt.Errorf("request start is outside capacity or unaligned")
	}
	if p.Op == uapi.UBLK_IO_OP_REPORT_ZONES {
		if p.Sectors == 0 || p.Sectors > math.MaxUint32 || off == capacity {
			return RequestRange{}, fmt.Errorf("invalid zone count or report start")
		}
		return RequestRange{int64(off), int64(min(p.Sectors*64, p.Limits.MaxPayload))}, nil
	}
	if p.Sectors > math.MaxInt64/uapi.SectorSize {
		return RequestRange{}, fmt.Errorf("sector count exceeds signed byte addressing")
	}
	n := p.Sectors * uapi.SectorSize
	if n > capacity-off || n%p.Limits.LogicalBlockSize != 0 {
		return RequestRange{}, fmt.Errorf("request end exceeds capacity or length is unaligned")
	}
	switch p.Op {
	case uapi.UBLK_IO_OP_READ, uapi.UBLK_IO_OP_WRITE, uapi.UBLK_IO_OP_ZONE_APPEND:
		if n == 0 || n > p.Limits.MaxPayload {
			return RequestRange{}, fmt.Errorf("payload length %d outside 1..%d", n, p.Limits.MaxPayload)
		}
	case uapi.UBLK_IO_OP_FLUSH, uapi.UBLK_IO_OP_ZONE_RESET_ALL:
		if n != 0 {
			return RequestRange{}, fmt.Errorf("operation must not carry a sector range")
		}
	}
	return RequestRange{int64(off), int64(n)}, nil
}

// DispatchRequest is the production guard before buffer preparation, backend
// callbacks or zero-copy submission. Tests can supply a rejecting backend on
// Darwin without compiling the Linux transport.
func DispatchRequest(p RequestParams, dispatch func(RequestRange)) error {
	r, err := ValidateRequestRange(p)
	if err != nil {
		return err
	}
	dispatch(r)
	return nil
}

// MetadataLength bounds the per-request integrity span before multiplication
// or narrowing, including range-only operations with no payload buffer.
func MetadataLength(length, interval, metadata, size uint64) (int, error) {
	if interval == 0 || metadata == 0 || size > uint64(^uint(0)>>1) {
		return 0, fmt.Errorf("invalid integrity span geometry")
	}
	blocks := length / interval
	if blocks > size/metadata {
		return 0, fmt.Errorf("integrity span exceeds tag buffer")
	}
	return int(blocks * metadata), nil
}
