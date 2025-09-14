//go:build linux && cgo

package uring

/*
#include <linux/io_uring.h>
static unsigned char get_uring_cmd_opcode() {
    return (unsigned char)IORING_OP_URING_CMD;
}
*/
import "C"

// kernelUringCmdOpcode returns the kernel's IORING_OP_URING_CMD opcode value.
func kernelUringCmdOpcode() uint8 {
    return uint8(C.get_uring_cmd_opcode())
}

