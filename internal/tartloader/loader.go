// Package tartloader registers the VM initializer that uses Tart for VM management.
// This avoids the macOS Tahoe signing requirements by using Tart which is
// properly signed and notarized by Cirrus Labs.
package tartloader

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/anthropics/fc-macos/internal/cli"
	"github.com/anthropics/fc-macos/internal/tart"
)

func init() {
	cli.RegisterVMInitializer(initializeVM)
}

// vmWrapper implements cli.VMProvider using Tart
type vmWrapper struct {
	vm *tart.VM
}

func (w *vmWrapper) Start(ctx context.Context) error {
	return w.vm.Start(ctx)
}

func (w *vmWrapper) Stop(ctx context.Context, force bool) error {
	return w.vm.Stop(ctx, force)
}

func (w *vmWrapper) State() string {
	return w.vm.State()
}

func (w *vmWrapper) PingAgent() bool {
	return w.vm.PingAgent()
}

func (w *vmWrapper) Logs(ctx context.Context, follow bool, lines int) error {
	return w.vm.Logs(ctx, follow, lines)
}

func (w *vmWrapper) Shell(ctx context.Context) error {
	return w.vm.Shell(ctx)
}

func (w *vmWrapper) Config() cli.VMConfig {
	return &configWrapper{cfg: w.vm.Config()}
}

// configWrapper implements cli.VMConfig
type configWrapper struct {
	cfg *tart.Config
}

func (c *configWrapper) GetCPUCount() uint {
	return c.cfg.CPUCount
}

func (c *configWrapper) GetMemorySizeMiB() uint64 {
	return c.cfg.MemorySizeMiB
}

// httpClientWrapper implements cli.FirecrackerClientProvider using HTTP to fc-agent
type httpClientWrapper struct {
	vm         *tart.VM
	httpClient *http.Client
	agentURL   string
}

func (c *httpClientWrapper) doRequest(ctx context.Context, method, path string, body interface{}) ([]byte, error) {
	var reqBody io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request: %w", err)
		}
		reqBody = bytes.NewReader(data)
	}

	url := c.agentURL + path
	req, err := http.NewRequestWithContext(ctx, method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("API error (status %d): %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}

func (c *httpClientWrapper) SetBootSource(ctx context.Context, bs interface{}) error {
	_, err := c.doRequest(ctx, "PUT", "/boot-source", bs)
	return err
}

func (c *httpClientWrapper) GetBootSource(ctx context.Context) (interface{}, error) {
	data, err := c.doRequest(ctx, "GET", "/boot-source", nil)
	if err != nil {
		return nil, err
	}
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *httpClientWrapper) SetDrive(ctx context.Context, id string, drive interface{}) error {
	_, err := c.doRequest(ctx, "PUT", "/drives/"+id, drive)
	return err
}

func (c *httpClientWrapper) GetDrives(ctx context.Context) ([]interface{}, error) {
	data, err := c.doRequest(ctx, "GET", "/drives", nil)
	if err != nil {
		return nil, err
	}
	var result []interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *httpClientWrapper) PatchDrive(ctx context.Context, id string, pathOnHost string) error {
	_, err := c.doRequest(ctx, "PATCH", "/drives/"+id, map[string]string{"path_on_host": pathOnHost})
	return err
}

func (c *httpClientWrapper) DeleteDrive(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, "DELETE", "/drives/"+id, nil)
	return err
}

func (c *httpClientWrapper) SetNetworkInterface(ctx context.Context, id string, iface interface{}) error {
	_, err := c.doRequest(ctx, "PUT", "/network-interfaces/"+id, iface)
	return err
}

func (c *httpClientWrapper) GetNetworkInterfaces(ctx context.Context) ([]interface{}, error) {
	data, err := c.doRequest(ctx, "GET", "/network-interfaces", nil)
	if err != nil {
		return nil, err
	}
	var result []interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *httpClientWrapper) PatchNetworkInterface(ctx context.Context, id string, rxLimiter, txLimiter interface{}) error {
	_, err := c.doRequest(ctx, "PATCH", "/network-interfaces/"+id, map[string]interface{}{
		"rx_rate_limiter": rxLimiter,
		"tx_rate_limiter": txLimiter,
	})
	return err
}

func (c *httpClientWrapper) DeleteNetworkInterface(ctx context.Context, id string) error {
	_, err := c.doRequest(ctx, "DELETE", "/network-interfaces/"+id, nil)
	return err
}

func (c *httpClientWrapper) SetMachineConfig(ctx context.Context, cfg interface{}) error {
	_, err := c.doRequest(ctx, "PUT", "/machine-config", cfg)
	return err
}

func (c *httpClientWrapper) GetMachineConfig(ctx context.Context) (interface{}, error) {
	data, err := c.doRequest(ctx, "GET", "/machine-config", nil)
	if err != nil {
		return nil, err
	}
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *httpClientWrapper) GetVersion(ctx context.Context) (interface{}, error) {
	data, err := c.doRequest(ctx, "GET", "/version", nil)
	if err != nil {
		return nil, err
	}
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *httpClientWrapper) StartInstance(ctx context.Context) error {
	_, err := c.doRequest(ctx, "PUT", "/actions", map[string]string{"action_type": "InstanceStart"})
	return err
}

func (c *httpClientWrapper) StopInstance(ctx context.Context) error {
	_, err := c.doRequest(ctx, "PUT", "/actions", map[string]string{"action_type": "SendCtrlAltDel"})
	return err
}

func (c *httpClientWrapper) ForceStopInstance(ctx context.Context) error {
	_, err := c.doRequest(ctx, "PUT", "/actions", map[string]string{"action_type": "SendCtrlAltDel"})
	return err
}

func (c *httpClientWrapper) PauseInstance(ctx context.Context) error {
	_, err := c.doRequest(ctx, "PATCH", "/vm", map[string]string{"state": "Paused"})
	return err
}

func (c *httpClientWrapper) ResumeInstance(ctx context.Context) error {
	_, err := c.doRequest(ctx, "PATCH", "/vm", map[string]string{"state": "Resumed"})
	return err
}

func (c *httpClientWrapper) CreateSnapshot(ctx context.Context, params interface{}) error {
	_, err := c.doRequest(ctx, "PUT", "/snapshot/create", params)
	return err
}

func (c *httpClientWrapper) LoadSnapshot(ctx context.Context, params interface{}) error {
	_, err := c.doRequest(ctx, "PUT", "/snapshot/load", params)
	return err
}

func (c *httpClientWrapper) GetMetrics(ctx context.Context) (interface{}, error) {
	data, err := c.doRequest(ctx, "GET", "/metrics", nil)
	if err != nil {
		return nil, err
	}
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *httpClientWrapper) SetBalloon(ctx context.Context, balloon interface{}) error {
	_, err := c.doRequest(ctx, "PUT", "/balloon", balloon)
	return err
}

func (c *httpClientWrapper) GetBalloon(ctx context.Context) (interface{}, error) {
	data, err := c.doRequest(ctx, "GET", "/balloon", nil)
	if err != nil {
		return nil, err
	}
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *httpClientWrapper) GetBalloonStats(ctx context.Context) (interface{}, error) {
	data, err := c.doRequest(ctx, "GET", "/balloon/statistics", nil)
	if err != nil {
		return nil, err
	}
	var result interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	return result, nil
}

func (c *httpClientWrapper) PatchBalloon(ctx context.Context, amountMib int64) error {
	_, err := c.doRequest(ctx, "PATCH", "/balloon", map[string]int64{"amount_mib": amountMib})
	return err
}

func initializeVM(ctx context.Context, cfg cli.VMInitConfig) (cli.VMProvider, cli.FirecrackerClientProvider, error) {
	// Convert shared directory
	var sharedDirs []tart.SharedDir
	if cfg.SharedDir != "" {
		sharedDirs = append(sharedDirs, tart.SharedDir{
			Name:     "shared",
			HostPath: cfg.SharedDir,
			ReadOnly: false,
		})
	}

	tartCfg := &tart.Config{
		CPUCount:      cfg.CPUCount,
		MemorySizeMiB: cfg.MemorySizeMiB,
		SharedDirs:    sharedDirs,
		Nested:        true, // Always enable nested virtualization for Firecracker
	}

	vm, err := tart.New(tartCfg)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Tart VM: %w", err)
	}

	// For Tart, we need a pre-built Linux image with fc-agent and Firecracker
	linuxImage := "ghcr.io/cirruslabs/ubuntu:latest"

	// Ensure VM exists
	if err := vm.EnsureVM(ctx, linuxImage); err != nil {
		return nil, nil, fmt.Errorf("failed to ensure VM exists: %w", err)
	}

	// Start the VM
	logrus.Info("Starting Linux VM...")
	if err := vm.Start(ctx); err != nil {
		return nil, nil, fmt.Errorf("failed to start VM: %w", err)
	}

	// Wait for SSH to be available
	logrus.Info("Waiting for VM to be ready...")
	if err := vm.WaitForSSH(ctx); err != nil {
		vm.Stop(ctx, true)
		return nil, nil, fmt.Errorf("SSH not available: %w", err)
	}

	// Get VM IP for the HTTP client
	ip, err := vm.GetIP(ctx)
	if err != nil {
		vm.Stop(ctx, true)
		return nil, nil, fmt.Errorf("failed to get VM IP: %w", err)
	}

	logrus.Infof("VM ready at %s", ip)

	// Create HTTP client that talks to the agent
	client := &httpClientWrapper{
		vm:       vm,
		agentURL: fmt.Sprintf("http://%s:8080", ip),
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}

	return &vmWrapper{vm: vm}, client, nil
}
