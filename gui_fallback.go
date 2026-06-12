//go:build !cgo

package main

import (
	"os/exec"
	"runtime"
)

func openGUI(url string, _ chan<- struct{}) {
	fallbackGUIStarted = true
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}

var fallbackGUIStarted bool

func hasGUI() bool {
	return fallbackGUIStarted
}

func navigateGUI(url string) {
	// fallback 模式下由于是调用外部浏览器，所以这里不做实质处理
}
