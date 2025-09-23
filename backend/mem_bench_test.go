package backend

import (
	"fmt"
	"math/rand"
	"testing"
	"time"
)

// BenchmarkMemoryBackend measures the raw performance of memory backend operations
func BenchmarkMemoryBackend(b *testing.B) {
	sizes := []int{
		4 * 1024,    // 4KB
		128 * 1024,  // 128KB
		1024 * 1024, // 1MB
	}

	for _, size := range sizes {
		b.Run(formatSize(size), func(b *testing.B) {
			backend := NewMemory(64 << 20) // 64MB backend
			data := make([]byte, size)
			rand.Read(data) // Random data to avoid compression optimizations

			b.Run("ReadAt", func(b *testing.B) {
				buf := make([]byte, size)
				b.SetBytes(int64(size))
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					offset := int64(rand.Intn(64<<20 - size))
					backend.ReadAt(buf, offset)
				}
			})

			b.Run("WriteAt", func(b *testing.B) {
				b.SetBytes(int64(size))
				b.ResetTimer()

				for i := 0; i < b.N; i++ {
					offset := int64(rand.Intn(64<<20 - size))
					backend.WriteAt(data, offset)
				}
			})

			b.Run("ReadAt_Sequential", func(b *testing.B) {
				buf := make([]byte, size)
				b.SetBytes(int64(size))
				b.ResetTimer()

				offset := int64(0)
				for i := 0; i < b.N; i++ {
					backend.ReadAt(buf, offset)
					offset += int64(size)
					if offset+int64(size) > backend.Size() {
						offset = 0
					}
				}
			})

			b.Run("WriteAt_Sequential", func(b *testing.B) {
				b.SetBytes(int64(size))
				b.ResetTimer()

				offset := int64(0)
				for i := 0; i < b.N; i++ {
					backend.WriteAt(data, offset)
					offset += int64(size)
					if offset+int64(size) > backend.Size() {
						offset = 0
					}
				}
			})
		})
	}
}

// BenchmarkMemoryBackendConcurrent measures concurrent access performance
func BenchmarkMemoryBackendConcurrent(b *testing.B) {
	backend := NewMemory(64 << 20) // 64MB backend
	blockSize := 4096              // 4KB blocks

	concurrencies := []int{1, 4, 8, 16, 32}

	for _, concurrency := range concurrencies {
		b.Run(fmt.Sprintf("Concurrency_%d", concurrency), func(b *testing.B) {
			b.SetBytes(int64(blockSize))

			b.RunParallel(func(pb *testing.PB) {
				buf := make([]byte, blockSize)
				data := make([]byte, blockSize)
				rand.Read(data)

				for pb.Next() {
					offset := int64(rand.Intn(64<<20 - blockSize))

					// Mix of reads and writes (70% read, 30% write)
					if rand.Float32() < 0.7 {
						backend.ReadAt(buf, offset)
					} else {
						backend.WriteAt(data, offset)
					}
				}
			})
		})
	}
}

// BenchmarkMemoryBackendLatency measures operation latency distribution
func BenchmarkMemoryBackendLatency(b *testing.B) {
	backend := NewMemory(64 << 20) // 64MB backend
	blockSize := 4096
	buf := make([]byte, blockSize)
	data := make([]byte, blockSize)
	rand.Read(data)

	b.Run("ReadLatency", func(b *testing.B) {
		latencies := make([]time.Duration, 0, b.N)
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			offset := int64(rand.Intn(64<<20 - blockSize))

			start := time.Now()
			backend.ReadAt(buf, offset)
			latencies = append(latencies, time.Since(start))
		}

		b.StopTimer()
		// Report percentiles
		reportLatencyPercentiles(b, latencies)
	})

	b.Run("WriteLatency", func(b *testing.B) {
		latencies := make([]time.Duration, 0, b.N)
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			offset := int64(rand.Intn(64<<20 - blockSize))

			start := time.Now()
			backend.WriteAt(data, offset)
			latencies = append(latencies, time.Since(start))
		}

		b.StopTimer()
		reportLatencyPercentiles(b, latencies)
	})
}

// BenchmarkMemoryOverhead measures the overhead of the locking mechanism
func BenchmarkMemoryOverhead(b *testing.B) {
	size := 4096
	data := make([]byte, size)

	// Baseline: raw memory copy without any locking
	b.Run("RawMemcpy", func(b *testing.B) {
		src := make([]byte, 64<<20)
		dst := make([]byte, size)
		b.SetBytes(int64(size))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			offset := rand.Intn(64<<20 - size)
			copy(dst, src[offset:offset+size])
		}
	})

	// With RWMutex (read lock)
	b.Run("WithRWMutexRead", func(b *testing.B) {
		backend := NewMemory(64 << 20)
		buf := make([]byte, size)
		b.SetBytes(int64(size))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			offset := int64(rand.Intn(64<<20 - size))
			backend.ReadAt(buf, offset)
		}
	})

	// With RWMutex (write lock)
	b.Run("WithRWMutexWrite", func(b *testing.B) {
		backend := NewMemory(64 << 20)
		b.SetBytes(int64(size))
		b.ResetTimer()

		for i := 0; i < b.N; i++ {
			offset := int64(rand.Intn(64<<20 - size))
			backend.WriteAt(data, offset)
		}
	})
}

func formatSize(bytes int) string {
	switch {
	case bytes >= 1<<20:
		return fmt.Sprintf("%dMB", bytes/(1<<20))
	case bytes >= 1<<10:
		return fmt.Sprintf("%dKB", bytes/(1<<10))
	default:
		return fmt.Sprintf("%dB", bytes)
	}
}

func reportLatencyPercentiles(b *testing.B, latencies []time.Duration) {
	if len(latencies) == 0 {
		return
	}

	// Sort latencies
	for i := 0; i < len(latencies); i++ {
		for j := i + 1; j < len(latencies); j++ {
			if latencies[i] > latencies[j] {
				latencies[i], latencies[j] = latencies[j], latencies[i]
			}
		}
	}

	p50 := latencies[len(latencies)*50/100]
	p90 := latencies[len(latencies)*90/100]
	p99 := latencies[len(latencies)*99/100]

	b.Logf("Latency percentiles: p50=%v, p90=%v, p99=%v", p50, p90, p99)
}
