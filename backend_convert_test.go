package ublk

import (
	"reflect"
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/ctrl"
)

// fieldsOf returns the set of exported field names of the given struct type.
func fieldsOf(typ reflect.Type) map[string]bool {
	names := make(map[string]bool)
	if typ.Kind() != reflect.Struct {
		panic("fieldsOf called on non-struct type " + typ.String())
	}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath == "" && f.IsExported() {
			names[f.Name] = true
		}
	}
	return names
}

// TestConvertFieldSetCompleteness guards the two struct SHAPES rather than the
// copy itself: every exported field of the public DeviceParams (except Backend)
// must also exist on the internal ctrl.DeviceParams, or addDevicePath would have
// nowhere to forward a caller's value. This is the test that would have failed
// the day a field was added to only one of the two structs.
//
// Backend is deliberately excluded: it is typed Backend on the public side and
// interfaces.Backend on the internal side — a legitimate type difference, not a
// naming gap. It is forwarded by ctrl.DefaultDeviceParams(params.Backend), not
// by the hand-copy in convertToCtrlParams.
func TestConvertFieldSetCompleteness(t *testing.T) {
	pubFields := fieldsOf(reflect.TypeOf(DeviceParams{}))
	ctrlFields := fieldsOf(reflect.TypeOf(ctrl.DeviceParams{}))

	var ctrlOnly []string
	for f := range ctrlFields {
		if !pubFields[f] {
			ctrlOnly = append(ctrlOnly, f)
		}
	}

	for f := range pubFields {
		if f == "Backend" {
			continue // typed Backend vs interfaces.Backend; forwarded via DefaultDeviceParams
		}
		if !ctrlFields[f] {
			t.Errorf("DeviceParams.%s has no counterpart on ctrl.DeviceParams: convertToCtrlParams silently drops it", f)
		}
	}

	if len(ctrlOnly) > 0 {
		t.Logf("ctrl.DeviceParams-only fields (informational): %v", ctrlOnly)
	}
}

// TestConvertAllFieldsForwarded drives every hand-copied field of
// convertToCtrlParams with a distinctive, non-zero, non-default value and
// asserts it lands on the matching ctrl.DeviceParams field. Each field is
// asserted by name, so a copy-paste line dropped, duplicated onto the wrong
// field, or a right-hand-side typo fails loudly.
func TestConvertAllFieldsForwarded(t *testing.T) {
	backend := NewMockBackend(1 << 20)
	params := DeviceParams{
		Backend:            backend,
		DeviceID:           9876,
		QueueDepth:         77,
		NumQueues:          13,
		LogicalBlockSize:   65536,
		MaxIOSize:          4194304,
		EnableZeroCopy:     true,
		EnableUnprivileged: true,
		EnableUserCopy:     true,
		EnableZoned:        true,
		EnableIoctlEncode:  true,
		ReadOnly:           true,
		Rotational:         true,
		VolatileCache:      true,
		EnableFUA:          true,
		DiscardAlignment:   9999,
		DiscardGranularity: 8888,
		MaxDiscardSectors:  777777,
		MaxDiscardSegments: 6,
		DeviceName:         "distinctive-name",
		CPUAffinity:        []int{3, 1, 4},
	}

	got := convertToCtrlParams(params)

	checkField(t, "DeviceID", got.DeviceID, params.DeviceID)
	checkField(t, "QueueDepth", got.QueueDepth, params.QueueDepth)
	checkField(t, "NumQueues", got.NumQueues, params.NumQueues)
	checkField(t, "LogicalBlockSize", got.LogicalBlockSize, params.LogicalBlockSize)
	checkField(t, "MaxIOSize", got.MaxIOSize, params.MaxIOSize)
	checkField(t, "EnableZeroCopy", got.EnableZeroCopy, params.EnableZeroCopy)
	checkField(t, "EnableUnprivileged", got.EnableUnprivileged, params.EnableUnprivileged)
	checkField(t, "EnableUserCopy", got.EnableUserCopy, params.EnableUserCopy)
	checkField(t, "EnableZoned", got.EnableZoned, params.EnableZoned)
	checkField(t, "EnableIoctlEncode", got.EnableIoctlEncode, params.EnableIoctlEncode)
	checkField(t, "ReadOnly", got.ReadOnly, params.ReadOnly)
	checkField(t, "Rotational", got.Rotational, params.Rotational)
	checkField(t, "VolatileCache", got.VolatileCache, params.VolatileCache)
	checkField(t, "EnableFUA", got.EnableFUA, params.EnableFUA)
	checkField(t, "DiscardAlignment", got.DiscardAlignment, params.DiscardAlignment)
	checkField(t, "DiscardGranularity", got.DiscardGranularity, params.DiscardGranularity)
	checkField(t, "MaxDiscardSectors", got.MaxDiscardSectors, params.MaxDiscardSectors)
	checkField(t, "MaxDiscardSegments", got.MaxDiscardSegments, params.MaxDiscardSegments)
	checkField(t, "DeviceName", got.DeviceName, params.DeviceName)
	if !reflect.DeepEqual(got.CPUAffinity, params.CPUAffinity) {
		t.Errorf("CPUAffinity: got %v, want %v", got.CPUAffinity, params.CPUAffinity)
	}

	// Backend is forwarded by ctrl.DefaultDeviceParams, not the hand-copy. The
	// two-sided types differ (Backend vs interfaces.Backend), so compare by
	// identity: the same concrete *MockBackend must come through untouched.
	if got.Backend != backend {
		t.Errorf("Backend: got %v, want %v (same instance)", got.Backend, backend)
	}
}

// TestConvertNilBackendRoundTrip confirms a nil Backend round-trips as nil:
// ctrl.DefaultDeviceParams(nil) just assigns nil to the field (see
// internal/ctrl/types.go) and must not panic.
func TestConvertNilBackendRoundTrip(t *testing.T) {
	var params DeviceParams // Backend is nil

	got := convertToCtrlParams(params)

	if got.Backend != nil {
		t.Errorf("nil Backend did not round-trip: got %v, want nil", got.Backend)
	}
}

// TestConvertZeroValueParams converts the zero DeviceParams{} and asserts every
// hand-copied field of the result is the Go zero value for its type. This is the
// mirror image of TestConvertAllFieldsForwarded: ctrl.DefaultDeviceParams seeds
// non-zero defaults (DeviceID -1, QueueDepth 128, LogicalBlockSize 512,
// MaxIOSize 1<<20, DiscardAlignment 4096, DiscardGranularity 4096,
// MaxDiscardSectors 0xffffffff, MaxDiscardSegments 1) before the hand-copy runs,
// so a MISSING copy line would let one of those stale defaults survive in place
// of the caller's explicit zero.
func TestConvertZeroValueParams(t *testing.T) {
	got := convertToCtrlParams(DeviceParams{}) // must not panic

	checkField(t, "DeviceID", got.DeviceID, int32(0))
	checkField(t, "QueueDepth", got.QueueDepth, 0)
	checkField(t, "NumQueues", got.NumQueues, 0)
	checkField(t, "LogicalBlockSize", got.LogicalBlockSize, 0)
	checkField(t, "MaxIOSize", got.MaxIOSize, 0)
	checkField(t, "EnableZeroCopy", got.EnableZeroCopy, false)
	checkField(t, "EnableUnprivileged", got.EnableUnprivileged, false)
	checkField(t, "EnableUserCopy", got.EnableUserCopy, false)
	checkField(t, "EnableZoned", got.EnableZoned, false)
	checkField(t, "EnableIoctlEncode", got.EnableIoctlEncode, false)
	checkField(t, "ReadOnly", got.ReadOnly, false)
	checkField(t, "Rotational", got.Rotational, false)
	checkField(t, "VolatileCache", got.VolatileCache, false)
	checkField(t, "EnableFUA", got.EnableFUA, false)
	checkField(t, "DiscardAlignment", got.DiscardAlignment, uint32(0))
	checkField(t, "DiscardGranularity", got.DiscardGranularity, uint32(0))
	checkField(t, "MaxDiscardSectors", got.MaxDiscardSectors, uint32(0))
	checkField(t, "MaxDiscardSegments", got.MaxDiscardSegments, uint16(0))
	checkField(t, "DeviceName", got.DeviceName, "")
	if got.CPUAffinity != nil {
		t.Errorf("CPUAffinity: got %v, want nil", got.CPUAffinity)
	}
	if got.Backend != nil {
		t.Errorf("Backend: got %v, want nil", got.Backend)
	}
}

// checkField is a typed named-field assertion for comparable field values.
func checkField[T comparable](t *testing.T, name string, got, want T) {
	t.Helper()
	if got != want {
		t.Errorf("%s: got %v, want %v", name, got, want)
	}
}
