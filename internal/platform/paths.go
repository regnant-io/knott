// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// Package platform resolves the per-OS locations and defaults a self-contained
// KNOTT install needs: where state lives, where the secret key is kept, and how
// to open the user's browser.
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
	// Only a directory that already holds KNOTT state counts: an app launched
	// from Explorer or Finder inherits whatever working directory it was given,
	// and an unrelated ./data there must not become the state directory.
	if looksLikeState(".") {
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

// looksLikeState reports whether dir holds a KNOTT data directory.
func looksLikeState(dir string) bool {
	for _, f := range []string{"workflows.db", "runs.db"} {
		if _, err := os.Stat(filepath.Join(dir, "data", f)); err == nil {
			return true
		}
	}
	return false
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
