package ublk

import "testing"

func TestDispatchModeValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  DispatchMode
		valid bool
	}{
		{"goroutine", DispatchGoroutine, true}, {"pool", DispatchPool, true},
		{"unknown", DispatchMode(255), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := DefaultParams(NewMockBackend(1 << 20))
			params.Dispatch = tc.mode
			if err := validateParams(&params); (err == nil) != tc.valid {
				t.Fatalf("dispatch=%d: error=%v, valid=%v", tc.mode, err, tc.valid)
			}
		})
	}
}
