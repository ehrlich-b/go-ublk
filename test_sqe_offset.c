#include <stdio.h>
#include <stddef.h>
#include <stdint.h>

// Simplified io_uring_sqe structure from kernel
struct io_uring_sqe {
    uint8_t  opcode;        // 0
    uint8_t  flags;         // 1
    uint16_t ioprio;        // 2-3
    int32_t  fd;            // 4-7
    union {
        uint64_t off;       // 8-15
        uint64_t addr2;
        struct {
            uint32_t cmd_op;
            uint32_t __pad1;
        };
    };
    union {
        uint64_t addr;      // 16-23
        uint64_t splice_off_in;
        void *   __pad2;
    };
    uint32_t len;           // 24-27
    union {
        uint32_t uring_cmd_flags;  // 28-31
        uint32_t rw_flags;
    };
    uint64_t user_data;     // 32-39
    union {
        struct {
            union {
                uint16_t buf_index;  // 40-41
                uint16_t buf_group;
            };
            uint16_t personality;    // 42-43
            union {
                int32_t splice_fd_in;  // 44-47
                uint32_t file_index;
                uint32_t __pad3;
                struct {
                    uint16_t addr_len;
                    uint16_t __pad4;
                };
            };
        };
        uint64_t __pad5[2];
    };
    // Note: with IORING_SETUP_SQE128, there are additional fields:
    // union {
    //     uint64_t addr3;      // 48-55
    //     uint64_t __pad6[1];
    // };
};

int main() {
    printf("io_uring_sqe size: %zu\n", sizeof(struct io_uring_sqe));
    printf("offsetof(opcode): %zu\n", offsetof(struct io_uring_sqe, opcode));
    printf("offsetof(fd): %zu\n", offsetof(struct io_uring_sqe, fd));
    printf("offsetof(off): %zu\n", offsetof(struct io_uring_sqe, off));
    printf("offsetof(addr): %zu\n", offsetof(struct io_uring_sqe, addr));
    printf("offsetof(len): %zu\n", offsetof(struct io_uring_sqe, len));
    printf("offsetof(uring_cmd_flags): %zu\n", offsetof(struct io_uring_sqe, uring_cmd_flags));
    printf("offsetof(user_data): %zu\n", offsetof(struct io_uring_sqe, user_data));
    printf("offsetof(buf_index): %zu\n", offsetof(struct io_uring_sqe, buf_index));
    printf("offsetof(splice_fd_in): %zu\n", offsetof(struct io_uring_sqe, splice_fd_in));

    // Important: with SQE128, addr3 would be at offset 48
    printf("\n*** With SQE128, addr3 would be at offset 48 ***\n");
    printf("*** C code uses &sqe->addr3 which is offset 48 ***\n");

    return 0;
}