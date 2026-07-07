//go:build windows

package main

import "syscall"

// platformUILanguage 在 Windows 下通过 kernel32.GetUserDefaultUILanguage 获取系统 UI 语言。
func platformUILanguage() string {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("GetUserDefaultUILanguage")
	ret, _, _ := proc.Call()
	langID := uint16(ret)
	// Primary language ID 位于低 10 位
	primary := langID & 0x3ff
	switch primary {
	case 0x0004: // LANG_CHINESE
		return "zh"
	case 0x0009: // LANG_ENGLISH
		return "en"
	}
	return ""
}
