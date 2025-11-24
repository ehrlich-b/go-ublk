//go:build linux && cgo

package uring

/*
#include <stdint.h>

// x86-64 store fence to ensure all prior stores are globally visible
static inline void sfence_impl(void) {
    __asm__ __volatile__("sfence" ::: "memory");
}

// x86-64 full memory fence to ensure all prior memory operations are complete
static inline void mfence_impl(void) {
    __asm__ __volatile__("mfence" ::: "memory");
}
*/
import "C"

// Sfence issues a store fence (x86 SFENCE instruction).
// This ensures all prior stores are globally visible before any subsequent
// stores. Required for io_uring SQE visibility before updating the tail.
func Sfence() {
	C.sfence_impl()
}

// Mfence issues a full memory fence (x86 MFENCE instruction).
// This ensures all prior memory operations are complete before any
// subsequent memory operations.
func Mfence() {
	C.mfence_impl()
}
