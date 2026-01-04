//go:build vm

// Package main provides VM support when built with the 'vm' tag.
// Build with: go build -tags vm ./cmd/fc-macos
package main

import (
	// Import vmloader to register the VM initializer.
	_ "github.com/anthropics/fc-macos/internal/vmloader"
)
