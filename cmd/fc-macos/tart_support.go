//go:build tart

// Package main provides Tart-based VM support when built with the 'tart' tag.
// Build with: go build -tags tart ./cmd/fc-macos
// This uses Tart CLI for VM management instead of Code-Hex/vz,
// avoiding macOS Tahoe signing requirements.
package main

import (
	// Import tartloader to register the Tart-based VM initializer.
	_ "github.com/anthropics/fc-macos/internal/tartloader"
)
