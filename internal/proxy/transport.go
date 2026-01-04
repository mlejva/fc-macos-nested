// Package proxy provides communication between the macOS host and Firecracker in the Linux VM.
package proxy

import (
	"bufio"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/Code-Hex/vz/v3"
)

// VsockTransport implements http.RoundTripper over vsock.
type VsockTransport struct {
	device  *vz.VirtioSocketDevice
	port    uint32
	timeout time.Duration
	mu      sync.Mutex
}

// NewVsockTransport creates a new vsock transport.
func NewVsockTransport(device *vz.VirtioSocketDevice, port uint32) *VsockTransport {
	return &VsockTransport{
		device:  device,
		port:    port,
		timeout: 30 * time.Second,
	}
}

// SetTimeout sets the connection timeout.
func (t *VsockTransport) SetTimeout(timeout time.Duration) {
	t.timeout = timeout
}

// RoundTrip executes a single HTTP transaction.
func (t *VsockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.device == nil {
		return nil, fmt.Errorf("vsock device not available")
	}

	conn, err := t.device.Connect(t.port)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to vsock port %d: %w", t.port, err)
	}
	defer conn.Close()

	// Set deadline
	if t.timeout > 0 {
		conn.SetDeadline(time.Now().Add(t.timeout))
	}

	// Write HTTP request
	if err := req.Write(conn); err != nil {
		return nil, fmt.Errorf("failed to write request: %w", err)
	}

	// Read HTTP response
	resp, err := http.ReadResponse(bufio.NewReader(conn), req)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return resp, nil
}
