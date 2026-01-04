package api

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBootSourceJSON(t *testing.T) {
	bs := &BootSource{
		KernelImagePath: "/path/to/kernel",
		InitrdPath:      "/path/to/initrd",
		BootArgs:        "console=ttyS0 reboot=k panic=1",
	}

	// Marshal
	data, err := json.Marshal(bs)
	require.NoError(t, err)

	// Verify expected JSON fields
	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, "/path/to/kernel", m["kernel_image_path"])
	assert.Equal(t, "/path/to/initrd", m["initrd_path"])
	assert.Equal(t, "console=ttyS0 reboot=k panic=1", m["boot_args"])

	// Unmarshal
	var bs2 BootSource
	require.NoError(t, json.Unmarshal(data, &bs2))
	assert.Equal(t, bs.KernelImagePath, bs2.KernelImagePath)
	assert.Equal(t, bs.InitrdPath, bs2.InitrdPath)
	assert.Equal(t, bs.BootArgs, bs2.BootArgs)
}

func TestBootSourceJSONOmitsEmptyFields(t *testing.T) {
	bs := &BootSource{
		KernelImagePath: "/path/to/kernel",
	}

	data, err := json.Marshal(bs)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, "/path/to/kernel", m["kernel_image_path"])
	assert.NotContains(t, m, "initrd_path")
	assert.NotContains(t, m, "boot_args")
}

func TestDriveJSON(t *testing.T) {
	drive := &Drive{
		DriveID:      "rootfs",
		PathOnHost:   "/path/to/rootfs.ext4",
		IsRootDevice: true,
		IsReadOnly:   false,
	}

	data, err := json.Marshal(drive)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, "rootfs", m["drive_id"])
	assert.Equal(t, "/path/to/rootfs.ext4", m["path_on_host"])
	assert.Equal(t, true, m["is_root_device"])
	assert.Equal(t, false, m["is_read_only"])
}

func TestDriveWithRateLimiter(t *testing.T) {
	drive := &Drive{
		DriveID:      "data",
		PathOnHost:   "/path/to/data.ext4",
		IsRootDevice: false,
		IsReadOnly:   true,
		RateLimiter: &RateLimiter{
			Bandwidth: &TokenBucket{
				Size:       1000000,
				RefillTime: 1000,
			},
		},
	}

	data, err := json.Marshal(drive)
	require.NoError(t, err)

	var drive2 Drive
	require.NoError(t, json.Unmarshal(data, &drive2))
	require.NotNil(t, drive2.RateLimiter)
	require.NotNil(t, drive2.RateLimiter.Bandwidth)
	assert.Equal(t, int64(1000000), drive2.RateLimiter.Bandwidth.Size)
	assert.Equal(t, int64(1000), drive2.RateLimiter.Bandwidth.RefillTime)
}

func TestMachineConfigJSON(t *testing.T) {
	cfg := &MachineConfig{
		VCPUCount:       2,
		MemSizeMib:      256,
		SMT:             true,
		TrackDirtyPages: true,
	}

	data, err := json.Marshal(cfg)
	require.NoError(t, err)

	var cfg2 MachineConfig
	require.NoError(t, json.Unmarshal(data, &cfg2))
	assert.Equal(t, 2, cfg2.VCPUCount)
	assert.Equal(t, 256, cfg2.MemSizeMib)
	assert.True(t, cfg2.SMT)
	assert.True(t, cfg2.TrackDirtyPages)
}

func TestNetworkInterfaceJSON(t *testing.T) {
	iface := &NetworkInterface{
		IfaceID:     "eth0",
		GuestMAC:    "06:00:AC:10:00:02",
		HostDevName: "tap0",
	}

	data, err := json.Marshal(iface)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, "eth0", m["iface_id"])
	assert.Equal(t, "06:00:AC:10:00:02", m["guest_mac"])
	assert.Equal(t, "tap0", m["host_dev_name"])
}

func TestActionJSON(t *testing.T) {
	action := &Action{ActionType: "InstanceStart"}

	data, err := json.Marshal(action)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, "InstanceStart", m["action_type"])
}

func TestSnapshotCreateJSON(t *testing.T) {
	snap := &SnapshotCreate{
		SnapshotPath: "/path/to/snapshot",
		MemFilePath:  "/path/to/mem",
		SnapshotType: "Full",
	}

	data, err := json.Marshal(snap)
	require.NoError(t, err)

	var snap2 SnapshotCreate
	require.NoError(t, json.Unmarshal(data, &snap2))
	assert.Equal(t, "/path/to/snapshot", snap2.SnapshotPath)
	assert.Equal(t, "/path/to/mem", snap2.MemFilePath)
	assert.Equal(t, "Full", snap2.SnapshotType)
}

func TestBalloonJSON(t *testing.T) {
	balloon := &Balloon{
		AmountMib:             256,
		DeflateOnOom:          true,
		StatsPollingIntervalS: 5,
	}

	data, err := json.Marshal(balloon)
	require.NoError(t, err)

	var balloon2 Balloon
	require.NoError(t, json.Unmarshal(data, &balloon2))
	assert.Equal(t, int64(256), balloon2.AmountMib)
	assert.True(t, balloon2.DeflateOnOom)
	assert.Equal(t, int64(5), balloon2.StatsPollingIntervalS)
}

func TestBalloonStatsJSON(t *testing.T) {
	stats := &BalloonStats{
		TargetPages:     1000,
		ActualPages:     900,
		TargetMib:       256,
		ActualMib:       230,
		FreeMemory:      1024000,
		TotalMemory:     2048000,
		AvailableMemory: 1500000,
	}

	data, err := json.Marshal(stats)
	require.NoError(t, err)

	var stats2 BalloonStats
	require.NoError(t, json.Unmarshal(data, &stats2))
	assert.Equal(t, int64(1000), stats2.TargetPages)
	assert.Equal(t, int64(900), stats2.ActualPages)
	assert.Equal(t, int64(256), stats2.TargetMib)
	assert.Equal(t, int64(230), stats2.ActualMib)
}

func TestErrorJSON(t *testing.T) {
	apiErr := &Error{
		FaultMessage: "Invalid configuration",
	}

	data, err := json.Marshal(apiErr)
	require.NoError(t, err)

	var m map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &m))
	assert.Equal(t, "Invalid configuration", m["fault_message"])
}
