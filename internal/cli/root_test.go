package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewRootCmd(t *testing.T) {
	cmd := NewRootCmd("test-version")

	assert.Equal(t, "fc-macos", cmd.Use)
	assert.NotEmpty(t, cmd.Short)
	assert.NotEmpty(t, cmd.Long)
}

func TestRootCmdHasSubcommands(t *testing.T) {
	cmd := NewRootCmd("test-version")

	subcommands := []string{
		"version",
		"boot",
		"drives",
		"network",
		"machine",
		"actions",
		"snapshots",
		"metrics",
		"balloon",
		"vm",
	}

	for _, name := range subcommands {
		found := false
		for _, sub := range cmd.Commands() {
			if sub.Name() == name {
				found = true
				break
			}
		}
		assert.True(t, found, "subcommand %s not found", name)
	}
}

func TestVersionCmd(t *testing.T) {
	cmd := NewRootCmd("1.2.3")
	cmd.SetArgs([]string{"version"})

	// The version command uses fmt.Printf which writes to os.Stdout
	// We just verify it executes without error
	err := cmd.Execute()
	require.NoError(t, err)
}

func TestBootCmdRequiresKernel(t *testing.T) {
	cmd := NewRootCmd("test")
	cmd.SetArgs([]string{"boot", "set"})

	err := cmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "required flag")
}

func TestDrivesCmdRequiresIdAndPath(t *testing.T) {
	cmd := NewRootCmd("test")
	cmd.SetArgs([]string{"drives", "add"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestNetworkCmdRequiresIdAndTap(t *testing.T) {
	cmd := NewRootCmd("test")
	cmd.SetArgs([]string{"network", "add"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestSnapshotCreateRequiresPaths(t *testing.T) {
	cmd := NewRootCmd("test")
	cmd.SetArgs([]string{"snapshots", "create"})

	err := cmd.Execute()
	require.Error(t, err)
}

func TestBalloonSetRequiresAmount(t *testing.T) {
	cmd := NewRootCmd("test")
	cmd.SetArgs([]string{"balloon", "set"})

	err := cmd.Execute()
	require.Error(t, err)
}
