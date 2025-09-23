//go:build !cgo || !linux

package uring

// kernelUringCmdOpcode returns a safe default for IORING_OP_URING_CMD.
// Linux 6.11 kernel uses 46 for IORING_OP_URING_CMD.
// If this is wrong for the build host, prefer building on the target VM
// with cgo enabled so we can read the correct value from kernel headers.
func kernelUringCmdOpcode() uint8 { return 46 }
