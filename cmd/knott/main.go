// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// Command knott is the all-in-one KNOTT server.
//
// One executable, one port, no runtime dependencies: it runs the workflow
// registry, execution engine, human-task service and agent registry inside a
// single process, serves the embedded web console, and keeps its state in a
// per-user directory it creates on first run.
//
// The native desktop application (cmd/knott-desktop in the desktop module)
// embeds the same platform behind a real window; this binary is the headless
// server for machines, containers and anyone who prefers a browser.
package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/regnant/knott/internal/app"
	"github.com/regnant/knott/internal/platform"
)

// Build metadata, stamped by the release build with -ldflags -X.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const banner = "\n" +
	"   ╭───╮ ╭───╮\n" +
	"   │  ─┼─┼─  │   K N O T T\n" +
	"   ╰───╯ ╰───╯   Sovereign Workflow Orchestration\n"

const helpText = `Usage:
  knott [command] [flags]

Commands:
  serve      Run the platform and print the console URL (default)
  desktop    Open the KNOTT desktop app (or the console in a browser)
  version    Print version and build information
  home       Print the directory KNOTT stores its data in
  help       Show this message

Flags:
  --port int         Port for the console and API (default 8002, or $PORT)
  --host string      Address to bind (default 127.0.0.1; use 0.0.0.0 to expose)
  --home string      State directory (default: per-OS app data, or $KNOTT_HOME)
  --open             Open the console in a browser once it is ready
  --ai-sidecar       Also start the optional Python AI service

Environment:
  API_KEYS           key:role pairs, e.g. "s3cr3t:admin,ro-key:viewer"
  KNOTT_SECRET_KEY   Encryption key for stored credentials (generated if unset)
  WEBHOOK_SECRET     HMAC secret required on inbound webhooks
  OLLAMA_HOST        Where a local Ollama listens (default 127.0.0.1:11434)

Documentation: https://github.com/regnant-io/knott
`

func main() {
	log.SetFlags(log.Ltime)

	cmd := "serve"
	if len(os.Args) > 1 && !strings.HasPrefix(os.Args[1], "-") {
		cmd = os.Args[1]
		os.Args = append(os.Args[:1], os.Args[2:]...)
	}

	switch cmd {
	case "serve", "start":
		if err := serve(false); err != nil {
			log.Fatalf("knott: %v", err)
		}
	case "desktop", "app":
		if launchDesktopApp() {
			return
		}
		log.Printf("The KNOTT desktop app is not installed next to this binary — opening the console in your browser instead.")
		if err := serve(true); err != nil {
			log.Fatalf("knott: %v", err)
		}
	case "version", "-v", "--version":
		fmt.Printf("knott %s (commit %s, built %s, %s/%s)\n",
			version, commit, date, runtime.GOOS, runtime.GOARCH)
	case "home":
		home, err := platform.Home()
		if err != nil {
			log.Fatalf("knott: %v", err)
		}
		fmt.Println(home)
	case "help", "-h", "--help":
		fmt.Print(banner, "\n", helpText)
	default:
		fmt.Fprintf(os.Stderr, "knott: unknown command %q\n\n", cmd)
		fmt.Print(helpText)
		os.Exit(2)
	}
}

func serve(openBrowser bool) error {
	var (
		port      = flag.Int("port", envInt("PORT", envInt("ENGINE_PORT", 8002)), "console and API port")
		host      = flag.String("host", envStr("KNOTT_BIND_HOST", "127.0.0.1"), "bind address")
		home      = flag.String("home", "", "state directory")
		open      = flag.Bool("open", false, "open the console in a browser once ready")
		aiSidecar = flag.Bool("ai-sidecar", os.Getenv("KNOTT_AI_SIDECAR") == "1", "start the optional Python AI service")
		quiet     = flag.Bool("quiet", false, "suppress the startup banner")
		_         = flag.Bool("no-ai", false, "deprecated: the Python AI service no longer starts by default")
	)
	flag.Parse()

	if !*quiet {
		fmt.Print(banner)
	}
	inst, err := app.Start(app.Options{
		Port: *port, Host: *host, Home: *home, Runtime: "server",
		Version: version, AISidecar: *aiSidecar,
	})
	if err != nil {
		return err
	}
	if *open || openBrowser {
		_ = platform.OpenBrowser(inst.URL)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-inst.Failures():
		inst.Shutdown(5 * time.Second)
		return err
	case <-stop:
		inst.Shutdown(20 * time.Second)
		return nil
	}
}

// launchDesktopApp starts the native desktop application when it sits next to
// this binary (as it does in every installer), reporting whether it did.
func launchDesktopApp() bool {
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	// The installers put this binary in a bin/ folder beneath the app, because
	// "KNOTT.exe" and "knott.exe" cannot share a directory on the
	// case-insensitive file systems of Windows and macOS.
	dir := filepath.Dir(exe)
	var candidates []string
	switch runtime.GOOS {
	case "windows":
		candidates = []string{filepath.Join(dir, "..", "KNOTT.exe")}
	case "darwin":
		candidates = []string{filepath.Join(dir, "..", "..", "MacOS", "KNOTT"), "/Applications/KNOTT.app"}
	default:
		candidates = []string{filepath.Join(dir, "knott-desktop"), "/usr/bin/knott-desktop", "/opt/knott/knott-desktop"}
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err != nil {
			continue
		}
		var cmd *exec.Cmd
		if strings.HasSuffix(c, ".app") {
			cmd = exec.Command("open", c)
		} else {
			cmd = exec.Command(c)
		}
		if cmd.Start() == nil {
			return true
		}
	}
	return false
}

func envStr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return fallback
}
