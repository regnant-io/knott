//go:build windows

package main

import (
	_ "embed"
	"log"
	"os"
	"path/filepath"

	"git.sr.ht/~jackmordaunt/go-toast/v2"
	"github.com/getlantern/systray"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed tray.ico
var trayIcon []byte

func (d *Desktop) startTray() {
	go systray.Run(func() {
		systray.SetIcon(trayIcon)
		systray.SetTooltip("KNOTT — workflow engine")
		open := systray.AddMenuItem("Open KNOTT", "Show the KNOTT window")
		systray.AddSeparator()
		quit := systray.AddMenuItem("Quit KNOTT", "Stop the workflow engine and exit")
		d.trayReady.Store(true)
		go func() {
			for {
				select {
				case <-open.ClickedCh:
					if d.ctx != nil {
						wruntime.WindowUnminimise(d.ctx)
						wruntime.WindowShow(d.ctx)
					}
				case <-quit.ClickedCh:
					d.quitting.Store(true)
					if d.ctx != nil {
						wruntime.Quit(d.ctx)
					}
					return
				}
			}
		}()
	}, func() { d.trayReady.Store(false) })
}

func (d *Desktop) stopTray() { systray.Quit() }

// Notify sends a Windows notification for a state change that matters outside
// the window. In-app toasts remain available for routine feedback.
func (d *Desktop) Notify(title, body string) error {
	if len(title) > 120 {
		title = title[:120]
	}
	if len(body) > 500 {
		body = body[:500]
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	icon := filepath.Join(filepath.Dir(exe), "knott-notification.png")
	if _, err := os.Stat(icon); err != nil {
		icon = ""
	}
	if err := toast.SetAppData(toast.AppData{
		AppID: "KNOTT", GUID: "91e97920-0b9c-4a1f-84df-f1874a9e38db",
		IconPath: icon,
	}); err != nil {
		return err
	}
	n := toast.Notification{AppID: "KNOTT", Title: title, Body: body, Icon: icon}
	if err := n.Push(); err != nil {
		log.Printf("desktop notification failed: %v", err)
		return err
	}
	return nil
}
