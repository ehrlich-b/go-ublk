// Command ublk-loop exports a regular file as a block device, the way losetup
// does, using only go-ublk's public API.
//
// It is the counterpart to ublk-mem: where that example shows the smallest
// possible backend, this one shows the parts a real backend has to get right —
// concurrent positional I/O, sparse allocation, discard that actually returns
// space, and an honest answer to "is a completed write durable yet?"
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/ehrlich-b/go-ublk"
)

func main() {
	var (
		path       = flag.String("file", "", "Backing file to export (required; created if missing)")
		sizeStr    = flag.String("size", "", "Device size (e.g. 1G); default is the file's current size")
		numQueues  = flag.Int("queues", 0, "Number of I/O queues (0 = auto-detect based on CPU count)")
		queueDepth = flag.Int("depth", 64, "Queue depth (number of concurrent I/Os per queue)")
		syncWrites = flag.Bool("sync", false, "Open the file O_DSYNC and advertise a write-through device")
		readOnly   = flag.Bool("read-only", false, "Export the file read-only")
		verbose    = flag.Bool("v", false, "Verbose output")
		delSpec    = flag.String("del", "", "Delete stuck device(s) and exit: a device ID (e.g. 3) or 'all'")
		devID      = flag.Int("id", -1, "Device ID to request, giving a stable /dev/ublkbN (-1 = first free)")
		zeroCopy   = flag.Bool("zero-copy", false, "Serve requests zero-copy: the kernel moves data between requests and the file with io_uring (kernel 6.15+)")
		recovery   = flag.Bool("recovery", false, "Keep the block device across server restarts: requeue in-flight I/O if the server dies, take an existing device -id over on start, and Detach on SIGUSR2 for upgrades")
	)
	flag.Parse()

	// Cleanup mode: reap devices left registered by an ungracefully-killed
	// server, which stay in the kernel and pin the ublk_drv module.
	if *delSpec != "" {
		if err := reapDevices(*delSpec); err != nil {
			log.Fatalf("reap failed: %v", err)
		}
		return
	}

	if *path == "" {
		fmt.Fprintln(os.Stderr, "-file is required")
		flag.Usage()
		os.Exit(2)
	}

	var size int64
	if *sizeStr != "" {
		var err error
		if size, err = parseSize(*sizeStr); err != nil {
			log.Fatalf("invalid size %q: %v", *sizeStr, err)
		}
	}

	backend, err := openLoop(*path, size, *readOnly, *syncWrites)
	if err != nil {
		log.Fatalf("open %s: %v", *path, err)
	}
	// The device does not own the backend: Device.Close() tears down the kernel
	// device and leaves the backend alone, so closing it is the caller's job.
	defer backend.Close()

	params := ublk.DefaultParams(backend)
	params.QueueDepth = *queueDepth
	params.NumQueues = *numQueues // 0 = auto-detect based on CPU count
	params.LogicalBlockSize = loopBlockSize
	// Required on kernel 6.11+: sets UBLK_F_CMD_IOCTL_ENCODE at ADD_DEV.
	params.EnableIoctlEncode = true
	params.ReadOnly = *readOnly
	params.DeviceID = int32(*devID)
	params.EnableZeroCopy = *zeroCopy

	// The durability contract, stated once, in the one place that knows the
	// answer. Buffered writes to a file are in the page cache when WriteAt
	// returns — NOT on the disk — so the device must advertise a volatile write
	// cache; that is what makes the kernel send us the FLUSH we turn into
	// fsync. Under -sync the file is O_DSYNC, every write is already durable
	// when it completes, and advertising no cache is then the truthful answer
	// (and saves the flush round-trips).
	params.VolatileCache = !*syncWrites

	options := &ublk.Options{Logger: stderrLogger{verbose: *verbose}}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var device *ublk.Device
	if *recovery {
		params.Recovery = ublk.RecoveryReissue
		device, err = recoverOrCreate(ctx, *devID, params, options)
	} else {
		device, err = ublk.CreateAndServe(ctx, params, options)
	}
	if err != nil {
		log.Fatalf("create device: %v", err)
	}

	cache := "volatile (writes need a flush to be durable; the kernel will send them)"
	if *syncWrites {
		cache = "write-through (O_DSYNC: every write is durable before it completes)"
	}
	fmt.Printf("Device created: %s\n", device.Path)
	fmt.Printf("Backing file:   %s\n", *path)
	fmt.Printf("Size:           %s (%d bytes)\n", formatSize(device.Size()), device.Size())
	fmt.Printf("Queues:         %d, Depth: %d\n", device.NumQueues(), params.QueueDepth)
	fmt.Printf("Write cache:    %s\n", cache)
	if *readOnly {
		fmt.Printf("Mode:           read-only (writes fail with EPERM at the block layer)\n")
	}
	fmt.Printf("\n  sudo mkfs.ext4 %s && sudo mount %s /mnt\n", device.Path, device.Path)
	fmt.Printf("\nPress Ctrl+C to stop...\n")
	sdNotify("READY=1")

	// Handle every signal that can arrive during a shutdown, not just the first
	// one. logind sends SIGTERM and then SIGHUP to a session's processes, and an
	// unhandled SIGHUP kills the daemon while Close is still running STOP_DEV,
	// stranding the I/O only this process can complete (TODO Critical Bug #15).
	// A write to a vanished terminal or log pipe must not kill it either.
	signal.Ignore(syscall.SIGPIPE)
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGUSR2)
	var sig os.Signal
	select {
	case sig = <-sigCh:
	case <-device.Done():
		log.Printf("device stopped serving: %v", device.Err())
	}
	sdNotify("STOPPING=1")

	// SIGUSR2 with -recovery hands the device to the next server instead of
	// deleting it: the block device, and anything mounted on it, stays.
	if sig == syscall.SIGUSR2 && *recovery {
		if err := device.Detach(); err != nil {
			log.Fatalf("detach: %v", err)
		}
		fmt.Printf("detached %s; start the new server to take it over\n", device.Path)
		return
	}
	fmt.Println("\nreceived shutdown signal")

	// Close runs STOP_DEV while the queues are still serving, so the kernel
	// can drain in-flight I/O through them, then deletes the device.
	done := make(chan struct{})
	go func() {
		if err := device.Close(); err != nil {
			log.Printf("error stopping device: %v", err)
		}
		close(done)
	}()

	// Close() is bounded, but aborting a lot of in-flight I/O legitimately
	// takes a moment. This backstop only guards against a genuine wedge; if it
	// fires, the device is still registered — reap it with -del=all.
	select {
	case <-done:
		fmt.Println("device stopped")
	case <-time.After(15 * time.Second):
		fmt.Println("cleanup timeout, forcing exit (device may be left registered; reap with -del=all)")
	}
}

// stderrLogger implements ublk.Logger, the public logging hook: two methods over
// whatever logger the caller already has.
type stderrLogger struct{ verbose bool }

func (l stderrLogger) Printf(format string, args ...interface{}) {
	log.Printf(format, args...)
}

func (l stderrLogger) Debugf(format string, args ...interface{}) {
	if l.verbose {
		log.Printf("debug: "+format, args...)
	}
}

// reapDevices deletes ublk devices left registered in the kernel by a server
// that was killed ungracefully. spec is a device ID or "all".
func reapDevices(spec string) error {
	if spec == "all" {
		ids, err := ublk.ListDevices()
		if err != nil {
			return err
		}
		for _, id := range ids {
			if err := ublk.DeleteDevice(id); err != nil {
				fmt.Printf("device %d: %v\n", id, err)
			} else {
				fmt.Printf("device %d: deleted\n", id)
			}
		}
		return nil
	}

	id, err := strconv.Atoi(spec)
	if err != nil || id < 0 {
		return fmt.Errorf("invalid -del value %q (want a device ID or 'all')", spec)
	}
	if err := ublk.DeleteDevice(uint32(id)); err != nil {
		return err
	}
	fmt.Printf("device %d: deleted\n", id)
	return nil
}

// parseSize parses a size string like "64M", "1G", "512K".
func parseSize(s string) (int64, error) {
	s = strings.ToUpper(s)

	var multiplier int64 = 1
	var numStr string

	switch {
	case strings.HasSuffix(s, "K"):
		multiplier = 1024
		numStr = strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "M"):
		multiplier = 1024 * 1024
		numStr = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "G"):
		multiplier = 1024 * 1024 * 1024
		numStr = strings.TrimSuffix(s, "G")
	default:
		numStr = s
	}

	num, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		return 0, err
	}
	return num * multiplier, nil
}

// formatSize formats a byte count as a human-readable string.
func formatSize(bytes int64) string {
	const unit = 1024
	if bytes < unit {
		return fmt.Sprintf("%d B", bytes)
	}

	div, exp := int64(unit), 0
	for n := bytes / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}

	units := []string{"K", "M", "G", "T"}
	return fmt.Sprintf("%.1f %sB", float64(bytes)/float64(div), units[exp])
}

// sdNotify sends a state change to systemd when running as a Type=notify
// service, so a mount ordered After= this unit waits until /dev/ublkbN is live.
// It does nothing outside systemd.
func sdNotify(state string) {
	name := os.Getenv("NOTIFY_SOCKET")
	if name == "" {
		return
	}
	if name[0] == '@' {
		name = "\x00" + name[1:] // abstract socket
	}
	conn, err := net.DialUnix("unixgram", nil, &net.UnixAddr{Name: name, Net: "unixgram"})
	if err != nil {
		log.Printf("sd_notify %s: %v", state, err)
		return
	}
	defer conn.Close()
	if _, err := conn.Write([]byte(state)); err != nil {
		log.Printf("sd_notify %s: %v", state, err)
	}
}

// recoverOrCreate takes over device id if a previous server left it behind
// with recovery enabled (crashed, or detached for an upgrade), and otherwise
// creates it. A leftover device without recovery cannot be taken over, so it
// is deleted first.
func recoverOrCreate(ctx context.Context, id int, params ublk.DeviceParams, options *ublk.Options) (*ublk.Device, error) {
	if id < 0 {
		return ublk.CreateAndServe(ctx, params, options)
	}
	info, err := ublk.GetDeviceInfo(uint32(id))
	if err != nil {
		return ublk.CreateAndServe(ctx, params, options) // no such device
	}
	if info.Features.Has(ublk.FeatureUserRecovery) {
		log.Printf("taking over device %d (kernel state %s)", id, info.State)
		return ublk.Recover(ctx, uint32(id), params, options)
	}
	log.Printf("deleting leftover device %d, which has no recovery", id)
	if err := ublk.DeleteDevice(uint32(id)); err != nil {
		return nil, err
	}
	return ublk.CreateAndServe(ctx, params, options)
}
