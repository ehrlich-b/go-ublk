package uapi

import (
	"encoding/binary"
	"reflect"
	"unsafe"
)

// Marshal converts a UAPI struct to bytes on the supported little-endian hosts.
// Explicit command fields use little-endian order; parameter blocks and the
// direct-copy fallback use native order. Big-endian hosts are not supported.
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

// MarshalInto marshals v into the caller-provided buffer buf without
// allocating. It writes the same bytes as Marshal and returns the number of
// bytes written. buf must be large enough (see the compile-time struct sizes);
// otherwise ErrBufferTooSmall is returned and buf is left untouched.
func MarshalInto(v interface{}, buf []byte) (int, error) {
	switch val := v.(type) {
	case *UblksrvCtrlCmd:
		return marshalCtrlCmdInto(val, buf)
	case *UblksrvIOCmd:
		return marshalIOCmdInto(val, buf)
	case *UblkParams:
		return marshalParamsInto(val, buf)
	case *UblksrvCtrlDevInfo:
		return marshalCtrlDevInfoInto(val, buf)
	default:
		// Fallback: direct memory copy
		return directMarshalInto(v, buf)
	}
}

// marshalCtrlCmd manually marshals UblksrvCtrlCmd (32-byte C-compatible variant)
func marshalCtrlCmd(cmd *UblksrvCtrlCmd) []byte {
	buf := make([]byte, 32)
	_, _ = marshalCtrlCmdInto(cmd, buf) // buffer is exactly 32 bytes, cannot fail
	return buf
}

// marshalCtrlCmdInto marshals UblksrvCtrlCmd into buf without allocating.
func marshalCtrlCmdInto(cmd *UblksrvCtrlCmd, buf []byte) (int, error) {
	if len(buf) < 32 {
		return 0, ErrBufferTooSmall
	}

	binary.LittleEndian.PutUint32(buf[0:4], cmd.DevID)
	binary.LittleEndian.PutUint16(buf[4:6], cmd.QueueID)
	binary.LittleEndian.PutUint16(buf[6:8], cmd.Len)
	binary.LittleEndian.PutUint64(buf[8:16], cmd.Addr)
	binary.LittleEndian.PutUint64(buf[16:24], cmd.Data)
	binary.LittleEndian.PutUint16(buf[24:26], cmd.DevPathLen)
	binary.LittleEndian.PutUint16(buf[26:28], cmd.Pad)
	binary.LittleEndian.PutUint32(buf[28:32], cmd.Reserved)

	return 32, nil
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
	_, _ = marshalIOCmdInto(cmd, buf) // buffer is exactly 16 bytes, cannot fail
	return buf
}

// marshalIOCmdInto marshals UblksrvIOCmd into buf without allocating.
func marshalIOCmdInto(cmd *UblksrvIOCmd, buf []byte) (int, error) {
	if len(buf) < 16 {
		return 0, ErrBufferTooSmall
	}

	binary.LittleEndian.PutUint16(buf[0:2], cmd.QID)
	binary.LittleEndian.PutUint16(buf[2:4], cmd.Tag)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(cmd.Result))
	binary.LittleEndian.PutUint64(buf[8:16], cmd.Addr)

	return 16, nil
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

// Linux keeps parameter blocks at fixed offsets, even if preceding types are
// absent. Send the prefix through the last selected block, without C tail padding.
const (
	paramsHeaderSize    = 8
	paramsBasicOffset   = 8
	paramsDiscardOffset = 40
	paramsDevtOffset    = 60
	paramsZonedOffset   = 76
	paramsBasicEnd      = 40
	paramsDiscardEnd    = 60
	paramsDevtEnd       = 76
	paramsZonedEnd      = 108
)

// paramsSize returns the prefix length needed for all selected known blocks.
func paramsSize(params *UblkParams) int {
	if params.HasZoned() {
		return paramsZonedEnd
	}
	if params.HasDevt() {
		return paramsDevtEnd
	}
	if params.HasDiscard() {
		return paramsDiscardEnd
	}
	if params.HasBasic() {
		return paramsBasicEnd
	}
	return paramsHeaderSize
}

// marshalParams handles the complex UblkParams structure
func marshalParams(params *UblkParams) []byte {
	buf := make([]byte, paramsSize(params))
	_, _ = marshalParamsInto(params, buf) // buffer is exactly the wire size, cannot fail
	return buf
}

// marshalParamsInto marshals UblkParams into buf without allocating.
func marshalParamsInto(params *UblkParams, buf []byte) (int, error) {
	size := paramsSize(params)
	if len(buf) < size {
		return 0, ErrBufferTooSmall
	}

	// Clear holes in reused buffers, while leaving bytes after the prefix alone.
	clear(buf[:size])
	binary.LittleEndian.PutUint32(buf[0:4], uint32(size))
	binary.LittleEndian.PutUint32(buf[4:8], params.Types)

	// Copy each selected block to its fixed kernel offset. These individual
	// Go block layouts have no internal padding on supported architectures.
	if params.HasBasic() {
		rawCopy(buf[paramsBasicOffset:paramsBasicEnd], unsafe.Pointer(&params.Basic), 32)
	}
	if params.HasDiscard() {
		rawCopy(buf[paramsDiscardOffset:paramsDiscardEnd], unsafe.Pointer(&params.Discard), 20)
	}
	if params.HasDevt() {
		rawCopy(buf[paramsDevtOffset:paramsDevtEnd], unsafe.Pointer(&params.Devt), 16)
	}
	if params.HasZoned() {
		rawCopy(buf[paramsZonedOffset:paramsZonedEnd], unsafe.Pointer(&params.Zoned), 32)
	}

	return size, nil
}

// unmarshalParams handles the complex UblkParams structure
func unmarshalParams(data []byte, params *UblkParams) error {
	return decodeParams(data, params, false)
}

// UnmarshalParamsResponse decodes a GET_PARAMS buffer. Linux retains the length
// supplied to SET_PARAMS (or zero before SET_PARAMS), even when it adds DEVT to
// GET_PARAMS. Thus response Len is metadata, not a bound on populated fields.
// The caller must pass the buffer capacity supplied to GET_PARAMS; known blocks
// are bounded by that slice. Unmarshal remains strict for serialized records.
func UnmarshalParamsResponse(data []byte, params *UblkParams) error {
	return decodeParams(data, params, true)
}

func decodeParams(data []byte, params *UblkParams, kernelResponse bool) error {
	if len(data) < paramsHeaderSize {
		return ErrInsufficientData
	}

	// Validate before mutating the destination. Use a widening comparison so a
	// malicious length cannot wrap int on 32-bit builds. In strict serialized
	// records, trailing data outside Len cannot satisfy a selected block;
	// kernel response Len instead retains SET metadata as described above.
	decoded := UblkParams{
		Len:   binary.LittleEndian.Uint32(data[0:4]),
		Types: binary.LittleEndian.Uint32(data[4:8]),
	}
	required := paramsSize(&decoded)
	if len(data) < required {
		return ErrInsufficientData
	}
	if !kernelResponse && (uint64(decoded.Len) > uint64(len(data)) || decoded.Len < uint32(required)) {
		return ErrInsufficientData
	}
	if decoded.HasBasic() {
		_ = directUnmarshal(data[paramsBasicOffset:paramsBasicEnd], &decoded.Basic)
	}
	if decoded.HasDiscard() {
		_ = directUnmarshal(data[paramsDiscardOffset:paramsDiscardEnd], &decoded.Discard)
	}
	if decoded.HasDevt() {
		_ = directUnmarshal(data[paramsDevtOffset:paramsDevtEnd], &decoded.Devt)
	}
	if decoded.HasZoned() {
		_ = directUnmarshal(data[paramsZonedOffset:paramsZonedEnd], &decoded.Zoned)
	}
	*params = decoded
	return nil
}

// directMarshal performs direct memory copy for marshaling
func directMarshal(v interface{}) []byte {
	// Dereference the interface to get actual struct pointer
	ptr := reflect.ValueOf(v).UnsafePointer()
	size := int(reflect.TypeOf(v).Elem().Size())

	// Create a copy of the bytes from the actual struct
	buf := make([]byte, size)
	src := (*[1 << 20]byte)(ptr)
	copy(buf, src[:size])

	return buf
}

// rawCopy copies size raw bytes from src into dst without allocating.
func rawCopy(dst []byte, src unsafe.Pointer, size int) {
	from := (*[1 << 20]byte)(src)
	copy(dst, from[:size])
}

// directMarshalInto performs a direct memory copy of v into buf without allocating.
func directMarshalInto(v interface{}, buf []byte) (int, error) {
	ptr := reflect.ValueOf(v).UnsafePointer()
	size := int(reflect.TypeOf(v).Elem().Size())

	if len(buf) < size {
		return 0, ErrBufferTooSmall
	}
	rawCopy(buf, ptr, size)

	return size, nil
}

// directUnmarshal performs direct memory copy for unmarshaling
func directUnmarshal(data []byte, v interface{}) error {
	// Get the actual pointer and size from the interface (must be a pointer type)
	ptr := reflect.ValueOf(v).UnsafePointer()
	size := int(reflect.TypeOf(v).Elem().Size())

	if len(data) < size {
		return ErrInsufficientData
	}

	// Direct memory copy to the struct
	dst := (*[1 << 20]byte)(ptr)
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
	buf := make([]byte, 64)                  // Now exactly 64 bytes to match kernel 6.6+
	_, _ = marshalCtrlDevInfoInto(info, buf) // buffer is exactly 64 bytes, cannot fail
	return buf
}

// marshalCtrlDevInfoInto marshals UblksrvCtrlDevInfo into buf without allocating.
func marshalCtrlDevInfoInto(info *UblksrvCtrlDevInfo, buf []byte) (int, error) {
	if len(buf) < 64 {
		return 0, ErrBufferTooSmall
	}

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

	return 64, nil
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

	// OwnerUID/GID are at bytes 40-48 in the 64-byte struct
	if len(data) >= 48 {
		info.OwnerUID = binary.LittleEndian.Uint32(data[40:44])
		info.OwnerGID = binary.LittleEndian.Uint32(data[44:48])
	}

	// Reserved fields at bytes 48-64
	if len(data) >= 64 {
		info.Reserved1 = binary.LittleEndian.Uint64(data[48:56])
		info.Reserved2 = binary.LittleEndian.Uint64(data[56:64])
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
	ErrBufferTooSmall   MarshalError = "buffer too small for marshaling into"
)
