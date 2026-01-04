# fc-macos

A CLI tool that runs Firecracker microVMs on macOS using nested virtualization.

## What We've Built

This project enables running Firecracker microVMs on Apple Silicon Macs (M3+) through nested virtualization. The key achievement is a complete CLI that:

- Sets up a Linux VM with KVM support via Tart
- Installs and manages Firecracker inside the Linux VM
- Provides interactive shell access to both the Linux VM and the microVM
- Exposes the full Firecracker API through CLI commands

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
- Tart installed (see below)

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

Or run with your custom rootfs and boot args:

```bash
./build/fc-macos run \
    --rootfs /var/lib/firecracker/rootfs/alpine-shell.ext4 \
    --boot-args "console=ttyS0 reboot=k panic=1 pci=off init=/init"
```

This will:
- Connect to the fc-agent in the Linux VM
- Configure boot source, rootfs, and machine settings
- Start the microVM
- Attach to the serial console

Press `Ctrl+]` to disconnect from the console.

### 6. Run in Background

To run the microVM in background mode:

```bash
# Start in background
./build/fc-macos run --background

# Check status
./build/fc-macos microvm status

# Connect to shell
./build/fc-macos microvm shell

# View logs
./build/fc-macos microvm logs

# Stop the microVM
./build/fc-macos microvm stop
```

## CLI Commands

### Setup and Run

| Command | Description |
|---------|-------------|
| `fc-macos setup` | Set up Linux VM with Firecracker |
| `fc-macos setup --force` | Force re-setup (recreates VM) |
| `fc-macos run` | Start microVM with interactive console |
| `fc-macos run --background` | Start microVM in background |
| `fc-macos run --vcpus 4 --memory 512` | Custom vCPUs and memory |
| `fc-macos run --rootfs PATH --boot-args "..."` | Custom rootfs and boot args |

### MicroVM Management

| Command | Description |
|---------|-------------|
| `fc-macos microvm status` | Check microVM and agent status |
| `fc-macos microvm shell` | Open interactive shell to microVM |
| `fc-macos microvm logs` | View fc-agent logs |
| `fc-macos microvm logs -f` | Follow fc-agent logs |
| `fc-macos microvm stop` | Gracefully stop the microVM |
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

## Verifying You're Inside Firecracker

Once connected to the microVM shell, you can verify you're inside Firecracker:

```bash
# Check hostname (set to "firecracker")
hostname

# View kernel boot arguments
cat /proc/cmdline

# Check available memory (matches --memory flag)
free -m

# View kernel info
uname -a

# Check CPU info (matches --vcpus flag)
cat /proc/cpuinfo | grep processor | wc -l
```

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

## How It Works

### Why Tart?

macOS Tahoe requires a provisioning profile with virtualization capabilities for any binary that uses `Virtualization.framework` directly. Tart is properly signed and notarized by Cirrus Labs, so it works without additional setup.

### fc-agent

The `fc-agent` runs inside the Linux VM and:
- Listens on port 8080 for HTTP requests
- Manages the Firecracker process lifecycle
- Proxies Firecracker API requests to the Unix socket
- Provides console streaming via HTTP connection hijacking

### Console Access

The microVM shell works through:
1. Firecracker's serial console (connected to stdin/stdout)
2. fc-agent streams console I/O over HTTP
3. fc-macos CLI connects and puts terminal in raw mode

## Troubleshooting

### Tart not found

Make sure Tart is installed in one of these locations:
- `~/Applications/tart.app/Contents/MacOS/tart`
- `/Applications/tart.app/Contents/MacOS/tart`
- `/usr/local/bin/tart`

### KVM not available

Ensure you're using:
- M3 chip or later
- macOS 15+ (Sequoia or Tahoe)
- Tart 2.20.0+ with `--nested` flag

### MicroVM not responding

Check fc-agent status:
```bash
./build/fc-macos microvm status
./build/fc-macos microvm logs
```

### Setup fails

Try forcing a fresh setup:
```bash
./build/fc-macos setup --force
```

### Linux VM shell for debugging

Access the Linux VM directly to debug issues:
```bash
./build/fc-macos vm shell

# Inside Linux VM, check:
ls /usr/local/bin/firecracker
ls /var/lib/firecracker/
systemctl status fc-agent
```

## Project Structure

```
fc-macos-nested/
├── cmd/
│   ├── fc-macos/main.go          # CLI entry point
│   └── fc-agent/main.go          # Guest agent entry point
├── internal/
│   ├── cli/                      # Cobra CLI commands
│   │   ├── root.go               # Root command
│   │   ├── setup.go              # Setup command (VM + Firecracker)
│   │   ├── run.go                # Run command (start microVM)
│   │   ├── microvm.go            # MicroVM management
│   │   ├── vm.go                 # Linux VM management
│   │   └── ...                   # Other Firecracker API commands
│   └── agent/                    # Guest agent
│       └── agent.go              # HTTP server + Firecracker proxy
├── test/
│   ├── unit/                     # Unit tests
│   └── e2e/                      # End-to-end tests
├── Makefile
└── README.md
```

## Verified Features

- ✅ Tart 2.30.1+ works on macOS Tahoe
- ✅ Nested virtualization (`--nested` flag)
- ✅ KVM available inside Linux VM (`/dev/kvm`)
- ✅ Firecracker runs inside Linux VM
- ✅ Interactive shell to microVM via serial console
- ✅ Interactive shell to Linux VM via tart exec
- ✅ Full Firecracker API access via CLI
- ✅ 22 unit tests passing
- ✅ 20 E2E tests passing

## License

MIT
