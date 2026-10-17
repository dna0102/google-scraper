//go:build windows

package ui

import (
	"os"

	"golang.org/x/sys/windows"
)

func init() {
	var mode uint32
	h := windows.Handle(os.Stdout.Fd())
	if windows.GetConsoleMode(h, &mode) == nil {
		windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}
}
