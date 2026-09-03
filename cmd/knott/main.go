// Command knott is the all-in-one KNOTT server.
//
// One executable, one port, no runtime dependencies: it runs the workflow
// registry, execution engine, human-task service and agent registry inside a
// single process, serves the embedded web console, and keeps its state in a
// per-user directory it creates on first run. That is what makes a KNOTT
// release a file you download and run on Windows, macOS or Linux rather than a
// stack you deploy.
//
// The same code still supports a distributed deployment: run the per-service
// binaries (knott-registry, knott-engine, knott-tasks, knott-agents) and point
// them at each other with the documented environment variables.
package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/regnant/knott/internal/agents"
	"github.com/regnant/knott/internal/execution"
	"github.com/regnant/knott/internal/humantask"
	"github.com/regnant/knott/internal/platform"
	"github.com/regnant/knott/internal/registry"
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
  desktop    Run the platform and open it in a dedicated app window
  version    Print version and build information
  home       Print the directory KNOTT stores its data in
  help       Show this message

Flags:
  --port int         Port for the console and API (default 8002, or $PORT)
  --host string      Address to bind (default 127.0.0.1; use 0.0.0.0 to expose)
  --home string      State directory (default: per-OS app data, or $KNOTT_HOME)
  --open             Open the console in a browser once it is ready
  --no-ai            Do not start the optional AI decision engine

Environment:
  API_KEYS           key:role pairs, e.g. "s3cr3t:admin,ro-key:viewer"
  KNOTT_SECRET_KEY   Encryption key for stored credentials (generated if unset)
  WEBHOOK_SECRET     HMAC secret required on inbound webhooks

Documentation: https://github.com/regnant/knott
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

func serve(desktop bool) error {
	var (
		port  = flag.Int("port", envInt("PORT", envInt("ENGINE_PORT", 8002)), "console and API port")
		host  = flag.String("host", envStr("KNOTT_BIND_HOST", "127.0.0.1"), "bind address")
		home  = flag.String("home", "", "state directory")
		open  = flag.Bool("open", false, "open the console in a browser once ready")
		noAI  = flag.Bool("no-ai", false, "do not start the AI decision engine")
		quiet = flag.Bool("quiet", false, "suppress the startup banner")
	)
	flag.Parse()

	if *home != "" {
		os.Setenv("KNOTT_HOME", *home)
	}
	stateDir, err := platform.Home()
	if err != nil {
		return fmt.Errorf("resolve state directory: %w", err)
	}
	dataDir := filepath.Join(stateDir, "data")
	if err := os.MkdirAll(dataDir, 0o750); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	key, err := platform.EnsureSecretKey(stateDir)
	if err != nil {
		return fmt.Errorf("provision secret key: %w", err)
	}
	os.Setenv("KNOTT_SECRET_KEY", key)

	// Internal services bind to loopback on ports chosen at startup, so nothing
	// but the console port is reachable and two KNOTT instances on one machine
	// do not collide.
	internal, err := reserveLoopbackPorts(4)
	if err != nil {
		return fmt.Errorf("reserve internal ports: %w", err)
	}
	registryPort, taskPort, agentPort, aiPort := internal[0], internal[1], internal[2], internal[3]

	setDefault("REGISTRY_DB", filepath.Join(dataDir, "workflows.db"))
	setDefault("ENGINE_DB", filepath.Join(dataDir, "runs.db"))
	setDefault("TASK_DB", filepath.Join(dataDir, "tasks.db"))
	setDefault("AGENT_DB", filepath.Join(dataDir, "agents.db"))

	os.Setenv("REGISTRY_PORT", strconv.Itoa(registryPort))
	os.Setenv("TASK_PORT", strconv.Itoa(taskPort))
	os.Setenv("AGENT_PORT", strconv.Itoa(agentPort))
	os.Setenv("ENGINE_PORT", strconv.Itoa(*port))
	os.Setenv("ENGINE_BIND_HOST", *host)
	os.Setenv("REGISTRY_URL", fmt.Sprintf("http://127.0.0.1:%d", registryPort))
	os.Setenv("HUMAN_TASK_URL", fmt.Sprintf("http://127.0.0.1:%d", taskPort))
	os.Setenv("AGENT_URL", fmt.Sprintf("http://127.0.0.1:%d", agentPort))
	setDefault("AI_DECISION_URL", fmt.Sprintf("http://127.0.0.1:%d", aiPort))
	setDefault("EXECUTION_ENGINE_URL", fmt.Sprintf("http://127.0.0.1:%d", *port))

	if !*quiet {
		fmt.Print(banner)
	}

	failures := make(chan error, 4)
	start := func(name string, run func() error) {
		go func() {
			if err := run(); err != nil && !errors.Is(err, http.ErrServerClosed) {
				failures <- fmt.Errorf("%s: %w", name, err)
			}
		}()
	}
	start("workflow-registry", registry.Run)
	start("human-task-service", humantask.Run)
	start("agent-integration", agents.Run)

	var aiCmd *exec.Cmd
	if !*noAI {
		aiCmd = startAIEngine(aiPort, dataDir)
	}

	// Let the internal services bind before the engine starts proxying to them.
	waitForPorts([]int{registryPort, taskPort, agentPort}, 8*time.Second)
	start("execution-engine", execution.Run)

	consoleURL := fmt.Sprintf("http://%s:%d", displayHost(*host), *port)
	if !waitForPorts([]int{*port}, 15*time.Second) {
		shutdownAI(aiCmd)
		select {
		case err := <-failures:
			return err
		default:
			return errors.New("execution engine did not start listening")
		}
	}

	log.Printf("KNOTT %s ready → %s", version, consoleURL)
	log.Printf("State directory: %s", stateDir)
	if os.Getenv("API_KEYS") == "" && os.Getenv("API_TOKEN") == "" && *host != "127.0.0.1" {
		log.Printf("⚠  Bound to %s with no API_KEYS set — the API is open to the network.", *host)
	}

	if desktop {
		profile := filepath.Join(stateDir, "app-window")
		if !platform.OpenAppWindow(consoleURL, profile) {
			_ = platform.OpenBrowser(consoleURL)
		}
	} else if *open {
		_ = platform.OpenBrowser(consoleURL)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt, syscall.SIGTERM)
	select {
	case err := <-failures:
		shutdownAI(aiCmd)
		return err
	case <-stop:
		log.Printf("Shutting down — waiting for in-flight runs…")
		// Runs are checkpointed, so anything still going at the deadline resumes
		// on the next start. Draining first simply avoids the wait.
		if !execution.WaitForBackgroundRuns(20 * time.Second) {
			log.Printf("Some runs were still in flight; they will resume on next start.")
		}
		shutdownAI(aiCmd)
		return nil
	}
}

// startAIEngine launches the optional Python AI decision engine when a suitable
// interpreter is present. KNOTT runs without it — the engine falls back to its
// deterministic rule-based decisions — so a missing interpreter is logged and
// never fatal.
func startAIEngine(port int, dataDir string) *exec.Cmd {
	script := findAIScript()
	if script == "" {
		return nil
	}
	python := findPython()
	if python == "" {
		log.Printf("AI decision engine: no Python interpreter found — using built-in rule-based decisions")
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
	if err := cmd.Start(); err != nil {
		log.Printf("AI decision engine: %v — using built-in rule-based decisions", err)
		return nil
	}
	return cmd
}

func shutdownAI(cmd *exec.Cmd) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = cmd.Process.Kill()
	_, _ = cmd.Process.Wait()
}

func findAIScript() string {
	candidates := []string{
		filepath.Join("services", "ai-decision-engine", "main.py"),
		filepath.Join("..", "services", "ai-decision-engine", "main.py"),
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "ai-decision-engine", "main.py"),
			filepath.Join(dir, "services", "ai-decision-engine", "main.py"),
			filepath.Join(dir, "..", "services", "ai-decision-engine", "main.py"),
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

func findPython() string {
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

// reserveLoopbackPorts asks the OS for n free loopback ports. They are released
// immediately and re-bound by the services a moment later; the race is
// acceptable on a loopback interface and avoids hard-coding ports that a second
// KNOTT instance would fight over.
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

// displayHost turns a wildcard bind address into something a person can click.
func displayHost(bind string) string {
	if bind == "" || bind == "0.0.0.0" || bind == "::" {
		return "localhost"
	}
	return bind
}
