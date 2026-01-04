#!/bin/bash
# Setup script for the fc-macos Linux VM
# This script installs Firecracker and fc-agent inside the Tart VM

set -e

FC_VERSION="${FC_VERSION:-1.10.1}"
ARCH="aarch64"

echo "=== Setting up fc-macos Linux VM ==="

# Update system
echo "Updating system packages..."
sudo apt-get update -qq
sudo apt-get install -y curl jq

# Create directories
echo "Creating directories..."
sudo mkdir -p /usr/local/bin
sudo mkdir -p /var/lib/firecracker/{kernels,rootfs,snapshots}
sudo mkdir -p /etc/fc-agent

# Download Firecracker
echo "Downloading Firecracker ${FC_VERSION}..."
FC_URL="https://github.com/firecracker-microvm/firecracker/releases/download/v${FC_VERSION}/firecracker-v${FC_VERSION}-${ARCH}.tgz"
curl -sL "$FC_URL" -o /tmp/firecracker.tgz
tar -xzf /tmp/firecracker.tgz -C /tmp

# Install Firecracker
echo "Installing Firecracker..."
sudo mv /tmp/release-v${FC_VERSION}-${ARCH}/firecracker-v${FC_VERSION}-${ARCH} /usr/local/bin/firecracker
sudo chmod +x /usr/local/bin/firecracker

# Verify installation
echo "Verifying Firecracker installation..."
/usr/local/bin/firecracker --version

# Check KVM access
echo "Checking KVM access..."
if [ -c /dev/kvm ]; then
    echo "KVM is available"
    ls -la /dev/kvm
    # Ensure current user can access KVM
    sudo chmod 666 /dev/kvm
else
    echo "WARNING: KVM device not found. Firecracker will not work!"
    exit 1
fi

# Download sample kernel and rootfs
echo "Downloading sample kernel..."
KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.10/${ARCH}/vmlinux-6.1.102"
sudo curl -sL "$KERNEL_URL" -o /var/lib/firecracker/kernels/vmlinux

echo "Downloading sample rootfs..."
ROOTFS_URL="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/v1.10/${ARCH}/ubuntu-24.04.ext4"
sudo curl -sL "$ROOTFS_URL" -o /var/lib/firecracker/rootfs/ubuntu.ext4

# Make rootfs writable (create a copy)
echo "Creating writable rootfs copy..."
sudo cp /var/lib/firecracker/rootfs/ubuntu.ext4 /var/lib/firecracker/rootfs/ubuntu-rw.ext4
sudo chmod 644 /var/lib/firecracker/rootfs/ubuntu-rw.ext4

echo "=== Setup complete ==="
echo ""
echo "Firecracker: /usr/local/bin/firecracker"
echo "Kernel: /var/lib/firecracker/kernels/vmlinux"
echo "Rootfs: /var/lib/firecracker/rootfs/ubuntu-rw.ext4"
echo ""
echo "To test Firecracker manually:"
echo "  sudo /usr/local/bin/firecracker --api-sock /tmp/firecracker.socket"
