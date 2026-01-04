// Package api provides types for the Firecracker REST API.
package api

// BootSource represents the boot source configuration for a microVM.
type BootSource struct {
	KernelImagePath string `json:"kernel_image_path"`
	InitrdPath      string `json:"initrd_path,omitempty"`
	BootArgs        string `json:"boot_args,omitempty"`
}

// Drive represents a block device configuration.
type Drive struct {
	DriveID      string       `json:"drive_id"`
	PathOnHost   string       `json:"path_on_host"`
	IsRootDevice bool         `json:"is_root_device"`
	IsReadOnly   bool         `json:"is_read_only"`
	Partuuid     string       `json:"partuuid,omitempty"`
	CacheType    string       `json:"cache_type,omitempty"`
	IoEngine     string       `json:"io_engine,omitempty"`
	RateLimiter  *RateLimiter `json:"rate_limiter,omitempty"`
}

// NetworkInterface represents a network interface configuration.
type NetworkInterface struct {
	IfaceID       string       `json:"iface_id"`
	GuestMAC      string       `json:"guest_mac,omitempty"`
	HostDevName   string       `json:"host_dev_name"`
	RxRateLimiter *RateLimiter `json:"rx_rate_limiter,omitempty"`
	TxRateLimiter *RateLimiter `json:"tx_rate_limiter,omitempty"`
}

// MachineConfig represents the machine configuration.
type MachineConfig struct {
	VCPUCount       int    `json:"vcpu_count"`
	MemSizeMib      int    `json:"mem_size_mib"`
	SMT             bool   `json:"smt,omitempty"`
	CPUTemplate     string `json:"cpu_template,omitempty"`
	TrackDirtyPages bool   `json:"track_dirty_pages,omitempty"`
}

// Action represents a VM action request.
type Action struct {
	ActionType string `json:"action_type"`
}

// SnapshotCreate represents snapshot creation parameters.
type SnapshotCreate struct {
	SnapshotPath string `json:"snapshot_path"`
	MemFilePath  string `json:"mem_file_path"`
	SnapshotType string `json:"snapshot_type,omitempty"`
}

// SnapshotLoad represents snapshot loading parameters.
type SnapshotLoad struct {
	SnapshotPath        string `json:"snapshot_path"`
	MemFilePath         string `json:"mem_file_path,omitempty"`
	EnableDiffSnapshots bool   `json:"enable_diff_snapshots,omitempty"`
	ResumeVM            bool   `json:"resume_vm,omitempty"`
}

// Balloon represents memory balloon configuration.
type Balloon struct {
	AmountMib             int64 `json:"amount_mib"`
	DeflateOnOom          bool  `json:"deflate_on_oom"`
	StatsPollingIntervalS int64 `json:"stats_polling_interval_s,omitempty"`
}

// BalloonStats represents balloon statistics.
type BalloonStats struct {
	TargetPages        int64 `json:"target_pages"`
	ActualPages        int64 `json:"actual_pages"`
	TargetMib          int64 `json:"target_mib"`
	ActualMib          int64 `json:"actual_mib"`
	SwapIn             int64 `json:"swap_in,omitempty"`
	SwapOut            int64 `json:"swap_out,omitempty"`
	MajorFaults        int64 `json:"major_faults,omitempty"`
	MinorFaults        int64 `json:"minor_faults,omitempty"`
	FreeMemory         int64 `json:"free_memory,omitempty"`
	TotalMemory        int64 `json:"total_memory,omitempty"`
	AvailableMemory    int64 `json:"available_memory,omitempty"`
	DiskCaches         int64 `json:"disk_caches,omitempty"`
	HugetlbAllocations int64 `json:"hugetlb_allocations,omitempty"`
	HugetlbFailures    int64 `json:"hugetlb_failures,omitempty"`
}

// BalloonUpdate represents a balloon update request.
type BalloonUpdate struct {
	AmountMib int64 `json:"amount_mib"`
}

// RateLimiter represents rate limiting configuration.
type RateLimiter struct {
	Bandwidth *TokenBucket `json:"bandwidth,omitempty"`
	Ops       *TokenBucket `json:"ops,omitempty"`
}

// TokenBucket represents a token bucket configuration.
type TokenBucket struct {
	Size         int64 `json:"size"`
	OneTimeBurst int64 `json:"one_time_burst,omitempty"`
	RefillTime   int64 `json:"refill_time"`
}

// Metrics represents VM metrics.
type Metrics struct {
	Block   map[string]interface{} `json:"block,omitempty"`
	Net     map[string]interface{} `json:"net,omitempty"`
	VCPUs   []interface{}          `json:"vcpus,omitempty"`
	Seccomp map[string]interface{} `json:"seccomp,omitempty"`
	VMM     map[string]interface{} `json:"vmm,omitempty"`
	Signals map[string]interface{} `json:"signals,omitempty"`
	API     map[string]interface{} `json:"api,omitempty"`
}

// Vsock represents vsock device configuration.
type Vsock struct {
	GuestCID int64  `json:"guest_cid"`
	UDSPath  string `json:"uds_path"`
}

// Logger represents logger configuration.
type Logger struct {
	LogPath       string `json:"log_path"`
	Level         string `json:"level,omitempty"`
	ShowLevel     bool   `json:"show_level,omitempty"`
	ShowLogOrigin bool   `json:"show_log_origin,omitempty"`
}

// Version represents Firecracker version information.
type Version struct {
	FirecrackerVersion string `json:"firecracker_version"`
}

// InstanceInfo represents instance information.
type InstanceInfo struct {
	ID      string `json:"id"`
	State   string `json:"state"`
	VMState string `json:"vmm_version"`
	AppName string `json:"app_name"`
}

// VMState represents the VM state for pause/resume.
type VMState struct {
	State string `json:"state"` // "Paused" or "Resumed"
}

// Error represents an API error response.
type Error struct {
	FaultMessage string `json:"fault_message"`
}
