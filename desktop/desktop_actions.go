package main

import (
	"fmt"
	"path/filepath"

	"github.com/regnant/knott/internal/platform"
)

// These actions are shared by the integrated Windows menu and the native menu
// on other platforms. They are bound to the web view only for desktop use.
func (d *Desktop) OpenInBrowser() error {
	if d.inst == nil {
		return fmt.Errorf("KNOTT is not ready")
	}
	return platform.OpenBrowser(d.inst.URL)
}

func (d *Desktop) ShowDataFolder() error {
	home, err := platform.Home()
	if err != nil {
		return err
	}
	return platform.OpenBrowser(home)
}

func (d *Desktop) ShowLogs() error {
	home, err := platform.Home()
	if err != nil {
		return err
	}
	return platform.OpenBrowser(filepath.Join(home, "logs"))
}

func (d *Desktop) About() string {
	where := ""
	if d.inst != nil {
		where = "\n\nAPI and webhooks: " + d.inst.URL + "\nData: " + d.inst.StateDir
	}
	return fmt.Sprintf("KNOTT %s\nSovereign workflow orchestration.%s", version, where)
}
