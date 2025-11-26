package ublk

import "github.com/ehrlich-b/go-ublk/internal/constants"

// Re-export constants for public API
const (
	DefaultQueueDepth         = constants.DefaultQueueDepth
	DefaultLogicalBlockSize   = constants.DefaultLogicalBlockSize
	DefaultMaxIOSize          = constants.DefaultMaxIOSize
	DefaultDiscardAlignment   = constants.DefaultDiscardAlignment
	DefaultDiscardGranularity = constants.DefaultDiscardGranularity
	DefaultMaxDiscardSectors  = constants.DefaultMaxDiscardSectors
	DefaultMaxDiscardSegments = constants.DefaultMaxDiscardSegments
	AutoAssignDeviceID        = constants.AutoAssignDeviceID
	IOBufferSizePerTag        = constants.IOBufferSizePerTag
)
