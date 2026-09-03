// Package platform resolves the per-OS locations and defaults a self-contained
// KNOTT install needs: where state lives, where the secret key is kept, and how
// to open a browser window.
package platform

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Home returns the KNOTT state directory, creating it if necessary.
//
// KNOTT_HOME overrides everything. Otherwise the convention of the host OS is
// followed so an installed copy behaves like a native application:
//
//	Windows  %LOCALAPPDATA%\KNOTT
//	macOS    ~/Library/Application Support/KNOTT
//	Linux    $XDG_DATA_HOME/knott  (default ~/.local/share/knott)
//
// A ./data directory next to the binary wins over all of these when it already
// exists, so unpacking a release into a folder keeps that folder portable.
func Home() (string, error) {
	if v := strings.TrimSpace(os.Getenv("KNOTT_HOME")); v != "" {
		return v, os.MkdirAll(v, 0o750)
	}
	if exe, err := os.Executable(); err == nil {
		portable := filepath.Join(filepath.Dir(exe), "data")
		if st, err := os.Stat(portable); err == nil && st.IsDir() {
			return filepath.Dir(portable), nil
		}
	}
	// A repository checkout keeps its state in ./data, matching the dev scripts.
	if st, err := os.Stat("data"); err == nil && st.IsDir() {
		abs, err := filepath.Abs(".")
		if err == nil {
			return abs, nil
		}
	}
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("LOCALAPPDATA")
		if base == "" {
			base = os.Getenv("APPDATA")
		}
		base = filepath.Join(base, "KNOTT")
	case "darwin":
		h, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(h, "Library", "Application Support", "KNOTT")
	default:
		if x := os.Getenv("XDG_DATA_HOME"); x != "" {
			base = filepath.Join(x, "knott")
		} else {
			h, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(h, ".local", "share", "knott")
		}
	}
	return base, os.MkdirAll(base, 0o750)
}

// EnsureSecretKey returns the key used to encrypt stored credentials, minting
// and persisting one on first run.
//
// Generating a key beats defaulting to a constant: an operator who never reads
// the docs still gets real encryption at rest rather than obfuscation. The file
// is owner-only, and KNOTT_SECRET_KEY still wins when set, so orchestrated
// deployments keep managing the key themselves.
func EnsureSecretKey(home string) (string, error) {
	if v := strings.TrimSpace(os.Getenv("KNOTT_SECRET_KEY")); v != "" {
		return v, nil
	}
	path := filepath.Join(home, "secret.key")
	if b, err := os.ReadFile(path); err == nil {
		if key := strings.TrimSpace(string(b)); key != "" {
			return key, nil
		}
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	key := hex.EncodeToString(buf)
	if err := os.MkdirAll(home, 0o750); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, []byte(key+"\n"), 0o600); err != nil {
		return "", err
	}
	return key, nil
}

// OpenBrowser opens url in the user's default browser. Failure is not fatal —
// callers print the URL as a fallback.
func OpenBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// OpenAppWindow launches a Chromium-family browser in app mode, giving KNOTT a
// chromeless window that looks and behaves like a native desktop app without
// shipping a second browser engine inside the download. It reports whether a
// suitable browser was found; callers fall back to OpenBrowser.
func OpenAppWindow(url, profileDir string) bool {
	for _, bin := range chromiumCandidates() {
		path, err := exec.LookPath(bin)
		if err != nil {
			if _, statErr := os.Stat(bin); statErr != nil {
				continue
			}
			path = bin
		}
		cmd := exec.Command(path,
			"--app="+url,
			"--user-data-dir="+profileDir,
			"--no-first-run",
			"--no-default-browser-check",
			menuflag,
		)
		if err := cmd.Start(); err == nil {
			return true
		}
	}
	return false
}

// menuflag keeps the app window free of the browser's own new-tab affordances.
const menuflag = "--disable-features=Translate,AutofillServerCommunication"

func chromiumCandidates() []string {
	switch runtime.GOOS {
	case "windows":
		pf := os.Getenv("ProgramFiles")
		pf86 := os.Getenv("ProgramFiles(x86)")
		local := os.Getenv("LOCALAPPDATA")
		return []string{
			filepath.Join(pf, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(pf86, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(local, "Google", "Chrome", "Application", "chrome.exe"),
			filepath.Join(pf86, "Microsoft", "Edge", "Application", "msedge.exe"),
			filepath.Join(pf, "Microsoft", "Edge", "Application", "msedge.exe"),
			"chrome.exe", "msedge.exe",
		}
	case "darwin":
		return []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			"/Applications/Microsoft Edge.app/Contents/MacOS/Microsoft Edge",
			"/Applications/Chromium.app/Contents/MacOS/Chromium",
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
		}
	default:
		return []string{
			"google-chrome", "google-chrome-stable", "chromium", "chromium-browser",
			"microsoft-edge", "brave-browser", "vivaldi",
		}
	}
}
