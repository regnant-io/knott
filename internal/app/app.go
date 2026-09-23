// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// Package app starts the whole KNOTT platform inside one process.
//
// The workflow registry, execution engine, human-task service and agent
// registry run as goroutines behind one public port, with state in a per-user
// directory created on first run. Both front ends use it: the `knott` command
// line (serve) and the native desktop application, which embeds the same
// platform behind a real window instead of launching a browser.
package app

import (
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/regnant/knott/internal/agents"
	"github.com/regnant/knott/internal/execution"
	"github.com/regnant/knott/internal/humantask"
	"github.com/regnant/knott/internal/platform"
	"github.com/regnant/knott/internal/registry"
)

// Options configures a platform instance.
type Options struct {
	// Port is the console/API port. Zero picks a free one; with FallbackPort
	// set, a busy Port falls back to a free one instead of failing.
	Port         int
	FallbackPort bool
	// Host is the bind address (default 127.0.0.1).
	Host string
	// Home overrides the state directory.
	Home string
	// Runtime labels the deployment for the console: "server" or "desktop".
	Runtime string
	Version string
	// AISidecar starts the optional Python decision service. The engine does
	// everything in-process without it; the sidecar exists for deployments
	// that already run it.
	AISidecar bool
}

// Instance is a running platform.
type Instance struct {
	// URL is where the console and API answer, e.g. http://127.0.0.1:8002.
	URL      string
	Port     int
	StateDir string

	failures chan error
	aiCmd    *exec.Cmd
	stopOnce sync.Once
}

// Start brings the platform up and returns once the API answers.
func Start(opts Options) (*Instance, error) {
	if opts.Host == "" {
		opts.Host = "127.0.0.1"
	}
	if opts.Runtime == "" {
		opts.Runtime = "server"
	}
	if opts.Version == "" {
		opts.Version = "dev"
	}
	os.Setenv("KNOTT_VERSION", opts.Version)
	os.Setenv("KNOTT_RUNTIME", opts.Runtime)
	if opts.Home != "" {
		os.Setenv("KNOTT_HOME", opts.Home)
	}

	stateDir, err := platform.Home()
	if err != nil {
		return nil, fmt.Errorf("resolve state directory: %w", err)
	}
	dataDir := filepath.Join(stateDir, "data")
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	key, err := platform.EnsureSecretKey(stateDir)
	if err != nil {
		return nil, fmt.Errorf("provision secret key: %w", err)
	}
	os.Setenv("KNOTT_SECRET_KEY", key)

	port, err := choosePort(opts.Host, opts.Port, opts.FallbackPort)
	if err != nil {
		return nil, err
	}

	// Internal services bind to loopback on ports chosen at startup, so nothing
	// but the console port is reachable and two instances do not collide.
	internal, err := reserveLoopbackPorts(4)
	if err != nil {
		return nil, fmt.Errorf("reserve internal ports: %w", err)
	}
	registryPort, taskPort, agentPort, aiPort := internal[0], internal[1], internal[2], internal[3]

	setDefault("REGISTRY_DB", filepath.Join(dataDir, "workflows.db"))
	setDefault("ENGINE_DB", filepath.Join(dataDir, "runs.db"))
	setDefault("TASK_DB", filepath.Join(dataDir, "tasks.db"))
	setDefault("AGENT_DB", filepath.Join(dataDir, "agents.db"))

	os.Setenv("REGISTRY_PORT", strconv.Itoa(registryPort))
	os.Setenv("TASK_PORT", strconv.Itoa(taskPort))
	os.Setenv("AGENT_PORT", strconv.Itoa(agentPort))
	os.Setenv("ENGINE_PORT", strconv.Itoa(port))
	os.Setenv("ENGINE_BIND_HOST", opts.Host)
	os.Setenv("REGISTRY_URL", fmt.Sprintf("http://127.0.0.1:%d", registryPort))
	os.Setenv("HUMAN_TASK_URL", fmt.Sprintf("http://127.0.0.1:%d", taskPort))
	os.Setenv("AGENT_URL", fmt.Sprintf("http://127.0.0.1:%d", agentPort))
	os.Setenv("EXECUTION_ENGINE_URL", fmt.Sprintf("http://127.0.0.1:%d", port))
	os.Setenv("KNOTT_PUBLIC_URL", fmt.Sprintf("http://%s:%d", DisplayHost(opts.Host), port))

	inst := &Instance{
		URL:      fmt.Sprintf("http://%s:%d", DisplayHost(opts.Host), port),
		Port:     port,
		StateDir: stateDir,
		failures: make(chan error, 4),
	}
	run := func(name string, fn func() error) {
		go func() {
			if err := fn(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				inst.failures <- fmt.Errorf("%s: %w", name, err)
			}
		}()
	}
	run("workflow-registry", registry.Run)
	run("human-task-service", humantask.Run)
	run("agent-integration", agents.Run)

	if opts.AISidecar {
		if cmd := startAIEngine(aiPort, dataDir); cmd != nil {
			inst.aiCmd = cmd
			setDefault("AI_DECISION_URL", fmt.Sprintf("http://127.0.0.1:%d", aiPort))
		}
	}

	// Let the internal services bind before the engine starts proxying to them.
	waitForPorts([]int{registryPort, taskPort, agentPort}, 8*time.Second)
	run("execution-engine", execution.Run)

	if !waitForPorts([]int{port}, 20*time.Second) {
		inst.stopAI()
		select {
		case err := <-inst.failures:
			return nil, err
		default:
			return nil, errors.New("execution engine did not start listening")
		}
	}
	log.Printf("KNOTT %s ready → %s", opts.Version, inst.URL)
	log.Printf("State directory: %s", stateDir)
	if os.Getenv("API_KEYS") == "" && os.Getenv("API_TOKEN") == "" && !isLoopback(opts.Host) {
		log.Printf("⚠  Bound to %s with no API_KEYS set — the API is open to the network.", opts.Host)
	}
	return inst, nil
}

// Failures reports a service that stopped unexpectedly.
func (i *Instance) Failures() <-chan error { return i.failures }

// Shutdown drains in-flight runs for up to timeout and stops the sidecar.
// Runs are checkpointed, so anything still going resumes on the next start.
func (i *Instance) Shutdown(timeout time.Duration) {
	i.stopOnce.Do(func() {
		log.Printf("Shutting down — waiting for in-flight runs…")
		if !execution.WaitForBackgroundRuns(timeout) {
			log.Printf("Some runs were still in flight; they will resume on next start.")
		}
		i.stopAI()
	})
}

func (i *Instance) stopAI() {
	if i.aiCmd == nil || i.aiCmd.Process == nil {
		return
	}
	_ = i.aiCmd.Process.Kill()
	_, _ = i.aiCmd.Process.Wait()
}

// choosePort returns a port to serve on: the one asked for, or a free one.
func choosePort(host string, want int, fallback bool) (int, error) {
	if want > 0 {
		l, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(want)))
		if err == nil {
			l.Close()
			return want, nil
		}
		if !fallback {
			return 0, fmt.Errorf("port %d is already in use — is KNOTT already running? Pass --port to use another (%v)", want, err)
		}
		log.Printf("Port %d is busy; picking a free one", want)
	}
	l, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, fmt.Errorf("find a free port: %w", err)
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

// startAIEngine launches the optional Python AI decision engine when an
// interpreter and the script are present. A missing one is logged, not fatal.
func startAIEngine(port int, dataDir string) *exec.Cmd {
	script := findAIScript()
	if script == "" {
		log.Printf("AI sidecar requested but ai-decision-engine/main.py was not found — using the built-in engine")
		return nil
	}
	python := ""
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			python = p
			break
		}
	}
	if python == "" {
		log.Printf("AI sidecar requested but no Python interpreter was found — using the built-in engine")
		return nil
	}
	cmd := exec.Command(python, script)
	cmd.Env = append(os.Environ(),
		"AI_PORT="+strconv.Itoa(port),
		"PORT="+strconv.Itoa(port),
		"AI_CONFIG_PATH="+filepath.Join(dataDir, "ai-config.json"),
	)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	hideWindow(cmd)
	if err := cmd.Start(); err != nil {
		log.Printf("AI sidecar: %v — using the built-in engine", err)
		return nil
	}
	return cmd
}

// findAIScript locates the optional Python decision engine in the places each
// packaging format puts it. KNOTT_AI_SCRIPT overrides the search.
func findAIScript() string {
	if v := strings.TrimSpace(os.Getenv("KNOTT_AI_SCRIPT")); v != "" {
		if _, err := os.Stat(v); err == nil {
			return v
		}
	}
	candidates := []string{
		filepath.Join("services", "ai-decision-engine", "main.py"),
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "ai-decision-engine", "main.py"),
			filepath.Join(dir, "..", "share", "knott", "ai-decision-engine", "main.py"),
		)
	}
	for _, c := range candidates {
		if _, err := os.Stat(c); err == nil {
			if abs, err := filepath.Abs(c); err == nil {
				return abs
			}
			return c
		}
	}
	return ""
}

// reserveLoopbackPorts asks the OS for n free loopback ports. They are released
// immediately and re-bound by the services a moment later; the race is
// acceptable on a loopback interface and avoids hard-coded ports.
func reserveLoopbackPorts(n int) ([]int, error) {
	ports := make([]int, 0, n)
	listeners := make([]net.Listener, 0, n)
	defer func() {
		for _, l := range listeners {
			l.Close()
		}
	}()
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return nil, err
		}
		listeners = append(listeners, l)
		ports = append(ports, l.Addr().(*net.TCPAddr).Port)
	}
	return ports, nil
}

// waitForPorts blocks until every port accepts a connection, or the deadline
// passes. It reports whether all of them came up.
func waitForPorts(ports []int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for _, p := range ports {
		addr := fmt.Sprintf("127.0.0.1:%d", p)
		for {
			conn, err := net.DialTimeout("tcp", addr, 400*time.Millisecond)
			if err == nil {
				conn.Close()
				break
			}
			if time.Now().After(deadline) {
				return false
			}
			time.Sleep(75 * time.Millisecond)
		}
	}
	return true
}

func setDefault(key, value string) {
	if os.Getenv(key) == "" {
		os.Setenv(key, value)
	}
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// DisplayHost turns a wildcard bind address into something a person can click.
func DisplayHost(bind string) string {
	if bind == "" || bind == "0.0.0.0" || bind == "::" {
		return "localhost"
	}
	return bind
}
