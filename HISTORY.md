# Development History

This document captures the journey of building fc-macos, including what worked, what didn't, and lessons learned.

## Project Goal

Run Firecracker microVMs on macOS using nested virtualization:
```
macOS (M3+) → Virtualization Framework → Linux VM (KVM) → Firecracker → microVM
```

## Timeline of Key Decisions

### Phase 1: Initial Approach with Code-Hex/vz

**Attempted:** Use [Code-Hex/vz](https://github.com/Code-Hex/vz) Go bindings to directly control Apple's Virtualization.framework.

**What Happened:**
- Built working VM configuration code
- Binary compiled successfully
- On execution: `SIGKILL` immediately

**Root Cause:** macOS Tahoe (26.x) requires a provisioning profile with virtualization entitlements for any binary using `Virtualization.framework`. Without it, the kernel kills the process.

**Lesson Learned:** Apple's security model for virtualization changed significantly in macOS Tahoe. Self-signed binaries with entitlements are no longer sufficient.

### Phase 2: Pivot to Tart

**Decision:** Use [Tart](https://github.com/cirruslabs/tart) as the VM management layer instead of direct Virtualization.framework access.

**Why Tart Works:**
- Properly signed and notarized by Cirrus Labs
- Has the required provisioning profile
- Supports nested virtualization (`--nested` flag) since v2.20.0
- Mature CLI with `tart exec` for running commands in VMs

**Trade-offs:**
- External dependency
- Less control over VM configuration
- But: actually works without Apple Developer Program membership

### Phase 3: fc-agent Development

**Challenge:** How to communicate between macOS host and Firecracker inside the Linux VM?

**Solution:** HTTP-based agent (`fc-agent`) running inside the Linux VM:
- Listens on port 8080
- Proxies Firecracker API requests to Unix socket
- Manages Firecracker process lifecycle
- Streams console I/O via HTTP connection hijacking

### Phase 4: Binary Transfer Problems

**Problem:** Transferring `fc-agent` binary to the Linux VM via `tart exec` stdin didn't work reliably.

**Attempted:**
```bash
cat fc-agent | tart exec vm-name "cat > /tmp/fc-agent"
```

**Result:** Binary was 0 bytes or corrupted inside VM.

**Solution:** Start a temporary HTTP server on macOS, use `curl` from inside the VM:
```go
// Start HTTP server serving the binary
listener, _ := net.Listen("tcp", hostIP+":0")
go http.Serve(listener, http.FileServer(...))

// Download from inside VM
tart exec vm-name "curl -o /tmp/fc-agent http://host:port/fc-agent"
```

**Lesson Learned:** `tart exec` stdin piping is unreliable for binary data. HTTP transfer is more robust.

### Phase 5: Firecracker Context Cancellation Bug

**Problem:** Firecracker was being killed after every HTTP request.

**Symptoms:**
- First API request would work
- Subsequent requests failed with "Firecracker not running"
- fc-agent logs showed: "Firecracker exited"

**Root Cause:** Using `exec.CommandContext(ctx, ...)` with the HTTP request context:
```go
// BAD: Request context cancels when response is sent
a.fcProcess = exec.CommandContext(ctx, a.config.FirecrackerBin, ...)
```

When the HTTP response completed, the context was cancelled, which sent SIGKILL to Firecracker.

**Fix:** Use `exec.Command()` without context for long-running processes:
```go
// GOOD: Process lives beyond request lifecycle
a.fcProcess = exec.Command(a.config.FirecrackerBin, ...)
```

**Lesson Learned:** Be careful with context propagation for processes that should outlive HTTP requests.

### Phase 6: MicroVM Rootfs Problems

**Problem:** MicroVM would boot and immediately reboot in a loop.

**Symptoms:**
- Kernel loaded successfully
- Panic or immediate reboot
- No shell prompt

**Root Cause:** Sample Ubuntu rootfs had no init system configured for serial console.

**Solution:** Build custom Alpine rootfs with minimal init:
```bash
#!/bin/sh
# /init script
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev
hostname firecracker
exec /sbin/getty -n -l /bin/sh 115200 ttyS0 vt100
```

**Key Components:**
- Mount essential filesystems (proc, sys, dev)
- Start getty on ttyS0 (serial console)
- Use `-n -l /bin/sh` for auto-login without password

### Phase 7: Console Streaming

**Challenge:** Provide interactive shell access to the microVM from macOS.

**Solution:** HTTP connection hijacking for bidirectional streaming:

1. **fc-agent side:** Hijack HTTP connection, stream Firecracker stdin/stdout
```go
hj, _ := w.(http.Hijacker)
conn, bufrw, _ := hj.Hijack()
// Bidirectional copy between conn and Firecracker pipes
```

2. **CLI side:** Raw terminal mode, watch for Ctrl+]
```go
oldState, _ := term.MakeRaw(int(os.Stdin.Fd()))
defer term.Restore(int(os.Stdin.Fd()), oldState)
// Copy stdin to connection, connection to stdout
```

### Phase 8: Dashboard Development

**Goal:** Create a live terminal dashboard to monitor VM and microVM status, inspired by htop.

**Technology Choice:** [Bubbletea](https://github.com/charmbracelet/bubbletea) + [Lipgloss](https://github.com/charmbracelet/lipgloss) - the Go equivalent of React's Ink for terminal UIs.

**First Attempt - Over-engineered:**
- Per-character gradient animations on every text
- Multiple color-cycling sparkle animations
- Fire gradient title with animated sparkles
- Result: Unreadable mess, too busy, didn't fit in terminal

**What Didn't Work:**
```go
// BAD: Per-character gradients - unreadable
for i, char := range text {
    colorIdx := (i + m.tick) % len(gradient)
    charStyle := lipgloss.NewStyle().Foreground(lipgloss.Color(gradient[colorIdx]))
    result.WriteString(charStyle.Render(string(char)))
}
```

**Lesson Learned:** Just because you CAN animate every character doesn't mean you SHOULD. Readability > visual flair.

**Final Design Principles:**
- Clean, consistent color palette (orange brand, green success, red error, cyan accent)
- Responsive layout (side-by-side on wide terminals, stacked on narrow)
- Simple two-color progress bars
- Only highlight what matters (status indicators)

### Phase 9: Dashboard Auto-Restart Bug

**Problem:** After stopping the microVM via dashboard, it would restart within 2 seconds.

**Symptoms:**
- Press `s` to stop microVM
- Status shows "Stopped" briefly
- 2 seconds later: "Running" again

**Root Cause:** The fc-agent auto-starts Firecracker when it receives ANY API request:
```go
func (a *Agent) handleProxy(w http.ResponseWriter, r *http.Request) {
    if !a.fcStarted {
        a.startFirecracker(r.Context())  // Auto-start on any request!
    }
    // ...
}
```

The dashboard was polling `/machine-config` every 2 seconds to get microVM stats, which triggered auto-start.

**Fix:** Only query Firecracker endpoints if `/agent/status` reports it's already running:
```go
if result.agent.Available && result.agent.FirecrackerRunning {
    result.microVM = m.checkMicroVM(ctx, result.linuxVM.IP)
}
```

**Lesson Learned:** Be aware of side effects when polling status endpoints. A "read-only" status check shouldn't modify system state.

### Phase 10: Real-Time Resource Monitoring

**Challenge:** Show actual CPU and memory usage from the Linux VM, not just allocated resources.

**Approach:** Use `tart exec` to run commands inside the VM:
```go
// Memory usage
memCmd := exec.CommandContext(ctx, tartPath, "exec", vmName, "sh", "-c",
    "free -m | awk '/^Mem:/ {print $2,$3}'")

// CPU load
loadCmd := exec.CommandContext(ctx, tartPath, "exec", vmName, "sh", "-c",
    "cat /proc/loadavg | awk '{print $1}'")
```

**Display:** Progress bars showing usage vs total:
```
CPU  ████░░░░░░░░░░░░░░░ 0.5/4
Mem  ██████████░░░░░░░░░ 1024/4096M
```

### Phase 11: Firecracker Log Spam

**Problem:** MicroVM console was flooded with Firecracker API logs:
```
2026-01-04T16:57:48 [fc_api] The API server received a Get request on "/machine-config".
2026-01-04T16:57:48 [fc_api] The request was executed successfully. Status code: 200 OK.
```

**Cause:** Firecracker logs to stderr at Info level by default, and we were piping stderr to the console.

**Fix:** Add `--level Warning` to suppress info-level logs:
```go
a.fcProcess = exec.Command(a.config.FirecrackerBin,
    "--api-sock", a.config.SocketPath,
    "--level", "Warning",
)
```

### Phase 12: Configuration Flexibility

**Added CLI flags for resource configuration:**

Linux VM (setup):
```bash
fc-macos setup --cpus 8 --memory 8192
```

MicroVM (run):
```bash
fc-macos run --vcpus 2 --memory 512 --rootfs /path/to/rootfs.ext4
```

**Lesson Learned:** Hardcoded values are fine for prototyping, but users need control over resources for real workloads.

### Phase 13: Multi-MicroVM Support

**Goal:** Run multiple Firecracker microVMs simultaneously with independent lifecycle management.

**Architecture Changes:**

1. **fc-agent refactor** - Changed from single `fcProcess` to `map[string]*MicroVM`:
```go
type Agent struct {
    microVMs  map[string]*MicroVM  // Keyed by ID
    vmMu      sync.RWMutex
    idCounter uint64
}

type MicroVM struct {
    ID         string
    Name       string
    SocketPath string  // /tmp/firecracker-{id}.socket
    fcProcess  *exec.Cmd
    proxy      *httputil.ReverseProxy
    // ...
}
```

2. **New API endpoints:**
   - `GET /agent/microvms` - List all microVMs
   - `POST /agent/microvms` - Create new microVM
   - `GET /agent/microvms/{id}` - Get specific microVM
   - `DELETE /agent/microvms/{id}` - Stop specific microVM
   - `GET /agent/microvms/{id}/console` - Console for specific microVM

3. **Per-VM socket paths:** Each microVM gets `/tmp/firecracker-{id}.socket` to avoid conflicts.

4. **Backward compatibility:** Legacy single-VM endpoints (`/agent/start`, `/agent/stop`, `/console`) continue to work.

**CLI Changes:**
- `fc-macos run --name <name>` - Optional name flag (auto-generates if not provided)
- `fc-macos microvm list` - New command to list all microVMs
- `fc-macos microvm stop --name <name>` - Stop specific VM
- `fc-macos microvm stop --all` - Stop all VMs

### Phase 14: Dashboard Multi-VM List with Navigation

**Challenge:** Display multiple microVMs in the dashboard with vim-like navigation.

**Implementation:**
```go
type dashboardModel struct {
    microVMs    []microVMStatus
    selectedIdx int             // Currently selected
    listOffset  int             // Scroll offset
    maxVisible  int             // Max visible (5)
    expandedVMs map[string]bool // Track expanded details
}
```

**Key bindings added:**
- `j`/`k` or `↓`/`↑` - Navigate list
- `Enter`/`Space` - Toggle details expansion
- `s` - Stop selected microVM

**List ordering issue:** MicroVMs appeared in random order after each refresh because Go maps don't guarantee iteration order.

**Fix:** Sort by name before rendering:
```go
sort.Slice(vms, func(i, j int) bool {
    return vms[i].Name < vms[j].Name
})
```

### Phase 15: Real-Time Resource Monitoring for MicroVMs

**Challenge:** Show CPU and RAM usage for each running microVM process.

**First Attempt - Reading /proc files:**
```go
// Tried reading /proc/{pid}/stat and /proc/{pid}/status
// Problem: Complex parsing, required calculating deltas for CPU
```

**What Didn't Work:** Reading `/proc` files directly required:
- Parsing complex `/proc/stat` format
- Tracking previous values to calculate CPU percentage
- Multiple file reads per process

**Final Solution - Using `ps` command:**
```go
func getProcessStats(pid int) (cpuPercent float64, memoryMB int) {
    cmd := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "%cpu=,rss=")
    output, err := cmd.Output()
    if err != nil {
        return 0, 0
    }
    fields := strings.Fields(string(output))
    if len(fields) >= 2 {
        cpuPercent, _ = strconv.ParseFloat(fields[0], 64)
        if rssKB, err := strconv.Atoi(fields[1]); err == nil {
            memoryMB = rssKB / 1024
        }
    }
    return cpuPercent, memoryMB
}
```

**Why `ps` works better:**
- Single command gives both CPU% and RSS
- OS handles the CPU calculation
- No state tracking needed
- Works consistently across Linux distros

**Lesson Learned:** Sometimes shelling out to a standard tool is simpler and more reliable than reimplementing its logic.

### Phase 16: fc-agent Deployment Challenges

**Problem:** Deploying updated fc-agent binary to the running Linux VM.

**Approach 1 - Stdin piping (failed again):**
```bash
cat fc-agent | tart exec vm "cat > /tmp/fc-agent"
# Result: 0-byte file
```

**Approach 2 - HTTP transfer (worked):**
```bash
# On macOS: Start HTTP server
python3 -m http.server 8000

# In VM: Download binary
curl -o /tmp/fc-agent http://host-ip:8000/fc-agent
```

**systemd service for persistence:**
```ini
[Unit]
Description=Firecracker Agent
After=network.target

[Service]
Type=simple
ExecStart=/usr/local/bin/fc-agent
Restart=always
```

**Lesson Learned:** For iterative development, having fc-agent as a systemd service makes deployment much easier - just replace the binary and restart the service.

## What Worked Well

1. **Tart as VM layer** - Stable, well-maintained, handles all the signing complexity
2. **HTTP-based agent** - Simple, debuggable, works with standard tools
3. **Connection hijacking for console** - Clean way to do bidirectional streaming over HTTP
4. **Alpine for rootfs** - Minimal, fast to build, easy to customize
5. **Comprehensive E2E tests** - Caught many issues before manual testing
6. **Map-based VM registry** - Clean way to manage multiple VMs with unique IDs
7. **Per-VM Unix sockets** - Avoids socket conflicts between Firecracker instances
8. **`ps` command for resource stats** - Simple, reliable, no state tracking needed
9. **Stable list ordering via sort** - Prevents UI flickering from map iteration randomness
10. **Expandable details in TUI** - Shows details on demand without cluttering the default view

## What Didn't Work

1. **Code-Hex/vz direct approach** - Killed by macOS security requirements
2. **Stdin piping via tart exec** - Unreliable for binary data (consistently produces 0-byte files)
3. **Context-based process management** - Killed Firecracker prematurely
4. **Ubuntu sample rootfs** - No proper init for serial console
5. **Serial socket approach** - More complex than stdin/stdout pipes
6. **Over-designed dashboard** - Per-character gradients, animations everywhere = unreadable
7. **Dark color palette** - Colors that look good in design tools may be unreadable on actual terminals
8. **Auto-start on status check** - Polling endpoints shouldn't have side effects
9. **Reading /proc files for CPU stats** - Complex parsing, required delta calculations, error-prone
10. **Unsorted map iteration for UI lists** - Causes flickering/reordering on every refresh
11. **Column alignment with variable-width data** - Required careful format string tuning

## Key Insights

### macOS Virtualization Security
Apple has significantly tightened security around virtualization in recent macOS versions. Using third-party signed tools (Tart) is often easier than dealing with provisioning profiles.

### Process Lifecycle in HTTP Servers
Long-running processes started from HTTP handlers need careful context management. The request context is not appropriate for processes that should outlive the request.

### Binary Transfer in VMs
When transferring binaries to VMs, HTTP is more reliable than stdin piping. The extra complexity of running a temporary server is worth the reliability.

### Minimal Init Systems
For microVMs, a simple shell script as `/init` is often sufficient. Full init systems (systemd, OpenRC) add unnecessary complexity for development/testing.

### Testing Strategy
E2E tests that actually boot VMs catch issues that unit tests cannot. The investment in E2E infrastructure paid off significantly.

### Terminal UI Design
Less is more. A clean dashboard with 4 colors is infinitely more usable than a rainbow explosion with animations. Key principles:
- Use bright colors on dark terminals (#b0b0b0 not #6c757d for text)
- Responsive layouts that adapt to terminal width
- Status indicators should be glanceable (green dot = good, red dot = bad)
- Reserve animations for loading states, not decoration

### Side Effects in Read Operations
Status checking endpoints should be truly read-only. The fc-agent's auto-start-on-any-request behavior caused the dashboard to inadvertently restart stopped microVMs. Always separate "check status" from "ensure running".

### Multi-Instance Management
When managing multiple instances of anything (VMs, processes, connections), use:
- Unique IDs generated server-side (timestamp + counter works well)
- Optional user-friendly names with collision detection
- A registry (map) protected by mutex for concurrent access
- Per-instance resources (sockets, pipes) with unique paths

### TUI List Design
For terminal UI lists with selection:
- Sort data before rendering to prevent visual jumping
- Track selection by index, not by ID (simpler bounds checking)
- Support vim-style navigation (j/k) - users expect it
- Toggle-able details prevent information overload
- Use scroll indicators (▲▼) when list exceeds visible area

### Resource Monitoring
For process resource monitoring:
- Prefer OS tools (`ps`, `top`) over manual `/proc` parsing
- The OS already handles complex calculations (CPU %, memory)
- Shell out to `ps -p PID -o %cpu=,rss=` - simple and reliable
- Accept slight overhead for reliability and maintainability

## Architecture Evolution

```
Initial Plan:
  macOS → Code-Hex/vz → Linux VM → Firecracker
  (Failed: SIGKILL from macOS security)

Final Architecture:
  macOS → Tart CLI → Linux VM → fc-agent → Firecracker
  (Works: Tart handles signing, fc-agent handles API proxy)
```

## Files Changed Most

| File | Changes | Reason |
|------|---------|--------|
| `internal/agent/agent.go` | 10+ revisions | Context bug, console streaming, log suppression, multi-VM support, resource monitoring |
| `internal/cli/setup.go` | 5+ revisions | Binary transfer, resource config |
| `internal/cli/run.go` | 6+ revisions | Console connection, auto-stop previous, --name flag, new API |
| `internal/cli/microvm.go` | 5+ revisions | list command, --name flags, --all flag, name resolution |
| `internal/cli/dashboard.go` | 8+ revisions | TUI design iterations, resource monitoring, multi-VM list, j/k nav, details toggle |
| `test/e2e/cli_test.go` | 4+ revisions | Test reliability, multi-VM tests |

## Metrics

- **Unit Tests:** 22 passing
- **E2E Tests:** 20 passing (+ 2 full workflow tests)
- **Lines of Go Code:** ~4500
- **Development Time:** Multiple sessions over several days
- **Major Pivots:** 1 (Code-Hex/vz → Tart)
- **Dashboard Iterations:** 4 (over-designed → simplified → polished → multi-VM)
- **Agent API Versions:** 2 (single-VM → multi-VM with backward compat)

## Future Improvements

1. **Networking** - Add tap device support for microVM networking
2. **Snapshots** - Test and document snapshot functionality
3. ~~**Multiple microVMs** - Support running multiple microVMs simultaneously~~ ✅ Done
4. **Resource limits** - Better memory and CPU management
5. **Vsock** - Consider vsock instead of HTTP for lower latency
6. **Rootfs templates** - Pre-built rootfs images for common use cases
7. **Config files** - YAML/JSON config for microVM definitions
8. **Batch operations** - Start/stop multiple VMs by pattern

## References

- [Tart GitHub](https://github.com/cirruslabs/tart) - VM management
- [Firecracker Getting Started](https://github.com/firecracker-microvm/firecracker/blob/main/docs/getting-started.md)
- [Code-Hex/vz](https://github.com/Code-Hex/vz) - Original approach (didn't work on Tahoe)
- [Apple Virtualization Framework](https://developer.apple.com/documentation/virtualization)
