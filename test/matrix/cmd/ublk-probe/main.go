//go:build linux

// Command ublk-probe reports what the running kernel's ublk driver offers, as
// one JSON object on stdout, for the kernel matrix: the UBLK_U_CMD_GET_FEATURES
// bitmask (raw and decoded), whether the kernel answered the ioctl-encoded or
// only the legacy opcode, and the loaded module's srcversion.
//
// The library has no GET_FEATURES call of its own, so this drives a control
// ring directly through internal/uring.
package main

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"syscall"
	"unsafe"

	"github.com/ehrlich-b/go-ublk/internal/uapi"
	"github.com/ehrlich-b/go-ublk/internal/uring"
)

const (
	controlPath    = "/dev/ublk-control"
	cmdGetFeatures = 0x13
	iocRead        = 2
)

// featureNames maps UBLK_F_* bit positions to names (ublk_cmd.h, v7.3-rc5).
var featureNames = []string{
	"SUPPORT_ZERO_COPY", "URING_CMD_COMP_IN_TASK", "NEED_GET_DATA", "USER_RECOVERY",
	"USER_RECOVERY_REISSUE", "UNPRIVILEGED_DEV", "CMD_IOCTL_ENCODE", "USER_COPY",
	"ZONED", "USER_RECOVERY_FAIL_IO", "UPDATE_SIZE", "AUTO_BUF_REG", "QUIESCE",
	"PER_IO_DAEMON", "BUF_REG_OFF_DAEMON", "BATCH_IO", "INTEGRITY", "SAFE_STOP_DEV",
	"NO_AUTO_PART_SCAN", "SHMEM_ZC", "IO_DESC_SIZE",
}

type report struct {
	Features   string   `json:"features"`
	Names      []string `json:"names,omitempty"`
	Opcode     string   `json:"opcode,omitempty"`
	Srcversion string   `json:"srcversion,omitempty"`
	Error      string   `json:"error,omitempty"`
}

func getFeatures(ring uring.Ring, op uint32) (uint64, error) {
	buf := make([]byte, uapi.UBLK_FEATURES_LEN)
	cmd := &uapi.UblksrvCtrlCmd{
		DevID:   ^uint32(0),
		QueueID: 0xFFFF,
		Len:     uapi.UBLK_FEATURES_LEN,
		Addr:    uint64(uintptr(unsafe.Pointer(&buf[0]))),
	}
	res, err := ring.SubmitCtrlCmd(op, cmd, 1)
	runtime.KeepAlive(buf)
	if err != nil {
		return 0, err
	}
	if v := res.Value(); v < 0 {
		return 0, syscall.Errno(-v)
	}
	return binary.LittleEndian.Uint64(buf), nil
}

func decode(f uint64) []string {
	var names []string
	for bit := 0; bit < 64; bit++ {
		if f&(1<<bit) == 0 {
			continue
		}
		if bit < len(featureNames) {
			names = append(names, featureNames[bit])
		} else {
			names = append(names, fmt.Sprintf("BIT%d", bit))
		}
	}
	return names
}

func main() {
	var r report
	if b, err := os.ReadFile("/sys/module/ublk_drv/srcversion"); err == nil {
		r.Srcversion = strings.TrimSpace(string(b))
	}
	r.Features = "unknown"
	defer func() {
		out, _ := json.Marshal(r)
		fmt.Println(string(out))
		if r.Error != "" {
			os.Exit(1)
		}
	}()

	fd, err := syscall.Open(controlPath, syscall.O_RDWR, 0)
	if err != nil {
		r.Error = fmt.Sprintf("open %s: %v", controlPath, err)
		return
	}
	defer syscall.Close(fd)
	ring, err := uring.NewMinimalRing(4, int32(fd))
	if err != nil {
		r.Error = fmt.Sprintf("io_uring setup: %v", err)
		return
	}
	defer ring.Close()

	f, err := getFeatures(ring, uapi.IoctlEncode(iocRead, 'u', cmdGetFeatures, 32))
	r.Opcode = "ioctl"
	if err != nil {
		// Kernels before ioctl encoding only know the bare command number.
		var legacyErr error
		f, legacyErr = getFeatures(ring, cmdGetFeatures)
		r.Opcode = "legacy"
		if legacyErr != nil {
			r.Opcode = ""
			r.Error = errors.Join(fmt.Errorf("ioctl-encoded: %w", err), fmt.Errorf("legacy: %w", legacyErr)).Error()
			return
		}
	}
	r.Features = fmt.Sprintf("0x%x", f)
	r.Names = decode(f)
}
