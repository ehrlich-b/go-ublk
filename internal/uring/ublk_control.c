#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <errno.h>
#include <fcntl.h>
#include <unistd.h>
#include <sys/ioctl.h>
#include <linux/types.h>

// UBLK ioctl definitions
#define UBLK_CMD_ADD_DEV    0x04
#define UBLK_CMD_SET_PARAMS 0x05
#define UBLK_CMD_START_DEV  0x06
#define UBLK_CMD_STOP_DEV   0x10
#define UBLK_CMD_DEL_DEV    0x02
#define UBLK_CMD_GET_DEV_INFO  0x01
#define UBLK_CMD_GET_PARAMS    0x09

#define UBLK_IOC_MAGIC 'u'
#define UBLK_IOCTL_CMD(cmd) _IOWR(UBLK_IOC_MAGIC, cmd, struct ublksrv_ctrl_cmd)

struct ublksrv_ctrl_cmd {
    __u32 dev_id;
    __u16 queue_id;
    __u16 len;
    __u64 addr;
    __u64 data;
    __u16 dev_path_len;
    __u16 pad;
    __u32 reserved;
};

// Wrapper functions to be called from Go
int ublk_add_dev(int fd, unsigned int dev_id, unsigned short queue_id,
                 unsigned short len, void* addr) {
    struct ublksrv_ctrl_cmd cmd = {
        .dev_id = dev_id,
        .queue_id = queue_id,
        .len = len,
        .addr = (__u64)addr,
        .data = 0,
        .dev_path_len = 0,
        .pad = 0,
        .reserved = 0
    };

    int ret = ioctl(fd, UBLK_IOCTL_CMD(UBLK_CMD_ADD_DEV), &cmd);
    if (ret < 0) {
        return -errno;
    }
    return ret;
}

int ublk_set_params(int fd, unsigned int dev_id, unsigned short len, void* addr) {
    struct ublksrv_ctrl_cmd cmd = {
        .dev_id = dev_id,
        .queue_id = 0xFFFF,
        .len = len,
        .addr = (__u64)addr,
        .data = 0,
        .dev_path_len = 0,
        .pad = 0,
        .reserved = 0
    };

    int ret = ioctl(fd, UBLK_IOCTL_CMD(UBLK_CMD_SET_PARAMS), &cmd);
    if (ret < 0) {
        return -errno;
    }
    return ret;
}

int ublk_start_dev(int fd, unsigned int dev_id, unsigned long pid) {
    struct ublksrv_ctrl_cmd cmd = {
        .dev_id = dev_id,
        .queue_id = 0xFFFF,
        .len = 0,
        .addr = 0,
        .data = pid,
        .dev_path_len = 0,
        .pad = 0,
        .reserved = 0
    };

    int ret = ioctl(fd, UBLK_IOCTL_CMD(UBLK_CMD_START_DEV), &cmd);
    if (ret < 0) {
        return -errno;
    }
    return ret;
}

int ublk_stop_dev(int fd, unsigned int dev_id) {
    struct ublksrv_ctrl_cmd cmd = {
        .dev_id = dev_id,
        .queue_id = 0xFFFF,
        .len = 0,
        .addr = 0,
        .data = 0,
        .dev_path_len = 0,
        .pad = 0,
        .reserved = 0
    };

    int ret = ioctl(fd, UBLK_IOCTL_CMD(UBLK_CMD_STOP_DEV), &cmd);
    if (ret < 0) {
        return -errno;
    }
    return ret;
}

int ublk_del_dev(int fd, unsigned int dev_id) {
    struct ublksrv_ctrl_cmd cmd = {
        .dev_id = dev_id,
        .queue_id = 0xFFFF,
        .len = 0,
        .addr = 0,
        .data = 0,
        .dev_path_len = 0,
        .pad = 0,
        .reserved = 0
    };

    int ret = ioctl(fd, UBLK_IOCTL_CMD(UBLK_CMD_DEL_DEV), &cmd);
    if (ret < 0) {
        return -errno;
    }
    return ret;
}