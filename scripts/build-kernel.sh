#!/bin/bash
# Build a minimal Linux kernel with KVM support for ARM64
# This script requires Docker and cross-compilation tools

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
OUTPUT_DIR="${PROJECT_DIR}/assets/kernel"
CONFIG_FILE="${PROJECT_DIR}/assets/configs/kernel-config-arm64"

KERNEL_VERSION="${KERNEL_VERSION:-6.6.10}"
KERNEL_URL="https://cdn.kernel.org/pub/linux/kernel/v6.x/linux-${KERNEL_VERSION}.tar.xz"

echo "=== Building Linux Kernel ${KERNEL_VERSION} for ARM64 with KVM support ==="

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Build in Docker to ensure consistent environment
docker run --rm -v "${PROJECT_DIR}:/work" -w /work ubuntu:22.04 bash -c "
    set -e

    # Install build dependencies
    apt-get update
    apt-get install -y --no-install-recommends \
        build-essential \
        bc \
        bison \
        flex \
        libelf-dev \
        libssl-dev \
        curl \
        xz-utils \
        gcc-aarch64-linux-gnu

    # Download kernel source if not exists
    if [ ! -d 'linux-${KERNEL_VERSION}' ]; then
        echo 'Downloading kernel source...'
        curl -L '${KERNEL_URL}' | tar xJ
    fi

    cd linux-${KERNEL_VERSION}

    # Use custom config if exists, otherwise generate minimal config
    if [ -f '/work/assets/configs/kernel-config-arm64' ]; then
        cp /work/assets/configs/kernel-config-arm64 .config
    else
        # Generate minimal config for ARM64 with KVM
        make ARCH=arm64 CROSS_COMPILE=aarch64-linux-gnu- defconfig

        # Enable required options
        scripts/config --enable CONFIG_VIRTUALIZATION
        scripts/config --enable CONFIG_KVM
        scripts/config --enable CONFIG_VIRTIO
        scripts/config --enable CONFIG_VIRTIO_PCI
        scripts/config --enable CONFIG_VIRTIO_MMIO
        scripts/config --enable CONFIG_VIRTIO_BLK
        scripts/config --enable CONFIG_VIRTIO_NET
        scripts/config --enable CONFIG_VIRTIO_CONSOLE
        scripts/config --enable CONFIG_VIRTIO_VSOCK
        scripts/config --enable CONFIG_VHOST_VSOCK
        scripts/config --enable CONFIG_EXT4_FS
        scripts/config --enable CONFIG_TMPFS
        scripts/config --enable CONFIG_DEVTMPFS
        scripts/config --enable CONFIG_DEVTMPFS_MOUNT
        scripts/config --enable CONFIG_TUN
        scripts/config --enable CONFIG_TAP
        scripts/config --enable CONFIG_VETH
        scripts/config --enable CONFIG_BRIDGE
        scripts/config --enable CONFIG_NETFILTER
        scripts/config --enable CONFIG_IP_NF_NAT
        scripts/config --enable CONFIG_NET_SCH_FQ_CODEL
    fi

    # Update config
    make ARCH=arm64 CROSS_COMPILE=aarch64-linux-gnu- olddefconfig

    # Build kernel (uncompressed Image for Virtualization.framework)
    echo 'Building kernel...'
    make ARCH=arm64 CROSS_COMPILE=aarch64-linux-gnu- -j\$(nproc) Image

    # Copy output
    cp arch/arm64/boot/Image /work/assets/kernel/vmlinux-${KERNEL_VERSION}
    echo 'Kernel built successfully!'
"

echo ""
echo "=== Kernel Build Complete ==="
echo "Kernel image: ${OUTPUT_DIR}/vmlinux-${KERNEL_VERSION}"
ls -la "${OUTPUT_DIR}/vmlinux-${KERNEL_VERSION}" 2>/dev/null || echo "(File will be created when build completes)"
