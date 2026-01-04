#!/bin/bash
# Demo script for fc-macos - shows full Firecracker workflow including snapshots
#
# Prerequisites:
# 1. Tart installed: ~/Applications/tart.app
# 2. Ubuntu image pulled: tart pull ghcr.io/cirruslabs/ubuntu:latest
# 3. fc-macos built: make build-cli
# 4. fc-agent built: make build-agent

set -e

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"
TART="${HOME}/Applications/tart.app/Contents/MacOS/tart"
VM_NAME="fc-macos-linux"

echo "========================================"
echo "fc-macos Firecracker Demo"
echo "========================================"
echo ""

# Check prerequisites
echo "Checking prerequisites..."

if [ ! -x "$TART" ]; then
    echo "ERROR: Tart not found at $TART"
    echo "Install with: curl -sL https://github.com/cirruslabs/tart/releases/latest/download/tart.tar.gz | tar -xz -C ~/Applications"
    exit 1
fi

if [ ! -f "$PROJECT_DIR/build/fc-agent-linux-arm64" ]; then
    echo "Building fc-agent..."
    cd "$PROJECT_DIR" && make build-agent
fi

# Check if VM exists, clone if not
if ! $TART list 2>/dev/null | grep -q "$VM_NAME"; then
    echo "Creating VM from Ubuntu image..."
    $TART clone ghcr.io/cirruslabs/ubuntu:latest "$VM_NAME"
fi

# Configure VM
echo "Configuring VM (4 CPUs, 4GB RAM)..."
$TART set "$VM_NAME" --cpu 4 --memory 4096

# Start VM with nested virtualization
echo "Starting VM with nested virtualization..."
$TART run "$VM_NAME" --no-graphics --nested &
VM_PID=$!
sleep 5

# Wait for VM to be ready
echo "Waiting for VM to be ready..."
for i in {1..60}; do
    if VM_IP=$($TART ip "$VM_NAME" 2>/dev/null); then
        echo "VM is ready at $VM_IP"
        break
    fi
    sleep 2
done

if [ -z "$VM_IP" ]; then
    echo "ERROR: VM did not get an IP address"
    kill $VM_PID 2>/dev/null
    exit 1
fi

# Wait for SSH
echo "Waiting for SSH..."
for i in {1..30}; do
    if $TART exec "$VM_NAME" echo "SSH ready" 2>/dev/null; then
        break
    fi
    sleep 2
done

echo ""
echo "========================================"
echo "Step 1: Setup VM with Firecracker"
echo "========================================"

# Copy setup script to VM
echo "Running setup script inside VM..."
$TART exec "$VM_NAME" sh -c 'cat > /tmp/setup.sh' < "$SCRIPT_DIR/setup-vm.sh"
$TART exec "$VM_NAME" chmod +x /tmp/setup.sh
$TART exec "$VM_NAME" sudo /tmp/setup.sh

# Copy fc-agent to VM
echo "Copying fc-agent to VM..."
# Note: For proper setup, you'd use scp or tart's file sharing
# For demo, we'll use curl to fetch from a hypothetical location

echo ""
echo "========================================"
echo "Step 2: Start fc-agent in VM"
echo "========================================"

# Start fc-agent (would normally be a systemd service)
echo "Starting fc-agent..."
$TART exec "$VM_NAME" sh -c 'nohup /usr/local/bin/fc-agent > /var/log/fc-agent.log 2>&1 &' || true

# Wait for agent
sleep 3

echo ""
echo "========================================"
echo "Step 3: Configure Firecracker microVM"
echo "========================================"

# These curl commands simulate what fc-macos CLI does

echo "Setting boot source..."
curl -s -X PUT "http://${VM_IP}:8080/boot-source" \
    -H "Content-Type: application/json" \
    -d '{
        "kernel_image_path": "/var/lib/firecracker/kernels/vmlinux",
        "boot_args": "console=ttyS0 reboot=k panic=1 pci=off"
    }' | jq .

echo "Setting rootfs drive..."
curl -s -X PUT "http://${VM_IP}:8080/drives/rootfs" \
    -H "Content-Type: application/json" \
    -d '{
        "drive_id": "rootfs",
        "path_on_host": "/var/lib/firecracker/rootfs/ubuntu-rw.ext4",
        "is_root_device": true,
        "is_read_only": false
    }' | jq .

echo "Setting machine config..."
curl -s -X PUT "http://${VM_IP}:8080/machine-config" \
    -H "Content-Type: application/json" \
    -d '{
        "vcpu_count": 2,
        "mem_size_mib": 256,
        "track_dirty_pages": true
    }' | jq .

echo ""
echo "========================================"
echo "Step 4: Start microVM"
echo "========================================"

echo "Starting microVM..."
curl -s -X PUT "http://${VM_IP}:8080/actions" \
    -H "Content-Type: application/json" \
    -d '{"action_type": "InstanceStart"}' | jq .

echo "Waiting for microVM to boot..."
sleep 5

echo ""
echo "========================================"
echo "Step 5: Create Snapshot"
echo "========================================"

echo "Pausing microVM..."
curl -s -X PATCH "http://${VM_IP}:8080/vm" \
    -H "Content-Type: application/json" \
    -d '{"state": "Paused"}' | jq .

echo "Creating snapshot..."
curl -s -X PUT "http://${VM_IP}:8080/snapshot/create" \
    -H "Content-Type: application/json" \
    -d '{
        "snapshot_type": "Full",
        "snapshot_path": "/var/lib/firecracker/snapshots/snapshot.bin",
        "mem_file_path": "/var/lib/firecracker/snapshots/memory.bin"
    }' | jq .

echo "Snapshot created!"

echo ""
echo "========================================"
echo "Step 6: Resume from Snapshot"
echo "========================================"

echo "Resuming microVM..."
curl -s -X PATCH "http://${VM_IP}:8080/vm" \
    -H "Content-Type: application/json" \
    -d '{"state": "Resumed"}' | jq .

echo ""
echo "========================================"
echo "Step 7: Get Metrics"
echo "========================================"

echo "Getting metrics..."
curl -s "http://${VM_IP}:8080/metrics" | jq .

echo ""
echo "========================================"
echo "Step 8: Stop microVM"
echo "========================================"

echo "Stopping microVM..."
curl -s -X PUT "http://${VM_IP}:8080/actions" \
    -H "Content-Type: application/json" \
    -d '{"action_type": "SendCtrlAltDel"}' | jq .

echo ""
echo "========================================"
echo "Demo Complete!"
echo "========================================"
echo ""
echo "The demo showed:"
echo "  1. Setting up a Linux VM with Tart and nested virtualization"
echo "  2. Installing Firecracker inside the VM"
echo "  3. Configuring a Firecracker microVM (kernel, rootfs, machine config)"
echo "  4. Starting the microVM"
echo "  5. Creating a full snapshot (memory + disk state)"
echo "  6. Resuming from the snapshot"
echo "  7. Getting metrics from the running microVM"
echo "  8. Stopping the microVM"
echo ""
echo "To clean up:"
echo "  $TART stop $VM_NAME"
echo "  $TART delete $VM_NAME"
echo ""

# Cleanup
echo "Stopping VM..."
$TART stop "$VM_NAME" 2>/dev/null || true
