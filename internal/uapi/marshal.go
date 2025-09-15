package uapi

import (
	"encoding/binary"
	"reflect"
	"unsafe"
)

// Marshal converts a struct to bytes using the system's native byte order
func Marshal(v interface{}) []byte {
	switch val := v.(type) {
	case *UblksrvCtrlCmd:
		return marshalCtrlCmd(val)
	case *UblksrvIOCmd:
		return marshalIOCmd(val)
	case *UblkParams:
		return marshalParams(val)
	case *UblksrvCtrlDevInfo:
		return marshalCtrlDevInfo(val)
	default:
		// Fallback: direct memory copy (unsafe but fast)
		return directMarshal(v)
	}
}

// Unmarshal converts bytes back to a struct
func Unmarshal(data []byte, v interface{}) error {
	switch val := v.(type) {
	case *UblksrvCtrlCmd:
		return unmarshalCtrlCmd(data, val)
	case *UblksrvIOCmd:
		return unmarshalIOCmd(data, val)
	case *UblkParams:
		return unmarshalParams(data, val)
	case *UblksrvCtrlDevInfo:
		return unmarshalCtrlDevInfo(data, val)
	default:
		// Fallback: direct memory copy
		return directUnmarshal(data, v)
	}
}

// marshalCtrlCmd manually marshals UblksrvCtrlCmd (32-byte C-compatible variant)
func marshalCtrlCmd(cmd *UblksrvCtrlCmd) []byte {
    buf := make([]byte, 32)

    binary.LittleEndian.PutUint32(buf[0:4], cmd.DevID)
    binary.LittleEndian.PutUint16(buf[4:6], cmd.QueueID)
    binary.LittleEndian.PutUint16(buf[6:8], cmd.Len)
    binary.LittleEndian.PutUint64(buf[8:16], cmd.Addr)
    binary.LittleEndian.PutUint64(buf[16:24], cmd.Data)
    binary.LittleEndian.PutUint16(buf[24:26], cmd.DevPathLen)
    binary.LittleEndian.PutUint16(buf[26:28], cmd.Pad)
    binary.LittleEndian.PutUint32(buf[28:32], cmd.Reserved)

    return buf
}

// unmarshalCtrlCmd manually unmarshals UblksrvCtrlCmd (32-byte C-compatible variant)
func unmarshalCtrlCmd(data []byte, cmd *UblksrvCtrlCmd) error {
    if len(data) < 32 {
        return ErrInsufficientData
    }

    cmd.DevID = binary.LittleEndian.Uint32(data[0:4])
    cmd.QueueID = binary.LittleEndian.Uint16(data[4:6])
    cmd.Len = binary.LittleEndian.Uint16(data[6:8])
    cmd.Addr = binary.LittleEndian.Uint64(data[8:16])
    cmd.Data = binary.LittleEndian.Uint64(data[16:24])
    cmd.DevPathLen = binary.LittleEndian.Uint16(data[24:26])
    cmd.Pad = binary.LittleEndian.Uint16(data[26:28])
    cmd.Reserved = binary.LittleEndian.Uint32(data[28:32])

    return nil
}

// marshalIOCmd manually marshals UblksrvIOCmd
func marshalIOCmd(cmd *UblksrvIOCmd) []byte {
	buf := make([]byte, 16)
	
	binary.LittleEndian.PutUint16(buf[0:2], cmd.QID)
	binary.LittleEndian.PutUint16(buf[2:4], cmd.Tag)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(cmd.Result))
	binary.LittleEndian.PutUint64(buf[8:16], cmd.Addr)
	
	return buf
}

// unmarshalIOCmd manually unmarshals UblksrvIOCmd
func unmarshalIOCmd(data []byte, cmd *UblksrvIOCmd) error {
	if len(data) < 16 {
		return ErrInsufficientData
	}
	
	cmd.QID = binary.LittleEndian.Uint16(data[0:2])
	cmd.Tag = binary.LittleEndian.Uint16(data[2:4])
	cmd.Result = int32(binary.LittleEndian.Uint32(data[4:8]))
	cmd.Addr = binary.LittleEndian.Uint64(data[8:16])
	
	return nil
}

// marshalParams handles the complex UblkParams structure
func marshalParams(params *UblkParams) []byte {
	// Calculate actual size based on types
	size := 8 // len + types
	if params.HasBasic() {
		size += int(unsafe.Sizeof(params.Basic))
	}
	if params.HasDiscard() {
		size += int(unsafe.Sizeof(params.Discard))
	}
	if params.HasDevt() {
		size += int(unsafe.Sizeof(params.Devt))
	}
	if params.HasZoned() {
		size += int(unsafe.Sizeof(params.Zoned))
	}
	
	buf := make([]byte, size)
	offset := 0
	
	// Marshal len and types
	binary.LittleEndian.PutUint32(buf[offset:offset+4], uint32(size))
	offset += 4
	binary.LittleEndian.PutUint32(buf[offset:offset+4], params.Types)
	offset += 4
	
	// Marshal each parameter type that's present
	if params.HasBasic() {
		basicBytes := directMarshal(&params.Basic)
		copy(buf[offset:], basicBytes)
		offset += len(basicBytes)
	}
	
	if params.HasDiscard() {
		discardBytes := directMarshal(&params.Discard)
		copy(buf[offset:], discardBytes)
		offset += len(discardBytes)
	}
	
	if params.HasDevt() {
		devtBytes := directMarshal(&params.Devt)
		copy(buf[offset:], devtBytes)
		offset += len(devtBytes)
	}
	
	if params.HasZoned() {
		zonedBytes := directMarshal(&params.Zoned)
		copy(buf[offset:], zonedBytes)
		offset += len(zonedBytes)
	}
	
	return buf
}

// unmarshalParams handles the complex UblkParams structure
func unmarshalParams(data []byte, params *UblkParams) error {
	if len(data) < 8 {
		return ErrInsufficientData
	}
	
	length := binary.LittleEndian.Uint32(data[0:4])
	params.Len = length
	params.Types = binary.LittleEndian.Uint32(data[4:8])
	
	if int(length) > len(data) {
		return ErrInsufficientData
	}
	
	offset := 8
	
	// Unmarshal each parameter type that's present
	if params.HasBasic() {
		if err := directUnmarshal(data[offset:], &params.Basic); err != nil {
			return err
		}
		offset += int(unsafe.Sizeof(params.Basic))
	}
	
	if params.HasDiscard() {
		if err := directUnmarshal(data[offset:], &params.Discard); err != nil {
			return err
		}
		offset += int(unsafe.Sizeof(params.Discard))
	}
	
	if params.HasDevt() {
		if err := directUnmarshal(data[offset:], &params.Devt); err != nil {
			return err
		}
		offset += int(unsafe.Sizeof(params.Devt))
	}
	
	if params.HasZoned() {
		if err := directUnmarshal(data[offset:], &params.Zoned); err != nil {
			return err
		}
		offset += int(unsafe.Sizeof(params.Zoned))
	}
	
	return nil
}

// directMarshal performs direct memory copy for marshaling
func directMarshal(v interface{}) []byte {
	// CRITICAL FIX: Need to dereference the interface to get actual struct pointer
	// The old code was marshaling the interface itself, not the struct!
	ptr := reflect.ValueOf(v).Pointer()
	size := int(reflect.TypeOf(v).Elem().Size())

	// Create a copy of the bytes from the actual struct
	buf := make([]byte, size)
	src := (*[1 << 20]byte)(unsafe.Pointer(ptr))
	copy(buf, src[:size])

	return buf
}

// directUnmarshal performs direct memory copy for unmarshaling
func directUnmarshal(data []byte, v interface{}) error {
	size := int(unsafe.Sizeof(v))
	if len(data) < size {
		return ErrInsufficientData
	}
	
	// Direct memory copy
	dst := (*[1 << 20]byte)(unsafe.Pointer(&v))
	copy(dst[:size], data[:size])
	
	return nil
}

// Error definitions
type MarshalError string

func (e MarshalError) Error() string {
	return string(e)
}

// marshalCtrlDevInfo manually marshals UblksrvCtrlDevInfo
func marshalCtrlDevInfo(info *UblksrvCtrlDevInfo) []byte {
	buf := make([]byte, 64) // Now exactly 64 bytes to match kernel 6.6+

	binary.LittleEndian.PutUint16(buf[0:2], info.NrHwQueues)
	binary.LittleEndian.PutUint16(buf[2:4], info.QueueDepth)
	binary.LittleEndian.PutUint16(buf[4:6], info.State)
	binary.LittleEndian.PutUint16(buf[6:8], info.Pad0)
	binary.LittleEndian.PutUint32(buf[8:12], info.MaxIOBufBytes)
	binary.LittleEndian.PutUint32(buf[12:16], info.DevID)
	binary.LittleEndian.PutUint32(buf[16:20], uint32(info.UblksrvPID))
	binary.LittleEndian.PutUint32(buf[20:24], info.Pad1)
	binary.LittleEndian.PutUint64(buf[24:32], info.Flags)
	binary.LittleEndian.PutUint64(buf[32:40], info.UblksrvFlags)
	binary.LittleEndian.PutUint32(buf[40:44], info.OwnerUID)
	binary.LittleEndian.PutUint32(buf[44:48], info.OwnerGID)
	binary.LittleEndian.PutUint64(buf[48:56], info.Reserved1)
	binary.LittleEndian.PutUint64(buf[56:64], info.Reserved2)

	return buf
}

// unmarshalCtrlDevInfo manually unmarshals UblksrvCtrlDevInfo
func unmarshalCtrlDevInfo(data []byte, info *UblksrvCtrlDevInfo) error {
    // Support both 64-byte and 80-byte layouts seen across kernels.
    if len(data) < 64 {
        return ErrInsufficientData
    }

    info.NrHwQueues = binary.LittleEndian.Uint16(data[0:2])
    info.QueueDepth = binary.LittleEndian.Uint16(data[2:4])
    info.State = binary.LittleEndian.Uint16(data[4:6])
    info.Pad0 = binary.LittleEndian.Uint16(data[6:8])
    info.MaxIOBufBytes = binary.LittleEndian.Uint32(data[8:12])
    info.DevID = binary.LittleEndian.Uint32(data[12:16])
    info.UblksrvPID = int32(binary.LittleEndian.Uint32(data[16:20]))
    info.Pad1 = binary.LittleEndian.Uint32(data[20:24])
    info.Flags = binary.LittleEndian.Uint64(data[24:32])
    info.UblksrvFlags = binary.LittleEndian.Uint64(data[32:40])

    // The 64-byte layout ends here at reserved[2].
    if len(data) >= 64 {
        info.Reserved1 = binary.LittleEndian.Uint64(data[48:56])
        info.Reserved2 = binary.LittleEndian.Uint64(data[56:64])
    }

    // Some kernels include owner uid/gid in a longer (80-byte) layout.
    if len(data) >= 80 {
        info.OwnerUID = binary.LittleEndian.Uint32(data[40:44])
        info.OwnerGID = binary.LittleEndian.Uint32(data[44:48])
        // bytes 64-80 may be padding
    }

    return nil
}

// MarshalCtrlDevInfo is a convenience function for external use
func MarshalCtrlDevInfo(info *UblksrvCtrlDevInfo) []byte {
	return marshalCtrlDevInfo(info)
}

// UnmarshalCtrlDevInfo is a convenience function for external use
func UnmarshalCtrlDevInfo(data []byte) *UblksrvCtrlDevInfo {
	info := &UblksrvCtrlDevInfo{}
	_ = unmarshalCtrlDevInfo(data, info)
	return info
}

const (
	ErrInsufficientData MarshalError = "insufficient data for unmarshaling"
	ErrInvalidType      MarshalError = "invalid type for marshaling"
)
