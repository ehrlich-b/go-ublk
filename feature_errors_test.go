package ublk

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/ctrl"
)

// These tests must remain sequential: only they replace the private controller
// factory, and each test restores it before another test can use it.
func replaceControllerFactory(t *testing.T, factory func() (*ctrl.Controller, error)) {
	t.Helper()
	original := createController
	createController = factory
	t.Cleanup(func() { createController = original })
}

func TestRejectInvalidModesBeforeControllerCreation(t *testing.T) {
	base := DefaultParams(NewMockBackend(1 << 20))
	if err := validateParams(&base); err != nil {
		t.Fatalf("default address-buffer mode rejected: %v", err)
	}
	userCopy := base
	userCopy.EnableUserCopy = true
	if err := validateParams(&userCopy); err != nil {
		t.Fatalf("user copy is implemented but was rejected: %v", err)
	}

	createCalls := 0
	replaceControllerFactory(t, func() (*ctrl.Controller, error) {
		createCalls++
		return nil, errors.New("controller must not be opened for an invalid mode")
	})
	handler := HandlerFunc(func(r *Request) { r.Complete(nil) })
	for _, c := range []struct {
		name, want string
		set        func(*DeviceParams)
	}{
		{"zero copy without a ZeroCopyBackend", "ZeroCopyBackend", func(p *DeviceParams) { p.EnableZeroCopy = true }},
		{"zoned with a Backend", "Handler", func(p *DeviceParams) { p.EnableZoned = true; p.Zoned.ZoneSize = 1 << 16 }},
		{"zoned with a bad zone size", "ZoneSize", func(p *DeviceParams) {
			p.Backend, p.Handler, p.Size = nil, handler, 1<<20
			p.EnableZoned, p.Zoned.ZoneSize = true, 3000
		}},
		{"zoned size not a multiple of the zone size", "multiple of Zoned.ZoneSize", func(p *DeviceParams) {
			p.Backend, p.Handler, p.Size = nil, handler, 1<<20+1<<16
			p.EnableZoned, p.Zoned.ZoneSize = true, 1<<17
		}},
	} {
		params := base
		c.set(&params)
		for _, test := range []struct {
			name string
			call func() (*Device, error)
		}{
			{"Create", func() (*Device, error) { return Create(params, nil) }},
			{"CreateAndServe", func() (*Device, error) { return CreateAndServe(context.Background(), params, nil) }},
		} {
			t.Run(c.name+"/"+test.name, func(t *testing.T) {
				device, err := test.call()
				if device != nil || err == nil || !strings.Contains(err.Error(), c.want) {
					t.Fatalf("device=%v error=%v, want an error mentioning %q", device, err, c.want)
				}
			})
		}
	}
	if createCalls != 0 {
		t.Fatalf("opened controller %d times for invalid modes", createCalls)
	}
}

func TestPublicOperationsPreserveControllerOpenErrors(t *testing.T) {
	for _, errno := range []syscall.Errno{syscall.EACCES, syscall.EOPNOTSUPP, syscall.ENOMEM} {
		t.Run(errno.Error(), func(t *testing.T) {
			cause := fmt.Errorf("synthetic control open: %w", errno)
			replaceControllerFactory(t, func() (*ctrl.Controller, error) { return nil, cause })
			params := DefaultParams(NewMockBackend(1 << 20))
			for _, test := range []struct {
				name string
				call func() error
			}{
				{"Create", func() error { _, err := Create(params, nil); return err }},
				{"CreateAndServe", func() error { _, err := CreateAndServe(context.Background(), params, nil); return err }},
				{"Stop", func() error { return (&Device{state: DeviceStateRunning, done: make(chan struct{})}).Stop() }},
				{"Close", func() error { return (&Device{state: DeviceStateCreated, done: make(chan struct{})}).Close() }},
				{"ListDevices", func() error { _, err := ListDevices(); return err }},
				{"DeleteDevice", func() error { return DeleteDevice(42) }},
			} {
				t.Run(test.name, func(t *testing.T) {
					err := test.call()
					if !errors.Is(err, errno) || !errors.Is(err, cause) {
						t.Fatalf("lost wrapped error: %v", err)
					}
					var got syscall.Errno
					if !errors.As(err, &got) || got != errno {
						t.Fatalf("errors.As errno = %v, error = %v", got, err)
					}
				})
			}
		})
	}
}
