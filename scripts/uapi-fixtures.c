/* Emit layout, constant and byte fixtures from an independently selected Linux
 * UAPI header, without ublk I/O. Run through scripts/uapi-fixtures.sh, which
 * also checks that every macro and struct field in the header is covered here:
 *
 *   scripts/uapi-fixtures.sh /path/to/include > internal/uapi/testdata/linux-le-fixtures.txt
 *
 * The include directory must hold linux/ublk_cmd.h and a linux/fs.h new enough
 * to define LBMD_PI_*. No kernel or device is opened. Output lines:
 *
 *   const  NAME VALUE                  every numeric macro, as a signed decimal
 *   sizeof STRUCT SIZE
 *   field  STRUCT.MEMBER OFFSET SIZE   every member, union alternatives included
 *   bytes  NAME HEX                    a struct filled with the values below
 *   helper NAME ARGS... RESULTS...     header inline functions on sample inputs
 */
#include <errno.h> /* UBLK_IO_RES_ABORT is -ENODEV */
#include <stddef.h>
#include <stdint.h>
#include <stdio.h>
#include <string.h>
#include <sys/ioctl.h>
#include <linux/fs.h>
#include <linux/ublk_cmd.h>

#ifndef LBMD_PI_CAP_INTEGRITY
#error "linux/fs.h predates LBMD_PI_*; point the include path at v6.17+ UAPI headers"
#endif

#define C(name) printf("const %s %lld\n", #name, (long long)(name))
#define S(type) printf("sizeof %s %zu\n", #type, sizeof(struct type))
#define F(type, member) printf("field %s.%s %zu %zu\n", #type, #member, \
	offsetof(struct type, member), sizeof(((struct type *)0)->member))

static void dump(const char *name, const void *data, size_t size)
{
	const unsigned char *p = data;

	printf("bytes %s ", name);
	for (size_t i = 0; i < size; i++)
		printf("%02x", p[i]);
	putchar('\n');
}

static void constants(void)
{
	C(UBLK_CMD_GET_QUEUE_AFFINITY); C(UBLK_CMD_GET_DEV_INFO); C(UBLK_CMD_ADD_DEV);
	C(UBLK_CMD_DEL_DEV); C(UBLK_CMD_START_DEV); C(UBLK_CMD_STOP_DEV);
	C(UBLK_CMD_SET_PARAMS); C(UBLK_CMD_GET_PARAMS); C(UBLK_CMD_START_USER_RECOVERY);
	C(UBLK_CMD_END_USER_RECOVERY); C(UBLK_CMD_GET_DEV_INFO2);

	C(UBLK_U_CMD_GET_QUEUE_AFFINITY); C(UBLK_U_CMD_GET_DEV_INFO); C(UBLK_U_CMD_ADD_DEV);
	C(UBLK_U_CMD_DEL_DEV); C(UBLK_U_CMD_START_DEV); C(UBLK_U_CMD_STOP_DEV);
	C(UBLK_U_CMD_SET_PARAMS); C(UBLK_U_CMD_GET_PARAMS); C(UBLK_U_CMD_START_USER_RECOVERY);
	C(UBLK_U_CMD_END_USER_RECOVERY); C(UBLK_U_CMD_GET_DEV_INFO2); C(UBLK_U_CMD_GET_FEATURES);
	C(UBLK_U_CMD_DEL_DEV_ASYNC); C(UBLK_U_CMD_UPDATE_SIZE); C(UBLK_U_CMD_QUIESCE_DEV);
	C(UBLK_U_CMD_TRY_STOP_DEV); C(UBLK_U_CMD_REG_BUF); C(UBLK_U_CMD_UNREG_BUF);

	C(UBLK_SHMEM_BUF_READ_ONLY); C(UBLK_FEATURES_LEN);

	C(UBLK_IO_FETCH_REQ); C(UBLK_IO_COMMIT_AND_FETCH_REQ); C(UBLK_IO_NEED_GET_DATA);
	C(UBLK_U_IO_FETCH_REQ); C(UBLK_U_IO_COMMIT_AND_FETCH_REQ); C(UBLK_U_IO_NEED_GET_DATA);
	C(UBLK_U_IO_REGISTER_IO_BUF); C(UBLK_U_IO_UNREGISTER_IO_BUF);
	C(UBLK_U_IO_PREP_IO_CMDS); C(UBLK_U_IO_COMMIT_IO_CMDS); C(UBLK_U_IO_FETCH_IO_CMDS);

	C(UBLK_IO_RES_OK); C(UBLK_IO_RES_NEED_GET_DATA); C(UBLK_IO_RES_ABORT);
	C(UBLKSRV_CMD_BUF_OFFSET); C(UBLKSRV_IO_BUF_OFFSET); C(UBLK_MAX_QUEUE_DEPTH);
	C(UBLK_IO_BUF_OFF); C(UBLK_IO_BUF_BITS); C(UBLK_IO_BUF_BITS_MASK);
	C(UBLK_TAG_OFF); C(UBLK_TAG_BITS); C(UBLK_TAG_BITS_MASK);
	C(UBLK_QID_OFF); C(UBLK_QID_BITS); C(UBLK_QID_BITS_MASK); C(UBLK_MAX_NR_QUEUES);
	C(UBLKSRV_IO_BUF_TOTAL_BITS); C(UBLKSRV_IO_BUF_TOTAL_SIZE);
	C(UBLK_INTEGRITY_FLAG_OFF); C(UBLKSRV_IO_INTEGRITY_FLAG);

	C(UBLK_F_SUPPORT_ZERO_COPY); C(UBLK_F_URING_CMD_COMP_IN_TASK); C(UBLK_F_NEED_GET_DATA);
	C(UBLK_F_USER_RECOVERY); C(UBLK_F_USER_RECOVERY_REISSUE); C(UBLK_F_UNPRIVILEGED_DEV);
	C(UBLK_F_CMD_IOCTL_ENCODE); C(UBLK_F_USER_COPY); C(UBLK_F_ZONED);
	C(UBLK_F_USER_RECOVERY_FAIL_IO); C(UBLK_F_UPDATE_SIZE); C(UBLK_F_AUTO_BUF_REG);
	C(UBLK_F_QUIESCE); C(UBLK_F_PER_IO_DAEMON); C(UBLK_F_BUF_REG_OFF_DAEMON);
	C(UBLK_F_BATCH_IO); C(UBLK_F_INTEGRITY); C(UBLK_F_SAFE_STOP_DEV);
	C(UBLK_F_NO_AUTO_PART_SCAN); C(UBLK_F_SHMEM_ZC); C(UBLK_F_IO_DESC_SIZE);

	C(UBLK_S_DEV_DEAD); C(UBLK_S_DEV_LIVE); C(UBLK_S_DEV_QUIESCED); C(UBLK_S_DEV_FAIL_IO);

	C(UBLK_IO_OP_READ); C(UBLK_IO_OP_WRITE); C(UBLK_IO_OP_FLUSH); C(UBLK_IO_OP_DISCARD);
	C(UBLK_IO_OP_WRITE_SAME); C(UBLK_IO_OP_WRITE_ZEROES); C(UBLK_IO_OP_ZONE_OPEN);
	C(UBLK_IO_OP_ZONE_CLOSE); C(UBLK_IO_OP_ZONE_FINISH); C(UBLK_IO_OP_ZONE_APPEND);
	C(UBLK_IO_OP_ZONE_RESET_ALL); C(UBLK_IO_OP_ZONE_RESET); C(UBLK_IO_OP_REPORT_ZONES);

	C(UBLK_IO_F_FAILFAST_DEV); C(UBLK_IO_F_FAILFAST_TRANSPORT); C(UBLK_IO_F_FAILFAST_DRIVER);
	C(UBLK_IO_F_META); C(UBLK_IO_F_FUA); C(UBLK_IO_F_NOUNMAP); C(UBLK_IO_F_SWAP);
	C(UBLK_IO_F_NEED_REG_BUF); C(UBLK_IO_F_INTEGRITY); C(UBLK_IO_F_SHMEM_ZC);

	C(UBLK_AUTO_BUF_REG_FALLBACK); C(UBLK_AUTO_BUF_REG_F_MASK);
	C(UBLK_BATCH_F_HAS_ZONE_LBA); C(UBLK_BATCH_F_HAS_BUF_ADDR);
	C(UBLK_BATCH_F_AUTO_BUF_REG_FALLBACK);

	C(UBLK_ATTR_READ_ONLY); C(UBLK_ATTR_ROTATIONAL); C(UBLK_ATTR_VOLATILE_CACHE);
	C(UBLK_ATTR_FUA); C(UBLK_MIN_SEGMENT_SIZE);

	C(UBLK_PARAM_TYPE_BASIC); C(UBLK_PARAM_TYPE_DISCARD); C(UBLK_PARAM_TYPE_DEVT);
	C(UBLK_PARAM_TYPE_ZONED); C(UBLK_PARAM_TYPE_DMA_ALIGN); C(UBLK_PARAM_TYPE_SEGMENT);
	C(UBLK_PARAM_TYPE_INTEGRITY);

	C(UBLK_SHMEM_ZC_OFF_MASK); C(UBLK_SHMEM_ZC_IDX_OFF); C(UBLK_SHMEM_ZC_IDX_MASK);

	/* linux/fs.h, referenced by ublk_param_integrity */
	C(LBMD_PI_CAP_INTEGRITY); C(LBMD_PI_CAP_REFTAG); C(LBMD_PI_CSUM_NONE);
	C(LBMD_PI_CSUM_IP); C(LBMD_PI_CSUM_CRC16_T10DIF); C(LBMD_PI_CSUM_CRC64_NVME);
}

static void layouts(void)
{
	S(ublk_shmem_buf_reg);
	F(ublk_shmem_buf_reg, addr); F(ublk_shmem_buf_reg, len);
	F(ublk_shmem_buf_reg, flags); F(ublk_shmem_buf_reg, reserved);

	S(ublksrv_ctrl_cmd);
	F(ublksrv_ctrl_cmd, dev_id); F(ublksrv_ctrl_cmd, queue_id); F(ublksrv_ctrl_cmd, len);
	F(ublksrv_ctrl_cmd, addr); F(ublksrv_ctrl_cmd, data); F(ublksrv_ctrl_cmd, dev_path_len);
	F(ublksrv_ctrl_cmd, pad); F(ublksrv_ctrl_cmd, reserved);

	S(ublksrv_ctrl_dev_info);
	F(ublksrv_ctrl_dev_info, nr_hw_queues); F(ublksrv_ctrl_dev_info, queue_depth);
	F(ublksrv_ctrl_dev_info, state); F(ublksrv_ctrl_dev_info, io_desc_size);
	F(ublksrv_ctrl_dev_info, max_io_buf_bytes); F(ublksrv_ctrl_dev_info, dev_id);
	F(ublksrv_ctrl_dev_info, ublksrv_pid); F(ublksrv_ctrl_dev_info, pad1);
	F(ublksrv_ctrl_dev_info, flags); F(ublksrv_ctrl_dev_info, ublksrv_flags);
	F(ublksrv_ctrl_dev_info, owner_uid); F(ublksrv_ctrl_dev_info, owner_gid);
	F(ublksrv_ctrl_dev_info, reserved1); F(ublksrv_ctrl_dev_info, reserved2);

	S(ublksrv_io_desc);
	F(ublksrv_io_desc, op_flags); F(ublksrv_io_desc, nr_sectors); F(ublksrv_io_desc, nr_zones);
	F(ublksrv_io_desc, start_sector); F(ublksrv_io_desc, addr);

	S(ublk_auto_buf_reg);
	F(ublk_auto_buf_reg, index); F(ublk_auto_buf_reg, flags);
	F(ublk_auto_buf_reg, reserved0); F(ublk_auto_buf_reg, reserved1);

	S(ublksrv_io_cmd);
	F(ublksrv_io_cmd, q_id); F(ublksrv_io_cmd, tag); F(ublksrv_io_cmd, result);
	F(ublksrv_io_cmd, addr); F(ublksrv_io_cmd, zone_append_lba);

	S(ublk_elem_header);
	F(ublk_elem_header, tag); F(ublk_elem_header, buf_index); F(ublk_elem_header, result);

	S(ublk_batch_io);
	F(ublk_batch_io, q_id); F(ublk_batch_io, flags); F(ublk_batch_io, nr_elem);
	F(ublk_batch_io, elem_bytes); F(ublk_batch_io, reserved); F(ublk_batch_io, reserved2);

	S(ublk_param_basic);
	F(ublk_param_basic, attrs); F(ublk_param_basic, logical_bs_shift);
	F(ublk_param_basic, physical_bs_shift); F(ublk_param_basic, io_opt_shift);
	F(ublk_param_basic, io_min_shift); F(ublk_param_basic, max_sectors);
	F(ublk_param_basic, chunk_sectors); F(ublk_param_basic, dev_sectors);
	F(ublk_param_basic, virt_boundary_mask);

	S(ublk_param_discard);
	F(ublk_param_discard, discard_alignment); F(ublk_param_discard, discard_granularity);
	F(ublk_param_discard, max_discard_sectors); F(ublk_param_discard, max_write_zeroes_sectors);
	F(ublk_param_discard, max_discard_segments); F(ublk_param_discard, reserved0);

	S(ublk_param_devt);
	F(ublk_param_devt, char_major); F(ublk_param_devt, char_minor);
	F(ublk_param_devt, disk_major); F(ublk_param_devt, disk_minor);

	S(ublk_param_zoned);
	F(ublk_param_zoned, max_open_zones); F(ublk_param_zoned, max_active_zones);
	F(ublk_param_zoned, max_zone_append_sectors); F(ublk_param_zoned, reserved);

	S(ublk_param_dma_align);
	F(ublk_param_dma_align, alignment); F(ublk_param_dma_align, pad);

	S(ublk_param_segment);
	F(ublk_param_segment, seg_boundary_mask); F(ublk_param_segment, max_segment_size);
	F(ublk_param_segment, max_segments); F(ublk_param_segment, pad);

	S(ublk_param_integrity);
	F(ublk_param_integrity, flags); F(ublk_param_integrity, max_integrity_segments);
	F(ublk_param_integrity, interval_exp); F(ublk_param_integrity, metadata_size);
	F(ublk_param_integrity, pi_offset); F(ublk_param_integrity, csum_type);
	F(ublk_param_integrity, tag_size); F(ublk_param_integrity, pad);

	S(ublk_params);
	F(ublk_params, len); F(ublk_params, types); F(ublk_params, basic);
	F(ublk_params, discard); F(ublk_params, devt); F(ublk_params, zoned);
	F(ublk_params, dma); F(ublk_params, seg); F(ublk_params, integrity);
}

/* Values must match fixtureValues in internal/uapi/kernel_fixture_test.go. */
static void structs(void)
{
	struct ublksrv_ctrl_cmd ctrl;
	memset(&ctrl, 0, sizeof(ctrl));
	ctrl.dev_id = UINT32_C(0x12345678);
	ctrl.queue_id = UINT16_C(0xabcd);
	ctrl.len = UINT16_C(0x1234);
	ctrl.addr = UINT64_C(0x1122334455667788);
	ctrl.data[0] = UINT64_C(0x8877665544332211);
	ctrl.dev_path_len = UINT16_C(0x0102);
	ctrl.pad = UINT16_C(0x0304);
	ctrl.reserved = UINT32_C(0x05060708);
	dump("ctrl", &ctrl, sizeof(ctrl));

	struct ublksrv_ctrl_dev_info info;
	memset(&info, 0, sizeof(info));
	info.nr_hw_queues = 0x0102;
	info.queue_depth = 0x0304;
	info.state = 0x0506;
	info.io_desc_size = 0x0708;
	info.max_io_buf_bytes = UINT32_C(0x090a0b0c);
	info.dev_id = UINT32_C(0x0d0e0f10);
	info.ublksrv_pid = -2;
	info.pad1 = UINT32_C(0x11121314);
	info.flags = UINT64_C(0x15161718191a1b1c);
	info.ublksrv_flags = UINT64_C(0x1d1e1f2021222324);
	info.owner_uid = UINT32_C(0x25262728);
	info.owner_gid = UINT32_C(0x292a2b2c);
	info.reserved1 = UINT64_C(0x2d2e2f3031323334);
	info.reserved2 = UINT64_C(0x35363738393a3b3c);
	dump("dev_info", &info, sizeof(info));

	struct ublksrv_io_desc iod;
	memset(&iod, 0, sizeof(iod));
	iod.op_flags = UINT32_C(0x01020304);
	iod.nr_sectors = UINT32_C(0x05060708);
	iod.start_sector = UINT64_C(0x090a0b0c0d0e0f10);
	iod.addr = UINT64_C(0x1112131415161718);
	dump("io_desc", &iod, sizeof(iod));

	struct ublksrv_io_cmd io;
	memset(&io, 0, sizeof(io));
	io.q_id = UINT16_C(0x0123);
	io.tag = UINT16_C(0xfedc);
	io.result = -5;
	io.addr = UINT64_C(0x8877665544332211);
	dump("io", &io, sizeof(io));

	struct ublk_elem_header elem;
	memset(&elem, 0, sizeof(elem));
	elem.tag = 0x0102;
	elem.buf_index = 0x0304;
	elem.result = -7;
	dump("elem_header", &elem, sizeof(elem));

	struct ublk_batch_io batch;
	memset(&batch, 0, sizeof(batch));
	batch.q_id = 0x0102;
	batch.flags = 0x0304;
	batch.nr_elem = 0x0506;
	batch.elem_bytes = 0x07;
	batch.reserved = 0x08;
	batch.reserved2 = UINT64_C(0x090a0b0c0d0e0f10);
	dump("batch_io", &batch, sizeof(batch));

	struct ublk_auto_buf_reg reg;
	memset(&reg, 0, sizeof(reg));
	reg.index = 0x0102;
	reg.flags = 0x03;
	reg.reserved0 = 0x04;
	reg.reserved1 = UINT32_C(0x05060708);
	dump("auto_buf_reg", &reg, sizeof(reg));

	struct ublk_shmem_buf_reg shm;
	memset(&shm, 0, sizeof(shm));
	shm.addr = UINT64_C(0x0102030405060708);
	shm.len = UINT64_C(0x1112131415161718);
	shm.flags = UINT32_C(0x21222324);
	shm.reserved = UINT32_C(0x25262728);
	dump("shmem_buf_reg", &shm, sizeof(shm));

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
	params.types |= UBLK_PARAM_TYPE_DEVT;
	params.len = offsetof(struct ublk_params, devt) + sizeof(params.devt);
	params.devt.char_major = UINT32_C(0x11223344);
	params.devt.char_minor = UINT32_C(0x55667788);
	params.devt.disk_major = UINT32_C(0x99aabbcc);
	params.devt.disk_minor = UINT32_C(0xddeeff00);
	dump("basic_devt", &params, params.len);

	memset(&params, 0, sizeof(params));
	params.len = sizeof(params);
	params.types = UBLK_PARAM_TYPE_BASIC | UBLK_PARAM_TYPE_DISCARD | UBLK_PARAM_TYPE_DEVT |
		UBLK_PARAM_TYPE_ZONED | UBLK_PARAM_TYPE_DMA_ALIGN | UBLK_PARAM_TYPE_SEGMENT |
		UBLK_PARAM_TYPE_INTEGRITY;
	params.basic.attrs = 0x0f;
	params.basic.logical_bs_shift = 9;
	params.basic.physical_bs_shift = 12;
	params.basic.io_opt_shift = 16;
	params.basic.io_min_shift = 12;
	params.basic.max_sectors = 0x800;
	params.basic.chunk_sectors = 0x100;
	params.basic.dev_sectors = UINT64_C(0x1122334455667788);
	params.basic.virt_boundary_mask = 0xfff;
	params.discard.discard_alignment = 0x1000;
	params.discard.discard_granularity = 0x2000;
	params.discard.max_discard_sectors = 0x30000;
	params.discard.max_write_zeroes_sectors = 0x40000;
	params.discard.max_discard_segments = 1;
	params.discard.reserved0 = 0x5a5a;
	params.devt.char_major = UINT32_C(0x11223344);
	params.devt.char_minor = UINT32_C(0x55667788);
	params.devt.disk_major = UINT32_C(0x99aabbcc);
	params.devt.disk_minor = UINT32_C(0xddeeff00);
	params.zoned.max_open_zones = 0x10;
	params.zoned.max_active_zones = 0x20;
	params.zoned.max_zone_append_sectors = 0x30;
	for (int i = 0; i < 20; i++)
		params.zoned.reserved[i] = 0x40 + i;
	params.dma.alignment = 0x1ff;
	for (int i = 0; i < 4; i++)
		params.dma.pad[i] = 0xa1 + i;
	params.seg.seg_boundary_mask = UINT64_C(0xffffffff);
	params.seg.max_segment_size = 0x10000;
	params.seg.max_segments = 0x80;
	params.seg.pad[0] = 0xb1;
	params.seg.pad[1] = 0xb2;
	params.integrity.flags = LBMD_PI_CAP_INTEGRITY | LBMD_PI_CAP_REFTAG;
	params.integrity.max_integrity_segments = 0x0102;
	params.integrity.interval_exp = 12;
	params.integrity.metadata_size = 8;
	params.integrity.pi_offset = 0;
	params.integrity.csum_type = LBMD_PI_CSUM_CRC64_NVME;
	params.integrity.tag_size = 2;
	for (int i = 0; i < 5; i++)
		params.integrity.pad[i] = 0xc1 + i;
	dump("params_all", &params, sizeof(params));
}

static void helpers(void)
{
	struct ublk_auto_buf_reg reg = {
		.index = 0x0102, .flags = 0x03, .reserved0 = 0x04, .reserved1 = 0x05060708,
	};
	printf("helper ublk_auto_buf_reg_to_sqe_addr 0x%x 0x%x 0x%x 0x%x 0x%llx\n",
	       reg.index, reg.flags, reg.reserved0, reg.reserved1,
	       (unsigned long long)ublk_auto_buf_reg_to_sqe_addr(&reg));

	__u64 sqe_addr = UINT64_C(0xfedcba9876543210);
	reg = ublk_sqe_addr_to_auto_buf_reg(sqe_addr);
	printf("helper ublk_sqe_addr_to_auto_buf_reg 0x%llx 0x%x 0x%x 0x%x 0x%x\n",
	       (unsigned long long)sqe_addr, reg.index, reg.flags, reg.reserved0, reg.reserved1);

	printf("helper ublk_shmem_zc_addr 0x%x 0x%x 0x%llx\n", 0xbeef, 0xdeadbeefu,
	       (unsigned long long)ublk_shmem_zc_addr(0xbeef, 0xdeadbeefu));
	__u64 zc = UINT64_C(0xffff123456789abc);
	printf("helper ublk_shmem_zc_index 0x%llx 0x%x\n", (unsigned long long)zc,
	       ublk_shmem_zc_index(zc));
	printf("helper ublk_shmem_zc_offset 0x%llx 0x%x\n", (unsigned long long)zc,
	       ublk_shmem_zc_offset(zc));

	struct ublksrv_io_desc iod = { .op_flags = UINT32_C(0xfedcba98) };
	printf("helper ublksrv_get_op_flags 0x%x 0x%x 0x%x\n", iod.op_flags,
	       ublksrv_get_op(&iod), ublksrv_get_flags(&iod));

	/* pread/pwrite position, composed from the header's bit layout */
	__u64 qid = 0xabc, tag = 0xfff, off = 0x1234567;
	__u64 pos = UBLKSRV_IO_BUF_OFFSET + (qid << UBLK_QID_OFF) + (tag << UBLK_TAG_OFF) + off;
	printf("helper user_copy_offset 0x%llx 0x%llx 0x%llx 0x%llx 0x%llx\n",
	       (unsigned long long)qid, (unsigned long long)tag, (unsigned long long)off,
	       (unsigned long long)pos, (unsigned long long)(pos | UBLKSRV_IO_INTEGRITY_FLAG));
}

int main(void)
{
	constants();
	layouts();
	structs();
	helpers();
	return 0;
}
