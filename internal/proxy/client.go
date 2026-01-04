package proxy

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/anthropics/fc-macos/pkg/api"
)

// FirecrackerClient is a client for the Firecracker API.
type FirecrackerClient struct {
	httpClient *http.Client
	baseURL    string
}

// NewFirecrackerClient creates a new Firecracker API client.
func NewFirecrackerClient(transport http.RoundTripper) *FirecrackerClient {
	return &FirecrackerClient{
		httpClient: &http.Client{Transport: transport},
		baseURL:    "http://localhost", // vsock doesn't need a real host
	}
}

// Boot Source

// SetBootSource configures the boot source for the microVM.
func (c *FirecrackerClient) SetBootSource(ctx context.Context, bs *api.BootSource) error {
	return c.put(ctx, "/boot-source", bs)
}

// GetBootSource retrieves the boot source configuration.
func (c *FirecrackerClient) GetBootSource(ctx context.Context) (*api.BootSource, error) {
	var bs api.BootSource
	if err := c.get(ctx, "/boot-source", &bs); err != nil {
		return nil, err
	}
	return &bs, nil
}

// Drives

// SetDrive configures a block device.
func (c *FirecrackerClient) SetDrive(ctx context.Context, id string, drive *api.Drive) error {
	return c.put(ctx, fmt.Sprintf("/drives/%s", id), drive)
}

// GetDrives retrieves all configured drives.
func (c *FirecrackerClient) GetDrives(ctx context.Context) ([]*api.Drive, error) {
	var drives []*api.Drive
	if err := c.get(ctx, "/drives", &drives); err != nil {
		return nil, err
	}
	return drives, nil
}

// PatchDrive updates a drive's backing file.
func (c *FirecrackerClient) PatchDrive(ctx context.Context, id string, pathOnHost string) error {
	return c.patch(ctx, fmt.Sprintf("/drives/%s", id), map[string]string{
		"path_on_host": pathOnHost,
	})
}

// DeleteDrive removes a drive configuration.
func (c *FirecrackerClient) DeleteDrive(ctx context.Context, id string) error {
	return c.delete(ctx, fmt.Sprintf("/drives/%s", id))
}

// Network Interfaces

// SetNetworkInterface configures a network interface.
func (c *FirecrackerClient) SetNetworkInterface(ctx context.Context, id string, iface *api.NetworkInterface) error {
	return c.put(ctx, fmt.Sprintf("/network-interfaces/%s", id), iface)
}

// GetNetworkInterfaces retrieves all configured network interfaces.
func (c *FirecrackerClient) GetNetworkInterfaces(ctx context.Context) ([]*api.NetworkInterface, error) {
	var interfaces []*api.NetworkInterface
	if err := c.get(ctx, "/network-interfaces", &interfaces); err != nil {
		return nil, err
	}
	return interfaces, nil
}

// PatchNetworkInterface updates a network interface's rate limiters.
func (c *FirecrackerClient) PatchNetworkInterface(ctx context.Context, id string, rxLimiter, txLimiter *api.RateLimiter) error {
	update := make(map[string]interface{})
	if rxLimiter != nil {
		update["rx_rate_limiter"] = rxLimiter
	}
	if txLimiter != nil {
		update["tx_rate_limiter"] = txLimiter
	}
	return c.patch(ctx, fmt.Sprintf("/network-interfaces/%s", id), update)
}

// DeleteNetworkInterface removes a network interface configuration.
func (c *FirecrackerClient) DeleteNetworkInterface(ctx context.Context, id string) error {
	return c.delete(ctx, fmt.Sprintf("/network-interfaces/%s", id))
}

// Machine Config

// SetMachineConfig configures the machine settings.
func (c *FirecrackerClient) SetMachineConfig(ctx context.Context, cfg *api.MachineConfig) error {
	return c.put(ctx, "/machine-config", cfg)
}

// GetMachineConfig retrieves the machine configuration.
func (c *FirecrackerClient) GetMachineConfig(ctx context.Context) (*api.MachineConfig, error) {
	var cfg api.MachineConfig
	if err := c.get(ctx, "/machine-config", &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// Version

// GetVersion retrieves the Firecracker version.
func (c *FirecrackerClient) GetVersion(ctx context.Context) (*api.Version, error) {
	var version api.Version
	if err := c.get(ctx, "/version", &version); err != nil {
		return nil, err
	}
	return &version, nil
}

// Actions

// StartInstance starts the microVM.
func (c *FirecrackerClient) StartInstance(ctx context.Context) error {
	return c.put(ctx, "/actions", &api.Action{ActionType: "InstanceStart"})
}

// StopInstance sends Ctrl+Alt+Del to the microVM.
func (c *FirecrackerClient) StopInstance(ctx context.Context) error {
	return c.put(ctx, "/actions", &api.Action{ActionType: "SendCtrlAltDel"})
}

// ForceStopInstance forcefully stops the microVM.
func (c *FirecrackerClient) ForceStopInstance(ctx context.Context) error {
	return c.put(ctx, "/actions", &api.Action{ActionType: "FlushMetrics"})
}

// PauseInstance pauses the microVM.
func (c *FirecrackerClient) PauseInstance(ctx context.Context) error {
	return c.patch(ctx, "/vm", &api.VMState{State: "Paused"})
}

// ResumeInstance resumes the microVM.
func (c *FirecrackerClient) ResumeInstance(ctx context.Context) error {
	return c.patch(ctx, "/vm", &api.VMState{State: "Resumed"})
}

// Snapshots

// CreateSnapshot creates a snapshot of the microVM.
func (c *FirecrackerClient) CreateSnapshot(ctx context.Context, params *api.SnapshotCreate) error {
	return c.put(ctx, "/snapshot/create", params)
}

// LoadSnapshot loads a snapshot into a microVM.
func (c *FirecrackerClient) LoadSnapshot(ctx context.Context, params *api.SnapshotLoad) error {
	return c.put(ctx, "/snapshot/load", params)
}

// Metrics

// GetMetrics retrieves the microVM metrics.
func (c *FirecrackerClient) GetMetrics(ctx context.Context) (*api.Metrics, error) {
	var metrics api.Metrics
	if err := c.get(ctx, "/metrics", &metrics); err != nil {
		return nil, err
	}
	return &metrics, nil
}

// Balloon

// SetBalloon configures the memory balloon device.
func (c *FirecrackerClient) SetBalloon(ctx context.Context, balloon *api.Balloon) error {
	return c.put(ctx, "/balloon", balloon)
}

// GetBalloon retrieves the balloon configuration.
func (c *FirecrackerClient) GetBalloon(ctx context.Context) (*api.Balloon, error) {
	var balloon api.Balloon
	if err := c.get(ctx, "/balloon", &balloon); err != nil {
		return nil, err
	}
	return &balloon, nil
}

// GetBalloonStats retrieves balloon statistics.
func (c *FirecrackerClient) GetBalloonStats(ctx context.Context) (*api.BalloonStats, error) {
	var stats api.BalloonStats
	if err := c.get(ctx, "/balloon/statistics", &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

// PatchBalloon updates the balloon target size.
func (c *FirecrackerClient) PatchBalloon(ctx context.Context, amountMib int64) error {
	return c.patch(ctx, "/balloon", &api.BalloonUpdate{AmountMib: amountMib})
}

// HTTP helpers

func (c *FirecrackerClient) put(ctx context.Context, path string, body interface{}) error {
	return c.doRequest(ctx, "PUT", path, body, nil)
}

func (c *FirecrackerClient) get(ctx context.Context, path string, result interface{}) error {
	return c.doRequest(ctx, "GET", path, nil, result)
}

func (c *FirecrackerClient) patch(ctx context.Context, path string, body interface{}) error {
	return c.doRequest(ctx, "PATCH", path, body, nil)
}

func (c *FirecrackerClient) delete(ctx context.Context, path string) error {
	return c.doRequest(ctx, "DELETE", path, nil, nil)
}

func (c *FirecrackerClient) doRequest(ctx context.Context, method, path string, body, result interface{}) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		var apiErr api.Error
		if err := json.NewDecoder(resp.Body).Decode(&apiErr); err == nil && apiErr.FaultMessage != "" {
			return fmt.Errorf("API error (%d): %s", resp.StatusCode, apiErr.FaultMessage)
		}
		return fmt.Errorf("API error: %s", resp.Status)
	}

	if result != nil && resp.StatusCode != http.StatusNoContent {
		if err := json.NewDecoder(resp.Body).Decode(result); err != nil {
			return fmt.Errorf("failed to decode response: %w", err)
		}
	}

	return nil
}
