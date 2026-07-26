package ublk

import (
	"fmt"

	"github.com/ehrlich-b/go-ublk/internal/ctrl"
)

// maxScanDeviceID bounds the ID space ListDevices probes. The kernel offers no
// "list devices" control command — the only way to enumerate is to ask each ID
// for its info — and ublk_drv's ublks_max defaults to 64.
const maxScanDeviceID = 64

// ListDevices returns the IDs of every ublk device currently registered with
// the kernel, including devices with no live server behind them (for example
// one left over from a daemon that was killed ungracefully). Requires root or
// CAP_SYS_ADMIN.
func ListDevices() ([]uint32, error) {
	c, err := ctrl.NewController()
	if err != nil {
		return nil, fmt.Errorf("open control device: %w", err)
	}
	defer c.Close()

	var ids []uint32
	for id := uint32(0); id < maxScanDeviceID; id++ {
		if _, err := c.GetDeviceInfo(id); err == nil {
			ids = append(ids, id)
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
	c, err := ctrl.NewController()
	if err != nil {
		return fmt.Errorf("open control device: %w", err)
	}
	defer c.Close()

	if _, err := c.GetDeviceInfo(id); err != nil {
		return fmt.Errorf("device %d: %w", id, err)
	}
	_ = c.StopDevice(id)
	if err := c.DeleteDevice(id); err != nil {
		return fmt.Errorf("device %d: %w", id, err)
	}
	return nil
}
