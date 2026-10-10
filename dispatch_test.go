package ublk

import "testing"

func TestDispatchModeValidation(t *testing.T) {
	for _, tc := range []struct {
		name  string
		mode  DispatchMode
		valid bool
	}{
		{"goroutine", DispatchGoroutine, true}, {"pool", DispatchPool, true},
		{"auto", DispatchAuto, true}, {"adaptive", DispatchAdaptive, true},
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

type declaredMockBackend struct {
	*MockBackend
}

func (declaredMockBackend) NonBlocking() bool { return true }

// Hiding NonBlocking tests that a custom observer is conservative by default.
type undeclaredMetricsObserver struct{ Observer }

func TestDeviceAutoDispatchObserverDeclaration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		observer Observer
		want     bool
	}{
		{"built-in-metrics", nil, true},
		{"no-op", NoOpObserver{}, true},
		{"custom-undeclared", undeclaredMetricsObserver{NoOpObserver{}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			params := DefaultParams(declaredMockBackend{NewMockBackend(1 << 20)})
			params.Dispatch = DispatchAuto
			device := newDevice(0, params, &Options{Observer: tc.observer}, 0)
			declaration := device.handler.(NonBlockingDeclarer)
			if got := declaration.NonBlocking(); got != tc.want {
				t.Fatalf("NonBlocking()=%v, want %v", got, tc.want)
			}
		})
	}
}
