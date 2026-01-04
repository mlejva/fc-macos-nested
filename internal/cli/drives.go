package cli

import (
	"encoding/json"
	"fmt"

	"github.com/anthropics/fc-macos/pkg/api"
	"github.com/spf13/cobra"
)

func newDrivesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drives",
		Short: "Manage block devices for the microVM",
	}

	cmd.AddCommand(newDrivesAddCmd())
	cmd.AddCommand(newDrivesListCmd())
	cmd.AddCommand(newDrivesUpdateCmd())
	cmd.AddCommand(newDrivesRemoveCmd())

	return cmd
}

func newDrivesAddCmd() *cobra.Command {
	var (
		driveID    string
		pathOnHost string
		isRoot     bool
		readOnly   bool
		cacheType  string
		ioEngine   string
	)

	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a block device to the microVM",
		Example: `  # Add root filesystem
  fc-macos drives add --id rootfs --path /path/to/rootfs.ext4 --root

  # Add additional read-only drive
  fc-macos drives add --id data --path /path/to/data.ext4 --read-only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			drive := &api.Drive{
				DriveID:      driveID,
				PathOnHost:   pathOnHost,
				IsRootDevice: isRoot,
				IsReadOnly:   readOnly,
				CacheType:    cacheType,
				IoEngine:     ioEngine,
			}

			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.SetDrive(cmd.Context(), driveID, drive); err != nil {
				return fmt.Errorf("failed to add drive: %w", err)
			}

			fmt.Printf("Drive '%s' added successfully\n", driveID)
			return nil
		},
	}

	cmd.Flags().StringVar(&driveID, "id", "", "unique identifier for the drive (required)")
	cmd.Flags().StringVar(&pathOnHost, "path", "", "path to the drive image on host (required)")
	cmd.Flags().BoolVar(&isRoot, "root", false, "mark as root device")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "mount as read-only")
	cmd.Flags().StringVar(&cacheType, "cache-type", "", "cache type (Unsafe, Writeback)")
	cmd.Flags().StringVar(&ioEngine, "io-engine", "", "I/O engine (Sync, Async)")
	cmd.MarkFlagRequired("id")
	cmd.MarkFlagRequired("path")

	return cmd
}

func newDrivesListCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all configured drives",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			result, err := client.GetDrives(cmd.Context())
			if err != nil {
				return fmt.Errorf("failed to get drives: %w", err)
			}

			// Convert []interface{} to []api.Drive via JSON
			var drives []api.Drive
			data, err := json.Marshal(result)
			if err != nil {
				return err
			}
			if err := json.Unmarshal(data, &drives); err != nil {
				return err
			}

			if outputJSON {
				data, err := json.MarshalIndent(drives, "", "  ")
				if err != nil {
					return err
				}
				fmt.Println(string(data))
			} else {
				if len(drives) == 0 {
					fmt.Println("No drives configured")
					return nil
				}
				for _, drive := range drives {
					fmt.Printf("ID: %s\n", drive.DriveID)
					fmt.Printf("  Path: %s\n", drive.PathOnHost)
					fmt.Printf("  Root Device: %v\n", drive.IsRootDevice)
					fmt.Printf("  Read Only: %v\n", drive.IsReadOnly)
					fmt.Println()
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output in JSON format")

	return cmd
}

func newDrivesUpdateCmd() *cobra.Command {
	var (
		driveID    string
		pathOnHost string
	)

	cmd := &cobra.Command{
		Use:   "update",
		Short: "Update a drive's backing file path",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.PatchDrive(cmd.Context(), driveID, pathOnHost); err != nil {
				return fmt.Errorf("failed to update drive: %w", err)
			}

			fmt.Printf("Drive '%s' updated successfully\n", driveID)
			return nil
		},
	}

	cmd.Flags().StringVar(&driveID, "id", "", "drive identifier (required)")
	cmd.Flags().StringVar(&pathOnHost, "path", "", "new path to the drive image (required)")
	cmd.MarkFlagRequired("id")
	cmd.MarkFlagRequired("path")

	return cmd
}

func newDrivesRemoveCmd() *cobra.Command {
	var driveID string

	cmd := &cobra.Command{
		Use:   "remove",
		Short: "Remove a drive from the microVM",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := getFirecrackerClient(cmd)
			if err != nil {
				return err
			}

			if err := client.DeleteDrive(cmd.Context(), driveID); err != nil {
				return fmt.Errorf("failed to remove drive: %w", err)
			}

			fmt.Printf("Drive '%s' removed successfully\n", driveID)
			return nil
		},
	}

	cmd.Flags().StringVar(&driveID, "id", "", "drive identifier (required)")
	cmd.MarkFlagRequired("id")

	return cmd
}
