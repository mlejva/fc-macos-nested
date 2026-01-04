// Package main is the entry point for the fc-macos CLI tool.
// fc-macos is a wrapper around Firecracker that runs Linux VMs on macOS
// using Apple's Virtualization Framework with nested virtualization support.
package main

import (
	"os"

	"github.com/anthropics/fc-macos/internal/cli"
)

var version = "dev"

func main() {
	rootCmd := cli.NewRootCmd(version)
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
