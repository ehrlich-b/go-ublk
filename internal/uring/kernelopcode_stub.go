//go:build !cgo || !linux

package uring

// kernelUringCmdOpcode returns a safe default for IORING_OP_URING_CMD.
// Modern 6.x kernels commonly use 51. If this is wrong for the build
// host, prefer building on the target VM with cgo enabled so we can
// read the correct value from kernel headers.
func kernelUringCmdOpcode() uint8 { return 51 }

