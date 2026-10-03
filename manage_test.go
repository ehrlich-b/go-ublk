package ublk

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"syscall"
	"testing"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
)

func TestScanDevicesSkipsOnlyAbsentIDs(t *testing.T) {
	var queried []uint32
	ids, err := scanDevices(func(id uint32) (*uapi.UblksrvCtrlDevInfo, error) {
		queried = append(queried, id)
		if id == 2 || id == maxScanDeviceID-1 {
			return &uapi.UblksrvCtrlDevInfo{DevID: id}, nil
		}
		return nil, fmt.Errorf("GET_DEV_INFO: %w", syscall.ENODEV)
	})
	if err != nil || !reflect.DeepEqual(ids, []uint32{2, maxScanDeviceID - 1}) {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	if len(queried) != maxScanDeviceID || queried[0] != 0 || queried[len(queried)-1] != maxScanDeviceID-1 {
		t.Fatalf("scan omitted boundary IDs: %v", queried)
	}
}

func TestScanDevicesReportsQueryFailures(t *testing.T) {
	for _, cause := range []error{
		syscall.EACCES, syscall.EOPNOTSUPP, syscall.EIO, syscall.ENOENT,
		context.DeadlineExceeded,
	} {
		t.Run(cause.Error(), func(t *testing.T) {
			var queried []uint32
			ids, err := scanDevices(func(id uint32) (*uapi.UblksrvCtrlDevInfo, error) {
				queried = append(queried, id)
				switch id {
				case 0:
					return &uapi.UblksrvCtrlDevInfo{DevID: id}, nil
				case 2:
					return nil, fmt.Errorf("synthetic query: %w", cause)
				default:
					return nil, syscall.ENODEV
				}
			})
			if ids != nil || !errors.Is(err, cause) || !strings.Contains(err.Error(), "device 2") {
				t.Fatalf("ids=%v error=%v, want a contextual failure, not partial success", ids, err)
			}
			if !reflect.DeepEqual(queried, []uint32{0, 1, 2}) {
				t.Fatalf("continued past failed query: %v", queried)
			}
		})
	}
}
