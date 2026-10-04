# Toolbox for hosts that lack the matrix's tools (the WSL rig has docker but no
# qemu, cpio or rpm2cpio). CI runners install the same packages natively.
FROM ubuntu:24.04
RUN apt-get update && DEBIAN_FRONTEND=noninteractive apt-get install -y --no-install-recommends \
        qemu-system-x86 qemu-utils cloud-image-utils genisoimage cpio rpm2cpio libarchive-tools \
        zstd xz-utils kmod python3 curl ca-certificates \
    && apt-get clean && find /var/lib/apt/lists -mindepth 1 -delete
