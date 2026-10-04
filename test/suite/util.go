//go:build linux

package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/unix"

	"github.com/ehrlich-b/go-ublk"
)

// Block-layer ioctls (include/uapi/linux/fs.h).
const (
	blkROGet     = 0x125e
	blkSSZGet    = 0x1268
	blkPBSZGet   = 0x127b
	blkDiscard   = 0x1277
	blkZeroOut   = 0x127f
	blkGetSize64 = 0x80081272
)

// newDevice creates and starts a device and registers its Close as a cleanup.
func newDevice(t *T, params ublk.DeviceParams) (*ublk.Device, error) {
	dev, err := ublk.CreateAndServe(context.Background(), params, nil)
	if err != nil {
		return nil, fmt.Errorf("CreateAndServe: %w", err)
	}
	t.Cleanup(func() {
		if err := dev.Close(); err != nil {
			t.Logf("cleanup: Close %s: %v", dev.Path, err)
		}
	})
	if err := waitForNode(dev.Path, 5*time.Second); err != nil {
		return nil, err
	}
	return dev, nil
}

func memParams(size int64) (ublk.DeviceParams, *memBackend) {
	b := newMemBackend(size)
	p := ublk.DefaultParams(b)
	p.NumQueues = 2
	p.QueueDepth = 64
	return p, b
}

func waitForNode(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if fi, err := os.Stat(path); err == nil && fi.Mode()&os.ModeDevice != 0 {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s did not appear within %s", path, timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func waitForGone(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for {
		if _, err := os.Stat(path); errors.Is(err, os.ErrNotExist) {
			return nil
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("%s still present after %s", path, timeout)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func ioctlInt(fd int, req uintptr) (int, error) {
	var v int32
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&v))); e != 0 {
		return 0, e
	}
	return int(v), nil
}

func blockSize64(fd int) (int64, error) {
	var v uint64
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), blkGetSize64, uintptr(unsafe.Pointer(&v))); e != 0 {
		return 0, e
	}
	return int64(v), nil
}

// blkRange issues BLKDISCARD or BLKZEROOUT over [off, off+length).
func blkRange(fd int, req uintptr, off, length int64) error {
	r := [2]uint64{uint64(off), uint64(length)}
	if _, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), req, uintptr(unsafe.Pointer(&r[0]))); e != 0 {
		return e
	}
	return nil
}

// sysfsQueue reads /sys/block/<dev>/queue/<attr>.
func sysfsQueue(devPath, attr string) (string, error) {
	name := strings.TrimPrefix(devPath, "/dev/")
	b, err := os.ReadFile("/sys/block/" + name + "/queue/" + attr)
	return strings.TrimSpace(string(b)), err
}

func sysfsInt(devPath, attr string) (int64, error) {
	s, err := sysfsQueue(devPath, attr)
	if err != nil {
		return 0, err
	}
	return strconv.ParseInt(s, 10, 64)
}

// alignedBuf returns a 4096-aligned buffer, as O_DIRECT requires.
func alignedBuf(n int) []byte {
	raw := make([]byte, n+4096)
	off := int(uintptr(unsafe.Pointer(&raw[0])) & 4095)
	if off != 0 {
		off = 4096 - off
	}
	return raw[off : off+n : off+n]
}

func pwriteFull(fd int, p []byte, off int64) error {
	for done := 0; done < len(p); {
		n, err := unix.Pwrite(fd, p[done:], off+int64(done))
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("pwrite at %d: wrote 0 bytes", off+int64(done))
		}
		done += n
	}
	return nil
}

func preadFull(fd int, p []byte, off int64) error {
	for done := 0; done < len(p); {
		n, err := unix.Pread(fd, p[done:], off+int64(done))
		if err != nil {
			return err
		}
		if n == 0 {
			return fmt.Errorf("pread at %d: unexpected EOF", off+int64(done))
		}
		done += n
	}
	return nil
}

// rng is a small deterministic xorshift generator, so a failing seed replays.
type rng struct{ s uint64 }

func newRNG(seed uint64) *rng {
	if seed == 0 {
		seed = 0x9e3779b97f4a7c15
	}
	return &rng{s: seed}
}

func (r *rng) next() uint64 {
	r.s ^= r.s << 13
	r.s ^= r.s >> 7
	r.s ^= r.s << 17
	return r.s
}

func (r *rng) intn(n int) int { return int(r.next() % uint64(n)) }

func (r *rng) fill(p []byte) {
	for i := 0; i+8 <= len(p); i += 8 {
		v := r.next()
		for j := 0; j < 8; j++ {
			p[i+j] = byte(v >> (8 * j))
		}
	}
}

func firstDiff(a, b []byte) int {
	for i := range a {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}

func openFDs() int {
	ents, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(ents)
}

func mapCount() int {
	b, err := os.ReadFile("/proc/self/maps")
	if err != nil {
		return -1
	}
	return bytes.Count(b, []byte{'\n'})
}

// kmsgMark returns the current kernel log length, so kernelProblems can look
// only at what a test produced.
func kmsgMark() int {
	out, err := exec.Command("dmesg").Output()
	if err != nil {
		return -1
	}
	return bytes.Count(out, []byte{'\n'})
}

// kernelProblems returns kernel log lines since mark that indicate a kernel
// bug (oops, warning, hung task, use-after-free), or "" if none.
func kernelProblems(mark int) string {
	if mark < 0 {
		return ""
	}
	out, err := exec.Command("dmesg").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(string(out), "\n")
	if mark > len(lines) {
		mark = 0
	}
	var bad []string
	for _, l := range lines[mark:] {
		for _, pat := range []string{"BUG:", "WARNING:", "Oops", "general protection", "blocked for more than",
			"KASAN", "use-after-free", "Call Trace"} {
			if strings.Contains(l, pat) {
				bad = append(bad, strings.TrimSpace(l))
				break
			}
		}
	}
	if len(bad) > 5 {
		bad = append(bad[:5], fmt.Sprintf("... %d more", len(bad)-5))
	}
	return strings.Join(bad, " | ")
}

// The suite re-executes itself as a separate server process for tests that
// kill a server (a device must outlive its creator's death, which can't be
// tested in-process).
const serverSubcommand = "__server"

func serverMain(args []string) {
	fs := flag.NewFlagSet(serverSubcommand, flag.ExitOnError)
	size := fs.Int64("size", 64<<20, "device size")
	file := fs.String("file", "", "back the device with this file instead of RAM")
	recovery := fs.Bool("recovery", false, "create with RecoveryReissue")
	detach := fs.Bool("detach-on-usr1", false, "on SIGUSR1, Detach and exit 0")
	unpriv := fs.Bool("unprivileged", false, "create an unprivileged device (run as a non-root user)")
	batch := fs.Bool("batch", false, "serve with BatchIO")
	integ := fs.Bool("integrity", false, "T10-DIF integrity, metadata kept in FILE.meta (needs -file)")
	_ = fs.Parse(args)

	var params ublk.DeviceParams
	if *file != "" {
		b, err := openRecoveryBackend(*file, *size, *integ)
		if err != nil {
			fmt.Printf("ERROR %v\n", err)
			os.Exit(1)
		}
		params = ublk.DefaultParams(b)
		params.NumQueues, params.QueueDepth = 2, 32
	} else {
		params, _ = memParams(*size)
	}
	if *integ {
		params.Integrity = &ublk.IntegrityParams{MetadataSize: integMetaSize, IntervalSize: integInterval,
			Checksum: ublk.IntegrityCsumCRC16, RefTag: true}
	}
	if *batch {
		params.BatchIO = true
	}
	if *recovery {
		params.Recovery = ublk.RecoveryReissue
	}
	if *unpriv {
		params.EnableUnprivileged = true
	}
	dev, err := ublk.CreateAndServe(context.Background(), params, nil)
	if err != nil {
		fmt.Printf("ERROR %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("READY %d\n", dev.ID)
	if *detach {
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGUSR1)
		<-ch
		if err := dev.Detach(); err != nil {
			fmt.Fprintf(os.Stderr, "detach: %v\n", err)
			os.Exit(3)
		}
		os.Exit(0)
	}
	select {} // serve until killed
}

// fileBackend is a file-backed backend, so a device's contents outlive the
// server process that wrote them (recovery tests).
type fileBackend struct {
	f    *os.File
	size int64
}

func openFileBackend(path string, size int64) (*fileBackend, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, err
	}
	if err := f.Truncate(size); err != nil {
		f.Close()
		return nil, err
	}
	return &fileBackend{f: f, size: size}, nil
}

func (b *fileBackend) ReadAt(p []byte, off int64) (int, error)  { return b.f.ReadAt(p, off) }
func (b *fileBackend) WriteAt(p []byte, off int64) (int, error) { return b.f.WriteAt(p, off) }
func (b *fileBackend) Size() int64                              { return b.size }
func (b *fileBackend) Close() error                             { return b.f.Close() }
func (b *fileBackend) Flush() error                             { return b.f.Sync() }

// The integrity format of the recovery tests: T10-DIF, 8 bytes per 512.
const integInterval, integMetaSize = 512, 8

// fileIntegBackend is a fileBackend that also keeps integrity metadata, in
// PATH.meta, so the metadata outlives the server like the data does.
// Intervals never written read as 0xff, the T10 escape.
type fileIntegBackend struct {
	*fileBackend
	meta *os.File
}

func openFileIntegBackend(path string, size int64) (*fileIntegBackend, error) {
	b, err := openFileBackend(path, size)
	if err != nil {
		return nil, err
	}
	m, err := os.OpenFile(path+".meta", os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		b.Close()
		return nil, err
	}
	msize := size / integInterval * integMetaSize
	if st, err := m.Stat(); err == nil && st.Size() != msize {
		ff := bytes.Repeat([]byte{0xff}, 1<<20)
		for off := int64(0); off < msize; off += int64(len(ff)) {
			if _, err := m.WriteAt(ff[:min(int64(len(ff)), msize-off)], off); err != nil {
				m.Close()
				b.Close()
				return nil, err
			}
		}
	}
	return &fileIntegBackend{fileBackend: b, meta: m}, nil
}

func (b *fileIntegBackend) ReadIntegrity(meta []byte, off int64) error {
	_, err := b.meta.ReadAt(meta, off/integInterval*integMetaSize)
	return err
}

func (b *fileIntegBackend) WriteIntegrity(meta []byte, off int64) error {
	_, err := b.meta.WriteAt(meta, off/integInterval*integMetaSize)
	return err
}

func (b *fileIntegBackend) Close() error {
	b.meta.Close()
	return b.fileBackend.Close()
}

// openRecoveryBackend opens the file backend a recovery test's server uses.
func openRecoveryBackend(path string, size int64, integrity bool) (ublk.Backend, error) {
	if integrity {
		return openFileIntegBackend(path, size)
	}
	return openFileBackend(path, size)
}

// startServer launches a server subprocess and returns it with its device ID.
func startServer(t *T, size int64, extra ...string) (*exec.Cmd, uint32, error) {
	return startServerAs(t, nil, size, extra...)
}

// startServerAs is startServer with the subprocess's credentials (nil: ours).
func startServerAs(t *T, cred *syscall.Credential, size int64, extra ...string) (*exec.Cmd, uint32, error) {
	args := append([]string{serverSubcommand, "-size", strconv.FormatInt(size, 10)}, extra...)
	cmd := exec.Command("/proc/self/exe", args...)
	if cred != nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{Credential: cred}
	}
	cmd.Stderr = os.Stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, err
	}
	if err := cmd.Start(); err != nil {
		return nil, 0, err
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	line := make(chan string, 1)
	go func() {
		s := bufio.NewScanner(out)
		if s.Scan() {
			line <- s.Text()
		}
		close(line)
	}()
	select {
	case l, ok := <-line:
		if !ok {
			return nil, 0, fmt.Errorf("server exited without reporting")
		}
		var id uint32
		if _, err := fmt.Sscanf(l, "READY %d", &id); err != nil {
			return nil, 0, fmt.Errorf("server: %s", l)
		}
		return cmd, id, nil
	case <-time.After(30 * time.Second):
		return nil, 0, fmt.Errorf("server did not report readiness")
	}
}

func hasCommand(name string) bool {
	_, err := exec.LookPath(name)
	return err == nil
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s %s: %v: %s", name, strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}
