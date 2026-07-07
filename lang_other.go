//go:build !windows

package main

// platformUILanguage 在非 Windows 平台无系统级 UI 语言 API，
// 语言判断已在 detectSystemLanguage 中通过环境变量完成，这里返回空。
func platformUILanguage() string {
	return ""
}
