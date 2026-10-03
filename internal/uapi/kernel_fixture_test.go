package uapi

import (
	"bytes"
	_ "embed"
	"encoding/hex"
	"strings"
	"testing"
)

// Embedded so the test does not depend on running from the package directory,
// which the cross-compiled binaries vm-test-unit copies to the VM do not.
//
//go:embed testdata/linux-le-fixtures.txt
var kernelFixtures string

// Generated independently by scripts/uapi-fixtures.c against Linux headers.
// The same LE prefixes are expected from v6.0 (common records), v6.6, v6.8,
// and v7.0. C struct sizeof includes tail padding; these prefixes do not.
func TestKernelCByteFixtures(t *testing.T) {
	basic := fixtureParams(UBLK_PARAM_TYPE_BASIC)
	devt := fixtureParams(UBLK_PARAM_TYPE_BASIC | UBLK_PARAM_TYPE_DEVT)
	values := map[string]interface{}{
		"ctrl":  &UblksrvCtrlCmd{DevID: 0x12345678, QueueID: 0xabcd, Len: 0x1234, Addr: 0x1122334455667788, Data: 0x8877665544332211},
		"io":    &UblksrvIOCmd{QID: 0x0123, Tag: 0xfedc, Result: -5, Addr: 0x8877665544332211},
		"basic": &basic, "basic_devt": &devt,
	}
	for _, line := range strings.Fields(kernelFixtures) {
		name, encoded, ok := strings.Cut(line, "=")
		if !ok {
			t.Fatalf("bad fixture: %s", line)
		}
		want, err := hex.DecodeString(encoded)
		if err != nil {
			t.Fatal(err)
		}
		value, ok := values[name]
		if !ok {
			t.Fatalf("unknown fixture: %s", name)
		}
		if got := Marshal(value); !bytes.Equal(got, want) {
			t.Fatalf("%s C fixture mismatch: got %x want %x", name, got, want)
		}
		if err := Unmarshal(want, value); err != nil {
			t.Fatal(err)
		}
		assertMarshalIntoMatches(t, value, want)
		delete(values, name)
	}
	if len(values) != 0 {
		t.Fatalf("missing C fixtures: %v", values)
	}
}
