// Package main is the entry point for the fc-agent daemon.
// fc-agent runs inside the Linux VM and proxies requests from the macOS host
// to the Firecracker process via vsock.
package main

import (
	"context"
	"flag"
	"os"
	"os/signal"
	"syscall"

	"github.com/anthropics/fc-macos/internal/agent"
	"github.com/sirupsen/logrus"
)

var version = "dev"

func main() {
	var (
		httpPort        = flag.Int("http-port", 8080, "HTTP port to listen on")
		vsockPort       = flag.Int("vsock-port", 2222, "vsock port to listen on (legacy)")
		fcPath          = flag.String("firecracker", "/usr/local/bin/firecracker", "path to firecracker binary")
		fcSocketPath    = flag.String("fc-socket", "/tmp/firecracker.socket", "path to firecracker API socket")
		logLevel        = flag.String("log-level", "info", "log level (debug, info, warn, error)")
		showVersion     = flag.Bool("version", false, "show version and exit")
	)
	flag.Parse()

	if *showVersion {
		logrus.Infof("fc-agent version %s", version)
		os.Exit(0)
	}

	// Configure logging
	level, err := logrus.ParseLevel(*logLevel)
	if err != nil {
		logrus.Fatalf("invalid log level: %v", err)
	}
	logrus.SetLevel(level)
	logrus.SetFormatter(&logrus.TextFormatter{
		FullTimestamp: true,
	})

	logrus.Infof("fc-agent version %s starting", version)

	// Create agent
	agentInstance := agent.New(&agent.Config{
		HTTPPort:       *httpPort,
		VsockPort:      uint32(*vsockPort),
		FirecrackerBin: *fcPath,
		SocketPath:     *fcSocketPath,
	})

	// Set up context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		logrus.Infof("received signal %v, shutting down", sig)
		cancel()
	}()

	// Run the agent
	if err := agentInstance.Run(ctx); err != nil {
		logrus.Fatalf("agent error: %v", err)
	}

	logrus.Info("fc-agent stopped")
}
