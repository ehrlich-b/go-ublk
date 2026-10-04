package ublk

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"syscall"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

// maxScanDeviceID bounds the ID space ListDevices probes when sysfs is not
// available. The kernel offers no "list devices" control command.
const maxScanDeviceID = 64

// ListDevices returns the IDs of every ublk device currently registered with
// the kernel, including devices with no live server behind them (for example
// one left over from a daemon that was killed ungracefully). Requires root or
// CAP_SYS_ADMIN. A query failure other than an absent device returns an error;
// callers must not treat an incomplete scan as an empty device list.
func ListDevices() ([]uint32, error) {
	c, err := createController()
	if err != nil {
		return nil, fmt.Errorf("open control device: %w", err)
	}
	defer c.Close()
	getInfo := func(id uint32) (*uapi.UblksrvCtrlDevInfo, error) { return c.GetDevInfo(context.Background(), id) }
	// Every registered device has a ublkcN entry in the ublk-char class, so
	// sysfs lists them all, whatever their IDs; fall back to probing IDs
	// where sysfs is not mounted.
	if ids, ok := sysfsDevices(); ok {
		var live []uint32
		for _, id := range ids {
			if _, err := getInfo(id); err == nil {
				live = append(live, id)
			} else if !errors.Is(err, syscall.ENODEV) {
				return nil, fmt.Errorf("query device %d: %w", id, err)
			}
		}
		return live, nil
	}
	return scanDevices(getInfo)
}

// sysfsDevices lists device IDs from /sys/class/ublk-char.
func sysfsDevices() ([]uint32, bool) {
	ents, err := os.ReadDir("/sys/class/ublk-char")
	if err != nil {
		return nil, false
	}
	var ids []uint32
	for _, e := range ents {
		var id uint32
		if _, err := fmt.Sscanf(e.Name(), "ublkc%d", &id); err == nil {
			ids = append(ids, id)
		}
	}
	slices.Sort(ids)
	return ids, true
}

func scanDevices(getInfo func(uint32) (*uapi.UblksrvCtrlDevInfo, error)) ([]uint32, error) {
	var ids []uint32
	for id := uint32(0); id < maxScanDeviceID; id++ {
		_, err := getInfo(id)
		switch {
		case err == nil:
			ids = append(ids, id)
		case errors.Is(err, syscall.ENODEV):
			// The control protocol returns ENODEV for an unregistered ID.
		default:
			return nil, fmt.Errorf("query device %d: %w", id, err)
		}
	}
	return ids, nil
}

// DeleteDevice stops and deletes the ublk device with the given ID, releasing
// the kernel resources it holds. This is the recovery path for a device leaked
// by a server process that never got to run Device.Close — such a device stays
// registered, cannot serve I/O, and pins the ublk_drv module. A device this
// process still owns should be torn down with Device.Close instead, which also
// drains in-flight I/O.
//
// The STOP step is best effort: a device whose server is already gone is
// quiesced, and the error STOP_DEV reports for it is not interesting here.
func DeleteDevice(id uint32) error {
	c, err := createController()
	if err != nil {
		return fmt.Errorf("open control device: %w", err)
	}
	defer c.Close()

	if _, err := c.GetDevInfo(context.Background(), id); err != nil {
		return fmt.Errorf("device %d: %w", id, err)
	}
	_ = c.StopDev(context.Background(), id)
	if err := c.DelDev(context.Background(), id); err != nil {
		return fmt.Errorf("device %d: %w", id, err)
	}
	return nil
}

// DeleteDeviceAsync is DeleteDevice without waiting for the device's last
// reference to go away (DEL_DEV_ASYNC, kernel 6.11+): the ID is freed once
// every opener has closed it.
func DeleteDeviceAsync(id uint32) error {
	c, err := createController()
	if err != nil {
		return fmt.Errorf("open control device: %w", err)
	}
	defer c.Close()
	if _, err := c.GetDevInfo(context.Background(), id); err != nil {
		return fmt.Errorf("device %d: %w", id, err)
	}
	_ = c.StopDev(context.Background(), id)
	if err := c.DelDevAsync(context.Background(), id); err != nil {
		return fmt.Errorf("device %d: %w", id, err)
	}
	return nil
}
