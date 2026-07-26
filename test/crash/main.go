//go:build linux

// Command crash is a crash / power-fail consistency oracle for a ublk device.
// Unlike ./verify, which keeps its expected state in memory, this one must
// survive the death of its own process (and of the whole machine), so every
// block is *self-describing*: its content is fully derivable from its own
// position and a generation counter stored inside it. Nothing has to be
// remembered across the crash except a single durability witness.
//
// The device is split in half:
//
//   - Region A (low half) is written generation by generation. Each generation
//     is a full pass over the region followed by fdatasync — which, on a device
//     advertising a volatile write cache, is a real FLUSH down the ublk path —
//     and only then is the witness advanced. The witness counts *completed*
//     generations, so a crash during the very first pass constrains nothing and
//     a witness of N means every block in A must be at generation >= N-1. A
//     block older than that is an acknowledged, flushed write that the stack
//     lost.
//
//   - Region B (high half) is a continuous torrent of writes that are never
//     synced, so there is always genuinely unflushed I/O in flight when the
//     crash lands. Its blocks are allowed to hold any generation, or to be
//     untouched — but each one must be internally consistent. A block holding
//     the header of one generation and the body of another is a torn write; a
//     block holding another block's index is aliasing. Region B is striped so
//     exactly one writer ever owns a given block, which is what makes those two
//     verdicts sound rather than a race in the oracle.
//
// The three failure modes it is built to catch, in the words of the roadmap
// item: lost (A older than the witness), torn (body disagrees with header), and
// silently wrong (index disagrees with position).
//
// Imports only the standard library so it cross-compiles CGO-free and runs as a
// static binary on a bare test box.
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"
)

const (
	// blockSize is the unit of self-description. 4096 satisfies O_DIRECT
	// alignment on both 512e and 4Kn devices and is the page size, so it is
	// also the largest write the kernel could plausibly tear at.
	blockSize = 4096

	// magic marks a block this tool wrote, distinguishing "never written" from
	// "written and then corrupted".
	magic = 0x55424c4b43524153 // "UBLKCRAS"

	// headerLen is magic(8) + blockIdx(4) + gen(4); the rest of the block is a
	// deterministic stream derived from those.
	headerLen = 16

	blkGetSize64 = 0x80081272 // _IOR(0x12,114,size_t)
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	switch os.Args[1] {
	case "write":
		doWrite(os.Args[2:])
	case "check":
		doCheck(os.Args[2:])
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage:
  crash write -device /dev/ublkbN -witness PATH [-duration 10s] [-workers 4] [-direct]
  crash check -target /dev/ublkbN|IMAGE -witness PATH

  write  drives the device until killed (or -duration elapses), advancing a
         durability witness after every flushed generation of region A.
  check  reads the whole target back and reports lost / torn / aliased blocks
         relative to that witness. Exit 0 = consistent.`)
	os.Exit(2)
}

//
// write
//

func doWrite(argv []string) {
	fs := flag.NewFlagSet("write", flag.ExitOnError)
	devPath := fs.String("device", "", "block device to write, e.g. /dev/ublkb0 (required)")
	witness := fs.String("witness", "", "durability witness path, on a filesystem NOT backed by this device (required)")
	duration := fs.Duration("duration", 0, "stop after this long (0 = until killed)")
	workers := fs.Int("workers", 4, "region-B writer goroutines (each owns a disjoint stripe)")
	direct := fs.Bool("direct", true, "open with O_DIRECT so writes are not absorbed by the host page cache")
	_ = fs.Parse(argv)

	if *devPath == "" || *witness == "" {
		fs.Usage()
		os.Exit(2)
	}

	size := targetSize(*devPath)
	regionA, regionB, blocksA, blocksB := layout(size)
	if blocksA < 1 || blocksB < int64(*workers) {
		fatal("device too small (%d bytes) for %d workers", size, *workers)
	}

	// A fresh run starts from a clean witness: otherwise a check would demand
	// generations from a previous run's data.
	if err := putWitness(*witness, 0); err != nil {
		fatal("witness init: %v", err)
	}

	openFlags := syscall.O_RDWR
	if *direct {
		openFlags |= syscall.O_DIRECT
	}

	fmt.Printf("crash write: device=%s size=%d regionA=[0,%d) blocksA=%d regionB=[%d,%d) blocksB=%d workers=%d direct=%v\n",
		*devPath, size, regionA, blocksA, regionA, regionA+regionB, blocksB, *workers, *direct)

	var (
		stop     atomic.Bool
		wg       sync.WaitGroup
		writtenA atomic.Int64
		writtenB atomic.Int64
	)
	if *duration > 0 {
		go func() {
			time.Sleep(*duration)
			stop.Store(true)
		}()
	}

	// Region A: full pass, fdatasync, advance witness. Repeat.
	wg.Add(1)
	go func() {
		defer wg.Done()
		fd, err := syscall.Open(*devPath, openFlags, 0)
		if err != nil {
			fatal("open %s: %v", *devPath, err)
		}
		defer syscall.Close(fd)

		// A pass is issued in 256KB batches rather than one block per syscall:
		// each block is still independently self-describing, but a per-block
		// pwrite left region A starved to a few hundred blocks a second by the
		// region-B workers, so no generation ever completed and the durability
		// check had nothing to assert.
		const batch = 64
		buf := alignedBuf(blockSize * batch)
		for gen := uint32(0); !stop.Load(); gen++ {
			for i := int64(0); i < blocksA && !stop.Load(); i += batch {
				n := int64(batch)
				if i+n > blocksA {
					n = blocksA - i
				}
				for k := int64(0); k < n; k++ {
					fill(buf[k*blockSize:(k+1)*blockSize], uint32(i+k), gen)
				}
				if _, err := pwriteFull(fd, buf[:n*blockSize], i*blockSize); err != nil {
					fatal("region A pwrite block=%d gen=%d: %v", i, gen, err)
				}
				writtenA.Add(n)
			}
			if stop.Load() {
				return
			}
			// The whole point of the test: only after this returns may the
			// witness advance, and after the crash every A block must be at
			// least this generation.
			if err := syscall.Fdatasync(fd); err != nil {
				fatal("region A fdatasync gen=%d: %v", gen, err)
			}
			if err := putWitness(*witness, gen+1); err != nil {
				fatal("witness gen=%d: %v", gen, err)
			}
		}
	}()

	// Region B: never synced, so a crash always lands with writes in flight.
	stripe := blocksB / int64(*workers)
	for w := 0; w < *workers; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			fd, err := syscall.Open(*devPath, openFlags, 0)
			if err != nil {
				fatal("open %s: %v", *devPath, err)
			}
			defer syscall.Close(fd)

			buf := alignedBuf(blockSize)
			first := blocksA + int64(w)*stripe
			rng := newRNG(uint64(w) + 1)
			for gen := uint32(0); !stop.Load(); gen++ {
				for n := int64(0); n < stripe && !stop.Load(); n++ {
					i := first + int64(rng.next()%uint64(stripe))
					fill(buf, uint32(i), gen)
					if _, err := pwriteFull(fd, buf, i*blockSize); err != nil {
						fatal("region B pwrite block=%d gen=%d: %v", i, gen, err)
					}
					writtenB.Add(1)
				}
			}
		}(w)
	}

	wg.Wait()
	g, _ := getWitness(*witness)
	fmt.Printf("crash write: stopped cleanly, witness=%d blocksA-written=%d blocksB-written=%d\n",
		g, writtenA.Load(), writtenB.Load())
}

//
// check
//

func doCheck(argv []string) {
	fs := flag.NewFlagSet("check", flag.ExitOnError)
	target := fs.String("target", "", "block device or backing image to verify (required)")
	witness := fs.String("witness", "", "durability witness written by `crash write` (required)")
	minFlushed := fs.Int("min-flushed", 0, "fail unless at least this many generations were flushed before the crash")
	verbose := fs.Bool("v", false, "print every violation instead of the first few per class")
	_ = fs.Parse(argv)

	if *target == "" || *witness == "" {
		fs.Usage()
		os.Exit(2)
	}

	// The witness counts generations that completed and were flushed. Zero means
	// the crash landed inside the very first pass, so region A is not yet
	// constrained by anything; otherwise every A block owes generation
	// minGen or newer.
	generations, err := getWitness(*witness)
	if err != nil {
		fatal("read witness %s: %v", *witness, err)
	}
	minGen, constrained := uint32(0), generations > 0
	if constrained {
		minGen = generations - 1
	}
	// A crash caught during the first pass constrains nothing, so "no lost
	// blocks" would be true by default. Callers that mean the durability check
	// to have teeth say how many generations they expect to have been flushed.
	if int(generations) < *minFlushed {
		fmt.Fprintf(os.Stderr, "FAIL: only %d generation(s) flushed before the crash, needed %d — the durability check would be vacuous\n",
			generations, *minFlushed)
		os.Exit(1)
	}

	size := targetSize(*target)
	_, _, blocksA, blocksB := layout(size)

	fd, err := syscall.Open(*target, syscall.O_RDONLY, 0)
	if err != nil {
		fatal("open %s: %v", *target, err)
	}
	defer syscall.Close(fd)

	fmt.Printf("crash check: target=%s size=%d flushed-generations=%d (region A owes gen >= %d, constrained=%v) blocksA=%d blocksB=%d\n",
		*target, size, generations, minGen, constrained, blocksA, blocksB)

	var (
		lost, torn, aliased, unwritten int64
		aNewest, aOldest               = uint32(0), ^uint32(0)
		bNewest                        uint32
		bTouched                       int64
		samples                        []string
	)
	note := func(format string, a ...any) {
		if *verbose || len(samples) < 12 {
			samples = append(samples, fmt.Sprintf(format, a...))
		}
	}

	buf := alignedBuf(blockSize)
	want := make([]byte, blockSize)
	for i := int64(0); i < blocksA+blocksB; i++ {
		if _, err := preadFull(fd, buf, i*blockSize); err != nil {
			fatal("pread block=%d: %v", i, err)
		}
		inA := i < blocksA

		if binary.LittleEndian.Uint64(buf) != magic {
			// Once a generation has been flushed, region A has been written
			// end to end at least once, so a blank block there is a lost
			// write. In region B it just means the torrent never reached it.
			if inA && constrained {
				lost++
				note("LOST: region A block %d (offset %d) has no magic (never written or erased)", i, i*blockSize)
			} else {
				unwritten++
			}
			continue
		}

		idx := binary.LittleEndian.Uint32(buf[8:])
		gen := binary.LittleEndian.Uint32(buf[12:])
		if int64(idx) != i {
			aliased++
			note("ALIASED: offset %d holds block index %d (gen %d) — another block's data", i*blockSize, idx, gen)
			continue
		}
		fill(want, uint32(i), gen)
		if bad := firstDiff(buf, want); bad >= 0 {
			torn++
			note("TORN: block %d (offset %d) claims gen %d but byte %d is 0x%02x, want 0x%02x%s",
				i, i*blockSize, gen, bad, buf[bad], want[bad], guessOtherGen(buf, uint32(i), gen))
			continue
		}
		if inA {
			if gen < aOldest {
				aOldest = gen
			}
			if gen > aNewest {
				aNewest = gen
			}
			if constrained && gen < minGen {
				lost++
				note("LOST: region A block %d (offset %d) is at gen %d, but gen %d was flushed and witnessed",
					i, i*blockSize, gen, minGen)
			}
		} else {
			bTouched++
			if gen > bNewest {
				bNewest = gen
			}
		}
	}

	if aOldest == ^uint32(0) {
		aOldest = 0
	}
	// Region A's *oldest* generation is the interesting number: it is the one
	// the durability claim rests on, and it must be >= minGen.
	fmt.Printf("crash check: regionA gens [%d..%d], owed >= %d; regionB %d/%d blocks written (newest gen %d), %d never written\n",
		aOldest, aNewest, minGen, bTouched, blocksB, bNewest, unwritten)

	for _, s := range samples {
		fmt.Printf("  %s\n", s)
	}
	if n := (lost + torn + aliased) - int64(len(samples)); n > 0 && !*verbose {
		fmt.Printf("  ... and %d more (-v for all)\n", n)
	}

	if lost+torn+aliased > 0 {
		fmt.Fprintf(os.Stderr, "\nFAIL: %d lost, %d torn, %d aliased\n", lost, torn, aliased)
		os.Exit(1)
	}
	fmt.Printf("PASS: no lost, torn, or aliased blocks — region A held gen >= %d across all %d blocks after %d flushed generation(s)\n",
		minGen, blocksA, generations)
}

// guessOtherGen makes a torn block's diagnosis concrete: if the body actually
// matches some *other* nearby generation, the block is a header from one write
// spliced onto the body of another, which is the classic torn-write signature.
func guessOtherGen(got []byte, idx, claimed uint32) string {
	probe := make([]byte, blockSize)
	for d := uint32(1); d <= 4; d++ {
		for _, g := range []int64{int64(claimed) - int64(d), int64(claimed) + int64(d)} {
			if g < 0 {
				continue
			}
			fill(probe, idx, uint32(g))
			if firstDiff(got[headerLen:], probe[headerLen:]) < 0 {
				return fmt.Sprintf(" (body is gen %d — header/body splice)", g)
			}
		}
	}
	return ""
}

//
// block content
//

// fill writes the self-describing content of (blockIdx, gen) into buf. Every
// byte is a function of those two numbers, so a checker can regenerate the
// expected block from the block's own header with nothing remembered.
func fill(buf []byte, blockIdx, gen uint32) {
	binary.LittleEndian.PutUint64(buf, magic)
	binary.LittleEndian.PutUint32(buf[8:], blockIdx)
	binary.LittleEndian.PutUint32(buf[12:], gen)
	r := newRNG(uint64(blockIdx)<<32 | uint64(gen))
	for i := headerLen; i+8 <= len(buf); i += 8 {
		binary.LittleEndian.PutUint64(buf[i:], r.next())
	}
}

// layout splits the target into the synced region A (low half) and the
// never-synced region B (high half), both a whole number of blocks.
func layout(size int64) (regionA, regionB, blocksA, blocksB int64) {
	blocks := size / blockSize
	blocksA = blocks / 2
	blocksB = blocks - blocksA
	return blocksA * blockSize, blocksB * blockSize, blocksA, blocksB
}

// targetSize returns the size of a block device (BLKGETSIZE64) or a file
// (stat), floored to a whole block.
func targetSize(path string) int64 {
	fd, err := syscall.Open(path, syscall.O_RDONLY, 0)
	if err != nil {
		fatal("open %s: %v", path, err)
	}
	defer syscall.Close(fd)

	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		fatal("fstat %s: %v", path, err)
	}
	size := st.Size
	if st.Mode&syscall.S_IFMT == syscall.S_IFBLK {
		var sz uint64
		_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), blkGetSize64, uintptr(unsafe.Pointer(&sz)))
		if errno != 0 {
			fatal("BLKGETSIZE64 %s: %v", path, errno)
		}
		size = int64(sz)
	}
	return (size / blockSize) * blockSize
}

//
// witness
//

// putWitness records the newest generation of region A that has been flushed.
// It is updated by rename so a crash mid-update leaves the old value rather
// than a torn number: the oracle's own state has to be crash-safe, or every
// verdict it reaches is suspect.
func putWitness(path string, gen uint32) error {
	tmp := path + ".tmp"
	f, err := os.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(strconv.FormatUint(uint64(gen), 10) + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	// The rename itself must be durable, or a power cut can lose it.
	d, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

func getWitness(path string) (uint32, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	g, err := strconv.ParseUint(strings.TrimSpace(string(b)), 10, 32)
	if err != nil {
		return 0, err
	}
	return uint32(g), nil
}

//
// syscall helpers (shared shape with test/verify)
//

func pwriteFull(fd int, p []byte, off int64) (int, error) {
	total := 0
	for total < len(p) {
		n, err := syscall.Pwrite(fd, p[total:], off+int64(total))
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, fmt.Errorf("pwrite returned 0")
		}
		total += n
	}
	return total, nil
}

func preadFull(fd int, p []byte, off int64) (int, error) {
	total := 0
	for total < len(p) {
		n, err := syscall.Pread(fd, p[total:], off+int64(total))
		if err != nil {
			return total, err
		}
		if n == 0 {
			return total, fmt.Errorf("pread returned 0 (short read at off=%d)", off+int64(total))
		}
		total += n
	}
	return total, nil
}

func firstDiff(a, b []byte) int {
	for i := range a {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}

// alignedBuf returns a slice of length n starting on a blockSize boundary, as
// O_DIRECT requires.
func alignedBuf(n int) []byte {
	raw := make([]byte, n+blockSize)
	off := int(blockSize - (uintptr(unsafe.Pointer(&raw[0])) % blockSize))
	if off == blockSize {
		off = 0
	}
	return raw[off : off+n]
}

type rng struct{ s uint64 }

func newRNG(seed uint64) *rng { return &rng{s: seed} }

func (r *rng) next() uint64 {
	r.s += 0x9E3779B97F4A7C15
	z := r.s
	z = (z ^ (z >> 30)) * 0xBF58476D1CE4E5B9
	z = (z ^ (z >> 27)) * 0x94D049BB133111EB
	return z ^ (z >> 31)
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "crash: "+format+"\n", a...)
	os.Exit(1)
}
