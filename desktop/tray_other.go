//go:build !windows

package main

func (d *Desktop) startTray()                      {}
func (d *Desktop) stopTray()                       {}
func (d *Desktop) Notify(title, body string) error { return nil }
