package cli

import (
	"fmt"

	"github.com/anthropics/fc-macos/pkg/api"
	"github.com/spf13/cobra"
)

func newSnapshotsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "snapshots",
		Aliases: []string{"snapshot"},
		Short:   "Manage microVM snapshots",
	}

	cmd.AddCommand(newSnapshotsCreateCmd())
	cmd.AddCommand(newSnapshotsLoadCmd())

	return cmd
}

func newSnapshotsCreateCmd() *cobra.Command {
	var (
		snapshotPath string
		memFilePath  string
		snapshotType string
	)

	cmd := &cobra.Command{
		Use:   "create",
		Short: "Create a snapshot of the microVM",
		Long: `Create a snapshot of the running microVM. The VM must be paused
before creating a snapshot. Snapshots include VM state and optionally memory.`,
		Example: `  # Create a full snapshot
  fc-macos snapshots create --snapshot-path /path/to/snapshot --mem-path /path/to/mem

  # Create a diff snapshot (requires track-dirty-pages enabled)
  fc-macos snapshots create --snapshot-path /path/to/snapshot --mem-path /path/to/mem --type Diff`,
		RunE: func(cmd *cobra.Command, args []string) error {
			params := &api.SnapshotCreate{
				SnapshotPath: snapshotPath,
				MemFilePath:  memFilePath,
				SnapshotType: snapshotType,
			}

			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.CreateSnapshot(cmd.Context(), params); err != nil {
				return fmt.Errorf("failed to create snapshot: %w", err)
			}

			fmt.Println("Snapshot created successfully")
			return nil
		},
	}

	cmd.Flags().StringVar(&snapshotPath, "snapshot-path", "", "path to save the snapshot file (required)")
	cmd.Flags().StringVar(&memFilePath, "mem-path", "", "path to save the memory file (required)")
	cmd.Flags().StringVar(&snapshotType, "type", "Full", "snapshot type (Full, Diff)")
	cmd.MarkFlagRequired("snapshot-path")
	cmd.MarkFlagRequired("mem-path")

	return cmd
}

func newSnapshotsLoadCmd() *cobra.Command {
	var (
		snapshotPath        string
		memFilePath         string
		enableDiffSnapshots bool
		resumeVM            bool
	)

	cmd := &cobra.Command{
		Use:   "load",
		Short: "Load a snapshot into a new microVM",
		Long: `Load a previously created snapshot to restore a microVM to a saved state.
This is used to quickly start a VM from a known state.`,
		Example: `  # Load snapshot and resume immediately
  fc-macos snapshots load --snapshot-path /path/to/snapshot --mem-path /path/to/mem --resume

  # Load snapshot with diff snapshots enabled
  fc-macos snapshots load --snapshot-path /path/to/snapshot --mem-path /path/to/mem --enable-diff`,
		RunE: func(cmd *cobra.Command, args []string) error {
			params := &api.SnapshotLoad{
				SnapshotPath:        snapshotPath,
				MemFilePath:         memFilePath,
				EnableDiffSnapshots: enableDiffSnapshots,
				ResumeVM:            resumeVM,
			}

			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.LoadSnapshot(cmd.Context(), params); err != nil {
				return fmt.Errorf("failed to load snapshot: %w", err)
			}

			fmt.Println("Snapshot loaded successfully")
			if resumeVM {
				fmt.Println("MicroVM resumed")
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&snapshotPath, "snapshot-path", "", "path to the snapshot file (required)")
	cmd.Flags().StringVar(&memFilePath, "mem-path", "", "path to the memory file")
	cmd.Flags().BoolVar(&enableDiffSnapshots, "enable-diff", false, "enable incremental/diff snapshots")
	cmd.Flags().BoolVar(&resumeVM, "resume", false, "resume the VM after loading")
	cmd.MarkFlagRequired("snapshot-path")

	return cmd
}
