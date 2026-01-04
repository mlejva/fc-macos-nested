#!/bin/bash
# Build an Alpine Linux rootfs with KVM support for running Firecracker
# This script requires Docker

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
OUTPUT_DIR="${PROJECT_DIR}/assets/rootfs"
BUILD_DIR="${PROJECT_DIR}/build"

ALPINE_VERSION="${ALPINE_VERSION:-3.19}"
ALPINE_ARCH="aarch64"
ROOTFS_SIZE="${ROOTFS_SIZE:-512M}"
FIRECRACKER_VERSION="${FIRECRACKER_VERSION:-v1.6.0}"

AGENT_BINARY="${BUILD_DIR}/fc-agent-linux-arm64"

echo "=== Building Alpine Linux ${ALPINE_VERSION} rootfs for ARM64 ==="

# Check if agent binary exists
if [ ! -f "$AGENT_BINARY" ]; then
    echo "Error: Agent binary not found at $AGENT_BINARY"
    echo "Please run 'make build-agent' first"
    exit 1
fi

# Create output directory
mkdir -p "$OUTPUT_DIR"

# Build in Docker to ensure consistent environment
docker run --rm --privileged \
    -v "${PROJECT_DIR}:/work" \
    -w /work \
    alpine:${ALPINE_VERSION} sh -c "
    set -e

    # Install required tools
    apk add --no-cache \
        e2fsprogs \
        curl \
        tar

    ROOTFS_IMG='/work/assets/rootfs/rootfs-alpine-${ALPINE_VERSION}.ext4'
    MOUNT_DIR='/mnt/rootfs'

    # Create empty ext4 image
    echo 'Creating ${ROOTFS_SIZE} ext4 image...'
    truncate -s ${ROOTFS_SIZE} \"\$ROOTFS_IMG\"
    mkfs.ext4 -F \"\$ROOTFS_IMG\"

    # Mount the image
    mkdir -p \"\$MOUNT_DIR\"
    mount -o loop \"\$ROOTFS_IMG\" \"\$MOUNT_DIR\"

    # Download and extract Alpine minirootfs
    echo 'Downloading Alpine minirootfs...'
    ALPINE_URL=\"https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VERSION}/releases/${ALPINE_ARCH}/alpine-minirootfs-${ALPINE_VERSION}.0-${ALPINE_ARCH}.tar.gz\"
    curl -L \"\$ALPINE_URL\" | tar xz -C \"\$MOUNT_DIR\"

    # Configure repositories
    echo 'https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VERSION}/main' > \"\$MOUNT_DIR/etc/apk/repositories\"
    echo 'https://dl-cdn.alpinelinux.org/alpine/v${ALPINE_VERSION}/community' >> \"\$MOUNT_DIR/etc/apk/repositories\"

    # Copy resolv.conf for networking in chroot
    cp /etc/resolv.conf \"\$MOUNT_DIR/etc/resolv.conf\"

    # Install packages
    echo 'Installing packages...'
    chroot \"\$MOUNT_DIR\" apk update
    chroot \"\$MOUNT_DIR\" apk add --no-cache \
        openrc \
        busybox-initscripts \
        util-linux \
        e2fsprogs \
        iptables \
        iproute2 \
        dhcpcd \
        openssh \
        curl \
        bash \
        ca-certificates

    # Download Firecracker binary
    echo 'Downloading Firecracker ${FIRECRACKER_VERSION}...'
    FC_URL=\"https://github.com/firecracker-microvm/firecracker/releases/download/${FIRECRACKER_VERSION}/firecracker-${FIRECRACKER_VERSION}-${ALPINE_ARCH}.tgz\"
    curl -L \"\$FC_URL\" | tar xz -C /tmp
    cp /tmp/release-${FIRECRACKER_VERSION}-${ALPINE_ARCH}/firecracker-${FIRECRACKER_VERSION}-${ALPINE_ARCH} \"\$MOUNT_DIR/usr/local/bin/firecracker\"
    chmod +x \"\$MOUNT_DIR/usr/local/bin/firecracker\"

    # Install agent binary
    echo 'Installing fc-agent...'
    cp /work/build/fc-agent-linux-arm64 \"\$MOUNT_DIR/usr/local/bin/fc-agent\"
    chmod +x \"\$MOUNT_DIR/usr/local/bin/fc-agent\"

    # Configure init system
    echo 'Configuring init system...'

    # Create fc-agent service
    cat > \"\$MOUNT_DIR/etc/init.d/fc-agent\" << 'INITEOF'
#!/sbin/openrc-run

name=\"fc-agent\"
description=\"Firecracker Agent\"
command=\"/usr/local/bin/fc-agent\"
command_args=\"--vsock-port 2222\"
command_background=\"yes\"
pidfile=\"/run/\${RC_SVCNAME}.pid\"
output_log=\"/var/log/fc-agent.log\"
error_log=\"/var/log/fc-agent.log\"

depend() {
    need net
    after firewall
}
INITEOF
    chmod +x \"\$MOUNT_DIR/etc/init.d/fc-agent\"

    # Enable services
    chroot \"\$MOUNT_DIR\" rc-update add devfs sysinit
    chroot \"\$MOUNT_DIR\" rc-update add dmesg sysinit
    chroot \"\$MOUNT_DIR\" rc-update add mdev sysinit
    chroot \"\$MOUNT_DIR\" rc-update add hwdrivers sysinit
    chroot \"\$MOUNT_DIR\" rc-update add networking boot
    chroot \"\$MOUNT_DIR\" rc-update add sshd default
    chroot \"\$MOUNT_DIR\" rc-update add fc-agent default

    # Configure console
    echo 'hvc0::respawn:/sbin/getty -L hvc0 115200 vt100' >> \"\$MOUNT_DIR/etc/inittab\"

    # Set root password (for debugging - change in production)
    echo 'root:firecracker' | chroot \"\$MOUNT_DIR\" chpasswd

    # Configure networking
    cat > \"\$MOUNT_DIR/etc/network/interfaces\" << 'NETEOF'
auto lo
iface lo inet loopback

auto eth0
iface eth0 inet dhcp
NETEOF

    # Create mount script for shared directories
    cat > \"\$MOUNT_DIR/etc/init.d/mount-shared\" << 'MOUNTEOF'
#!/sbin/openrc-run

name=\"mount-shared\"
description=\"Mount virtio-fs shared directories\"

start() {
    mkdir -p /shared
    mount -t virtiofs shared /shared 2>/dev/null || true
}
MOUNTEOF
    chmod +x \"\$MOUNT_DIR/etc/init.d/mount-shared\"
    chroot \"\$MOUNT_DIR\" rc-update add mount-shared boot

    # Load KVM module on boot
    echo 'kvm' >> \"\$MOUNT_DIR/etc/modules\"

    # Configure hostname
    echo 'fc-host' > \"\$MOUNT_DIR/etc/hostname\"

    # Create basic fstab
    cat > \"\$MOUNT_DIR/etc/fstab\" << 'FSTABEOF'
/dev/vda    /       ext4    rw,relatime     0 1
devtmpfs    /dev    devtmpfs    rw,relatime     0 0
proc        /proc   proc    rw,nosuid,nodev,noexec  0 0
sysfs       /sys    sysfs   rw,nosuid,nodev,noexec  0 0
tmpfs       /tmp    tmpfs   rw,nosuid,nodev 0 0
FSTABEOF

    # Cleanup
    rm -rf \"\$MOUNT_DIR/var/cache/apk/*\"
    rm -f \"\$MOUNT_DIR/etc/resolv.conf\"

    # Unmount
    umount \"\$MOUNT_DIR\"
    rmdir \"\$MOUNT_DIR\"

    echo 'Rootfs built successfully!'
"

echo ""
echo "=== Rootfs Build Complete ==="
echo "Rootfs image: ${OUTPUT_DIR}/rootfs-alpine-${ALPINE_VERSION}.ext4"
ls -la "${OUTPUT_DIR}/rootfs-alpine-${ALPINE_VERSION}.ext4" 2>/dev/null || echo "(File will be created when build completes)"
