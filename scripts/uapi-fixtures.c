/* Compile against an independently selected Linux UAPI header, without ublk IO.
 * cc -Wall -Werror -I/path/containing/linux/ublk_cmd.h scripts/uapi-fixtures.c -o /tmp/uapi-fixtures
 * /tmp/uapi-fixtures
 * Layout/byte fixtures are plain text for review; no kernel/device is opened.
 */
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/ioctl.h>
#include <linux/ublk_cmd.h>

_Static_assert(sizeof(struct ublksrv_ctrl_cmd) == 32, "ctrl cmd size");
_Static_assert(sizeof(struct ublksrv_ctrl_dev_info) == 64, "dev info size");
_Static_assert(sizeof(struct ublksrv_io_cmd) == 16, "io cmd size");
_Static_assert(sizeof(struct ublksrv_io_desc) == 24, "io desc size");
_Static_assert(offsetof(struct ublk_params, basic) == 8, "basic offset");
_Static_assert(offsetof(struct ublk_params, discard) == 40, "discard offset");
#ifdef UBLK_PARAM_TYPE_DEVT
_Static_assert(offsetof(struct ublk_params, devt) == 60, "devt offset");
#endif
#ifdef UBLK_PARAM_TYPE_ZONED
_Static_assert(offsetof(struct ublk_params, zoned) == 76, "zoned offset");
#endif

static void dump(const char *name, const void *data, size_t size) {
    const unsigned char *p = data;
    printf("%s=", name);
    for (size_t i = 0; i < size; i++) printf("%02x", p[i]);
    putchar('\n');
}

int main(void) {
    struct ublksrv_ctrl_cmd ctrl = {0};
    ctrl.dev_id = UINT32_C(0x12345678);
    ctrl.queue_id = UINT16_C(0xabcd);
    ctrl.len = UINT16_C(0x1234);
    ctrl.addr = UINT64_C(0x1122334455667788);
    ctrl.data[0] = UINT64_C(0x8877665544332211);
    dump("ctrl", &ctrl, sizeof(ctrl));

    struct ublksrv_io_cmd io = {0};
    io.q_id = UINT16_C(0x0123);
    io.tag = UINT16_C(0xfedc);
    io.result = -5;
    io.addr = UINT64_C(0x8877665544332211);
    dump("io", &io, sizeof(io));

    struct ublk_params params;
    memset(&params, 0, sizeof(params));
    params.len = 40;
    params.types = UBLK_PARAM_TYPE_BASIC;
    params.basic.attrs = UINT32_C(0x12345678);
    params.basic.logical_bs_shift = 9;
    params.basic.physical_bs_shift = 12;
    params.basic.max_sectors = UINT32_C(0x87654321);
    params.basic.dev_sectors = UINT64_C(0x1122334455667788);
    dump("basic", &params, params.len);
#ifdef UBLK_PARAM_TYPE_DEVT
    params.types |= UBLK_PARAM_TYPE_DEVT;
    params.len = offsetof(struct ublk_params, devt) + sizeof(params.devt);
    params.devt.char_major = UINT32_C(0x11223344);
    params.devt.char_minor = UINT32_C(0x55667788);
    params.devt.disk_major = UINT32_C(0x99aabbcc);
    params.devt.disk_minor = UINT32_C(0xddeeff00);
    dump("basic_devt", &params, params.len);
#endif
    fprintf(stderr, "params sizeof=%zu basic=%zu discard=%zu", sizeof(params), offsetof(struct ublk_params, basic), offsetof(struct ublk_params, discard));
#ifdef UBLK_PARAM_TYPE_DEVT
    fprintf(stderr, " devt=%zu", offsetof(struct ublk_params, devt));
#endif
#ifdef UBLK_PARAM_TYPE_ZONED
    fprintf(stderr, " zoned=%zu", offsetof(struct ublk_params, zoned));
#endif
    fputc('\n', stderr);
    return 0;
}
