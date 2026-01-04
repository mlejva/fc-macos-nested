A CLI tool that runs Firecracker microVMs on macOS using nested virtualization.

## What is this

This project enables running Firecracker microVMs on Apple Silicon Macs (M3+) through nested virtualization. The key achievement is a complete CLI that:

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                    macOS Host (M3+)                         │
├─────────────────────────────────────────────────────────────┤
│  fc-macos CLI (Go + Cobra)                                  │
│  - Wraps Firecracker API                                    │
│  - Manages Linux VM lifecycle via Tart                      │
│  - Proxies API calls via HTTP                               │
├─────────────────────────────────────────────────────────────┤
│                 Tart (VM Management)                        │
├─────────────────────────────────────────────────────────────┤
│  Linux VM (Ubuntu ARM64 + KVM)                              │
│  - Nested virtualization enabled                            │
│  - fc-agent daemon (HTTP → Firecracker proxy)               │
│  - Firecracker binary + kernels + rootfs images             │
├─────────────────────────────────────────────────────────────┤
│                    KVM / Firecracker                        │
├─────────────────────────────────────────────────────────────┤
│  Firecracker microVM                                        │
│  - Guest kernel + rootfs                                    │
│  - Interactive shell via serial console                     │
└─────────────────────────────────────────────────────────────┘
```

## Requirements

- Apple Silicon M3 or later (nested virtualization support)
- macOS 15.0 (Sequoia) or later
- [Tart](https://tart.run/) installed (see below)

## Quick Start

### 1. Install Tart

```bash
# Download latest release
curl -sL "https://github.com/cirruslabs/tart/releases/latest/download/tart.tar.gz" -o /tmp/tart.tar.gz

# Extract to Applications
cd /tmp && tar -xzf tart.tar.gz
mv tart.app ~/Applications/

# Verify installation
~/Applications/tart.app/Contents/MacOS/tart --version
```

### 2. Build fc-macos

```bash
# Build everything (CLI + agent)
make build
```

### 3. Set Up the Environment

This creates a Linux VM with Firecracker, fc-agent, kernel, and rootfs:

```bash
./build/fc-macos setup
```

The setup process:
- Clones an Ubuntu VM image with nested virtualization
- Downloads and installs Firecracker ARM64 binary
- Builds and installs the fc-agent
- Downloads a Linux kernel for microVMs
- Creates an Alpine rootfs with interactive shell

### 4. Create a Shell Rootfs

The default Ubuntu rootfs doesn't have a working serial console. Create a minimal Alpine rootfs with an interactive shell:

```bash
# SSH into the Linux VM
./build/fc-macos vm shell

# Inside the Linux VM, run these commands:

# Create directories
sudo mkdir -p /var/lib/firecracker/rootfs

# Create a 50MB ext4 image
sudo dd if=/dev/zero of=/var/lib/firecracker/rootfs/alpine-shell.ext4 bs=1M count=50
sudo mkfs.ext4 /var/lib/firecracker/rootfs/alpine-shell.ext4

# Mount and populate
sudo mkdir -p /mnt/rootfs
sudo mount /var/lib/firecracker/rootfs/alpine-shell.ext4 /mnt/rootfs

# Download Alpine minirootfs
curl -L https://dl-cdn.alpinelinux.org/alpine/v3.19/releases/aarch64/alpine-minirootfs-3.19.0-aarch64.tar.gz | sudo tar xz -C /mnt/rootfs

# Create the init script (this is the key part!)
sudo tee /mnt/rootfs/init << 'EOF'
#!/bin/sh
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev
hostname firecracker
echo "Welcome to Firecracker microVM!"
exec /sbin/getty -n -l /bin/sh 115200 ttyS0 vt100
EOF
sudo chmod +x /mnt/rootfs/init

# Unmount
sudo umount /mnt/rootfs

# Exit the Linux VM
exit
```

### 5. Run a MicroVM

Start a Firecracker microVM with an interactive shell:

```bash
./build/fc-macos run
```

Or run with a custom name and configuration:

```bash
./build/fc-macos run --name web-server --vcpus 2 --memory 512
```

This will:
- Connect to the fc-agent in the Linux VM
- Configure boot source, rootfs, and machine settings
- Start the microVM
- Attach to the serial console

Press `Ctrl+]` to disconnect from the console.

### 6. Run Multiple MicroVMs

fc-macos supports running multiple microVMs simultaneously:

```bash
# Start multiple microVMs with custom names
./build/fc-macos run --name web-server --background
./build/fc-macos run --name database --background
./build/fc-macos run --name worker-1 --background

# List all running microVMs
./build/fc-macos microvm list

# Check status of a specific microVM
./build/fc-macos microvm status --name web-server

# Connect to a specific microVM's shell
./build/fc-macos microvm shell --name database

# Stop a specific microVM
./build/fc-macos microvm stop --name worker-1

# Stop all microVMs
./build/fc-macos microvm stop --all
```

If `--name` is not provided, a name is auto-generated (e.g., `microvm-1`).

## CLI Commands

### Setup and Run

| Command | Description |
|---------|-------------|
| `fc-macos setup` | Set up Linux VM with Firecracker |
| `fc-macos setup --cpus 8 --memory 8192` | Custom CPUs and memory for Linux VM |
| `fc-macos setup --force` | Force re-setup (recreates VM) |
| `fc-macos run` | Start microVM with interactive console |
| `fc-macos run --name NAME` | Start microVM with custom name |
| `fc-macos run --background` | Start microVM in background |
| `fc-macos run --vcpus 4 --memory 512` | Custom vCPUs and memory |
| `fc-macos run --rootfs PATH --boot-args "..."` | Custom rootfs and boot args |

### Dashboard

| Command | Description |
|---------|-------------|
| `fc-macos dashboard` | Live dashboard showing VM and microVM status |

**Live monitoring with keyboard controls:**

```
FC-MACOS   FIRECRACKER ON MACOS

╭──────────────────────────────────────╮  ╭──────────────────────────────────────╮
│ LINUX VM                             │  │ FC-AGENT                             │
│                                      │  │                                      │
│   ✓  RUNNING                         │  │   ✓  ONLINE                          │
│                                      │  │                                      │
│   NAME  fc-macos-linux               │  │   ✓  FIRECRACKER                     │
│   IP    192.168.64.5                 │  │   VMs  3 running / 3 total           │
│                                      │  │                                      │
│   MEM  ████░░░░░░░░░░  320M / 3902M  │  │                                      │
│   CPU  █████░░░░░░░░░  0.5 / 4       │  │                                      │
╰──────────────────────────────────────╯  ╰──────────────────────────────────────╯

╭────────────────────────────────────────────────────────────────────────────────╮
│ MICROVMS  (3/3 running)                                                        │
│                                                                                │
│       NAME         STATUS     VCPUS  MEMORY                                    │
│   ────────────────────────────────────────────                                 │
│ > ▾ ● web-server   running    2      512                                       │
│       PID: 1234                                                                │
│       ID:  vm-1735847123-1                                                     │
│       CPU: 2.3%    RAM: 128 MB / 512 MB                                        │
│                                                                                │
│   ▸ ● database     running    1      256                                       │
│   ▸ ● worker-1     running    1      128                                       │
╰────────────────────────────────────────────────────────────────────────────────╯

18:13:24  │  j/k nav  ↵ details  r refresh  s stop vm  S stop linux  q quit
```

**Keyboard shortcuts:**
- `j`/`k` or `↓`/`↑` - Navigate microVM list
- `Enter`/`Space` - Toggle details (PID, ID, CPU, RAM usage)
- `r` - Refresh status
- `s` - Stop selected microVM
- `S` - Stop Linux VM
- `q` - Quit dashboard

### MicroVM Management

| Command | Description |
|---------|-------------|
| `fc-macos microvm list` | List all running microVMs |
| `fc-macos microvm status` | Check overall microVM and agent status |
| `fc-macos microvm status --name NAME` | Check specific microVM status |
| `fc-macos microvm shell --name NAME` | Open interactive shell to microVM |
| `fc-macos microvm logs` | View fc-agent logs |
| `fc-macos microvm logs -f` | Follow fc-agent logs |
| `fc-macos microvm stop --name NAME` | Gracefully stop specific microVM |
| `fc-macos microvm stop --all` | Stop all microVMs |
| `fc-macos microvm stop --force` | Force stop the microVM |

### Linux VM Management

| Command | Description |
|---------|-------------|
| `fc-macos vm status` | Check Linux VM status |
| `fc-macos vm shell` | Open shell to Linux VM |
| `fc-macos vm logs` | View Linux VM logs |
| `fc-macos vm start` | Start the Linux VM |
| `fc-macos vm stop` | Stop the Linux VM |

### Firecracker API Commands

| Command | Description |
|---------|-------------|
| `fc-macos boot set --kernel PATH` | Set kernel and boot args |
| `fc-macos boot get` | Get boot configuration |
| `fc-macos drives add --id ID --path PATH` | Add block device |
| `fc-macos drives list` | List drives |
| `fc-macos network add --id ID --tap TAP` | Add network interface |
| `fc-macos machine config --vcpus N --memory M` | Configure machine |
| `fc-macos actions start` | Start the microVM |
| `fc-macos actions stop` | Stop the microVM |
| `fc-macos snapshots create --path PATH` | Create snapshot |
| `fc-macos snapshots load --path PATH` | Load snapshot |
| `fc-macos metrics get` | Get metrics |
| `fc-macos balloon set --amount MiB` | Set balloon target |

## Testing

### Run Unit Tests

```bash
make test
```

### Run E2E Tests

E2E tests require the binary to be built and Tart to be installed:

```bash
# Build first
make build

# Run E2E tests
make test-e2e
```

### Run Full Workflow Test

The full workflow test starts a microVM and verifies it boots correctly:

```bash
FC_E2E_FULL=1 go test -v -tags=e2e ./test/e2e/... -run TestFullWorkflow
```
