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

## What Worked Well

1. **Tart as VM layer** - Stable, well-maintained, handles all the signing complexity
2. **HTTP-based agent** - Simple, debuggable, works with standard tools
3. **Connection hijacking for console** - Clean way to do bidirectional streaming over HTTP
4. **Alpine for rootfs** - Minimal, fast to build, easy to customize
5. **Comprehensive E2E tests** - Caught many issues before manual testing

## What Didn't Work

1. **Code-Hex/vz direct approach** - Killed by macOS security requirements
2. **Stdin piping via tart exec** - Unreliable for binary data
3. **Context-based process management** - Killed Firecracker prematurely
4. **Ubuntu sample rootfs** - No proper init for serial console
5. **Serial socket approach** - More complex than stdin/stdout pipes

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
| `internal/agent/agent.go` | 5+ revisions | Context bug, console streaming |
| `internal/cli/setup.go` | 4+ revisions | Binary transfer method |
| `internal/cli/run.go` | 3+ revisions | Console connection |
| `test/e2e/cli_test.go` | 3+ revisions | Test reliability |

## Metrics

- **Unit Tests:** 22 passing
- **E2E Tests:** 20 passing
- **Lines of Go Code:** ~2500
- **Development Time:** Multiple sessions over several days
- **Major Pivots:** 1 (Code-Hex/vz → Tart)

## Future Improvements

1. **Networking** - Add tap device support for microVM networking
2. **Snapshots** - Test and document snapshot functionality
3. **Multiple microVMs** - Support running multiple microVMs simultaneously
4. **Resource limits** - Better memory and CPU management
5. **Vsock** - Consider vsock instead of HTTP for lower latency

## References

- [Tart GitHub](https://github.com/cirruslabs/tart) - VM management
- [Firecracker Getting Started](https://github.com/firecracker-microvm/firecracker/blob/main/docs/getting-started.md)
- [Code-Hex/vz](https://github.com/Code-Hex/vz) - Original approach (didn't work on Tahoe)
- [Apple Virtualization Framework](https://developer.apple.com/documentation/virtualization)
