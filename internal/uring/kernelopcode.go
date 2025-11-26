package uring

// kernelUringCmdOpcode returns the IORING_OP_URING_CMD opcode.
// Linux 6.0+ uses 46 for IORING_OP_URING_CMD.
func kernelUringCmdOpcode() uint8 { return 46 }
