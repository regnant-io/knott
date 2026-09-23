// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

// Command knott-desktop is KNOTT as a native desktop application.
//
// It runs the whole platform in-process — the same code as `knott serve` —
// and shows the console in a native window through the operating system's own
// web view (WebView2 on Windows, WKWebView on macOS, WebKitGTK on Linux). There
// is no browser to find, no second engine in the download, and no terminal:
// the app has a real window, menus, a single-instance lock, and it shuts the
// platform down cleanly when the window closes.
//
// The window talks to the platform through Wails' asset server, which hands
// every request to a reverse proxy in front of the local API. The console
// therefore uses the same relative URLs as in a browser, and the API keeps
// answering on loopback for webhooks, the CLI and anything else on the
// machine.
//
// Build (see Makefile target `desktop`):
//
//	go build -tags desktop,production -ldflags "-H windowsgui" .   # Windows
//	CGO_ENABLED=1 go build -tags desktop,production .              # macOS
//	CGO_ENABLED=1 go build -tags desktop,production,webkit2_41 .   # Linux
package main

import (
	"context"
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"time"

	"github.com/regnant/knott/internal/app"
	"github.com/regnant/knott/internal/platform"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/linux"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// Build metadata, stamped by the release build with -ldflags -X.
var (
	version = "dev"
	commit  = "none"
)

const (
	appID    = "io.regnant.knott"
	docsURL  = "https://github.com/regnant-io/knott#readme"
	issueURL = "https://github.com/regnant-io/knott/issues/new/choose"
)

// Desktop holds the running platform and the window's context.
type Desktop struct {
	ctx       context.Context
	inst      *app.Instance
	quitting  atomic.Bool
	trayReady atomic.Bool
}

func main() {
	home, _ := platform.Home()
	logFile := setupLogging(home)
	if logFile != nil {
		defer logFile.Close()
	}
	log.Printf("KNOTT desktop %s (%s) starting on %s/%s", version, commit, runtime.GOOS, runtime.GOARCH)

	d := &Desktop{}
	d.startTray()
	window := initialWindowBounds()
	log.Printf("window size %dx%d (minimum %dx%d)", window.Width, window.Height, window.MinWidth, window.MinHeight)
	var handler http.Handler
	inst, err := app.Start(app.Options{
		// The usual port keeps webhook URLs stable between launches; when
		// another KNOTT already has it, fall back rather than refuse to open.
		Port: 8002, FallbackPort: true, Host: "127.0.0.1",
		Runtime: "desktop", Version: version,
	})
	if err != nil {
		log.Printf("platform failed to start: %v", err)
		handler = startupError(err, home)
	} else {
		d.inst = inst
		handler = proxyTo(inst.URL)
	}

	err = wails.Run(&options.App{
		Title:            "KNOTT",
		Width:            window.Width,
		Height:           window.Height,
		MinWidth:         window.MinWidth,
		MinHeight:        window.MinHeight,
		Frameless:        runtime.GOOS == "windows", // KNOTT's integrated Windows title strip
		BackgroundColour: &options.RGBA{R: 250, G: 251, B: 249, A: 255},
		AssetServer:      &assetserver.Options{Handler: handler},
		Menu:             d.menu(),
		Bind:             []interface{}{d},
		OnStartup:        func(ctx context.Context) { d.ctx = ctx },
		OnBeforeClose: func(ctx context.Context) bool {
			if runtime.GOOS != "windows" || d.quitting.Load() || !d.trayReady.Load() {
				return false
			}
			wruntime.WindowHide(ctx)
			return true
		},
		OnShutdown: func(context.Context) {
			d.stopTray()
			if d.inst != nil {
				d.inst.Shutdown(20 * time.Second)
			}
		},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: appID,
			// Launching KNOTT again brings the existing window forward
			// instead of starting a second platform on the same data.
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if d.ctx != nil {
					wruntime.WindowUnminimise(d.ctx)
					wruntime.WindowShow(d.ctx)
				}
			},
		},
		EnableDefaultContextMenu: true, // cut, copy and paste in text fields
		Windows: &windows.Options{
			WebviewIsTransparent:              false,
			WindowIsTranslucent:               false,
			DisableWindowIcon:                 false,
			DisableFramelessWindowDecorations: false,
			Theme:                             windows.SystemDefault,
			// Keep the web view's cache and storage with KNOTT's data rather
			// than next to the executable, which may be read-only.
			WebviewUserDataPath: filepath.Join(home, "webview"),
		},
		Mac: &mac.Options{
			TitleBar:   mac.TitleBarDefault(),
			Appearance: mac.DefaultAppearance,
			About: &mac.AboutInfo{
				Title:   "KNOTT",
				Message: fmt.Sprintf("Version %s\nSovereign workflow orchestration.\n© 2026 Regnant", version),
			},
		},
		Linux: &linux.Options{
			ProgramName:      "knott",
			WebviewGpuPolicy: linux.WebviewGpuPolicyOnDemand,
		},
	})
	if err != nil {
		log.Printf("window failed: %v", err)
		if d.inst != nil {
			d.inst.Shutdown(10 * time.Second)
		}
		os.Exit(1)
	}
}

// proxyTo forwards the window's requests to the local API.
func proxyTo(target string) http.Handler {
	u, _ := url.Parse(target)
	p := httputil.NewSingleHostReverseProxy(u)
	base := p.Director
	p.Director = func(r *http.Request) {
		base(r)
		r.Host = u.Host
		// The request came from the app's own window, not from a web page;
		// the API's origin checks are for browsers.
		for _, h := range []string{"Origin", "Referer", "Sec-Fetch-Site", "Sec-Fetch-Mode", "Sec-Fetch-Dest"} {
			r.Header.Del(h)
		}
	}
	// Stream responses (long AI calls, run polling) instead of buffering.
	p.FlushInterval = 100 * time.Millisecond
	p.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadGateway)
		fmt.Fprintf(w, `{"error":{"code":"PLATFORM_UNAVAILABLE","message":%q}}`, "KNOTT's engine is not responding: "+err.Error())
	}
	return p
}

// startupError serves a page explaining why the platform could not start,
// so a failed launch is a readable window rather than a vanished process.
func startupError(err error, home string) http.Handler {
	logPath := filepath.Join(home, "logs", "knott-desktop.log")
	page := `<!doctype html><meta charset="utf-8"><title>KNOTT</title>
<style>body{margin:0;min-height:100vh;display:grid;place-items:center;font:14px/1.6 -apple-system,"Segoe UI",Roboto,sans-serif;background:#fafbf9;color:#202a25}
main{max-width:34rem;padding:2rem}h1{font-size:1.2rem}code{background:#eef0eb;padding:.15em .4em;border-radius:4px;word-break:break-all}</style>
<main><h1>KNOTT could not start</h1><p>` + html.EscapeString(err.Error()) + `</p>
<p>Details are in <code>` + html.EscapeString(logPath) + `</code>.</p>
<p>If another copy of KNOTT is running, close it and open KNOTT again.</p></main>`
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		io.WriteString(w, page)
	})
}

// setupLogging sends logs to a file: a windowed app has no console, and a log
// is the first thing anyone asks for when something goes wrong.
func setupLogging(home string) *os.File {
	if home == "" {
		return nil
	}
	dir := filepath.Join(home, "logs")
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil
	}
	path := filepath.Join(dir, "knott-desktop.log")
	if st, err := os.Stat(path); err == nil && st.Size() > 20<<20 {
		_ = os.Rename(path, path+".1")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return nil
	}
	log.SetOutput(f)
	log.SetFlags(log.LstdFlags)
	return f
}

// menu builds the native menu bar.
func (d *Desktop) menu() *menu.Menu {
	if runtime.GOOS == "windows" {
		return nil // the same actions live beside KNOTT's window controls
	}
	m := menu.NewMenu()
	if runtime.GOOS == "darwin" {
		m.Append(menu.AppMenu())
	}

	file := m.AddSubmenu("File")
	file.AddText("Open in Browser", keys.CmdOrCtrl("b"), func(*menu.CallbackData) {
		if d.inst != nil {
			_ = platform.OpenBrowser(d.inst.URL)
		}
	})
	file.AddText("Show Data Folder", nil, func(*menu.CallbackData) {
		if home, err := platform.Home(); err == nil {
			_ = platform.OpenBrowser(home)
		}
	})
	file.AddText("Show Logs", nil, func(*menu.CallbackData) {
		if home, err := platform.Home(); err == nil {
			_ = platform.OpenBrowser(filepath.Join(home, "logs"))
		}
	})
	if runtime.GOOS != "darwin" {
		file.AddSeparator()
		file.AddText("Quit", keys.CmdOrCtrl("q"), func(*menu.CallbackData) { wruntime.Quit(d.ctx) })
	}

	// The Edit menu is what makes Cmd+C / Cmd+V work in a macOS web view.
	m.Append(menu.EditMenu())

	view := m.AddSubmenu("View")
	view.AddText("Reload", keys.CmdOrCtrl("r"), func(*menu.CallbackData) { wruntime.WindowReloadApp(d.ctx) })
	view.AddText("Toggle Full Screen", keys.Key("f11"), func(*menu.CallbackData) {
		if wruntime.WindowIsFullscreen(d.ctx) {
			wruntime.WindowUnfullscreen(d.ctx)
		} else {
			wruntime.WindowFullscreen(d.ctx)
		}
	})

	if runtime.GOOS == "darwin" {
		m.Append(menu.WindowMenu())
	}

	help := m.AddSubmenu("Help")
	help.AddText("Documentation", nil, func(*menu.CallbackData) { wruntime.BrowserOpenURL(d.ctx, docsURL) })
	help.AddText("Report an Issue", nil, func(*menu.CallbackData) { wruntime.BrowserOpenURL(d.ctx, issueURL) })
	help.AddSeparator()
	help.AddText("About KNOTT", nil, func(*menu.CallbackData) {
		where := ""
		if d.inst != nil {
			where = "\n\nAPI and webhooks: " + d.inst.URL + "\nData: " + d.inst.StateDir
		}
		_, _ = wruntime.MessageDialog(d.ctx, wruntime.MessageDialogOptions{
			Type:    wruntime.InfoDialog,
			Title:   "About KNOTT",
			Message: strings.TrimSpace(fmt.Sprintf("KNOTT %s\nSovereign workflow orchestration.%s", version, where)),
		})
	})
	return m
}
