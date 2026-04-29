//go:build cgo

package main

import (
	"github.com/webview/webview_go"
)

func openGUI(url string) {
	w := webview.New(true)
	defer w.Destroy()
	w.SetTitle("DSPlus - DeepSeek V4 Proxy")
	w.SetSize(1200, 800, webview.HintNone)
	w.Navigate(url)
	w.Run()
}
