//go:build cgo

package main

import (
	"syscall"
	"unicode/utf16"
	"unsafe"

	"github.com/webview/webview_go"
)

const (
	GWLP_WNDPROC    = ^uintptr(3)
	WM_CLOSE        = 0x0010
	WM_COMMAND      = 0x0111
	WM_TRAYICON     = 0x8001
	WM_LBUTTONDBLCLK = 0x0203
	WM_RBUTTONUP    = 0x0205

	SW_HIDE    = 0
	SW_RESTORE = 9

	NIM_ADD     = 0
	NIM_DELETE  = 2
	NIF_ICON    = 2
	NIF_MESSAGE = 1
	NIF_TIP     = 4

	MF_STRING    = 0
	MF_SEPARATOR = 0x0800

	TPM_RIGHTBUTTON = 2
	TPM_BOTTOMALIGN = 0x0020

	IDM_SHOW = 1001
	IDM_EXIT = 1002
)

type NOTIFYICONDATAT struct {
	cbSize           uint32
	hWnd             uintptr
	uID              uint32
	uFlags           uint32
	uCallbackMessage uint32
	hIcon            uintptr
	szTip            [128]uint16
}

type POINTT struct {
	X int32
	Y int32
}

var (
	user32  = syscall.NewLazyDLL("user32.dll")
	shell32 = syscall.NewLazyDLL("shell32.dll")

	pSetWindowLongPtrW   = user32.NewProc("SetWindowLongPtrW")
	pCallWindowProcW     = user32.NewProc("CallWindowProcW")
	pShowWindow          = user32.NewProc("ShowWindow")
	pSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	pPostQuitMessage     = user32.NewProc("PostQuitMessage")
	pGetCursorPos        = user32.NewProc("GetCursorPos")
	pCreatePopupMenu     = user32.NewProc("CreatePopupMenu")
	pAppendMenuW         = user32.NewProc("AppendMenuW")
	pTrackPopupMenu      = user32.NewProc("TrackPopupMenu")
	pDestroyMenu         = user32.NewProc("DestroyMenu")
	pCreateIcon          = user32.NewProc("CreateIcon")
	pDestroyIcon         = user32.NewProc("DestroyIcon")

	pShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	mainHwnd     uintptr
	oldWndProc   uintptr
	hTrayIcon    uintptr
	nid           NOTIFYICONDATAT
	wv           *webview.WebView
	sdChan       chan<- struct{}
	quitting     bool
)

func openGUI(url string, shutdown chan<- struct{}) {
	w := webview.New(true)
	defer w.Destroy()
	w.SetTitle("DSPlus - DeepSeek V4 Proxy")
	w.SetSize(1200, 800, webview.HintNone)
	w.Navigate(url)

	mainHwnd = uintptr(w.Window())
	wv = &w
	sdChan = shutdown

	cb := syscall.NewCallback(wndProc)
	r, _, _ := pSetWindowLongPtrW.Call(mainHwnd, GWLP_WNDPROC, cb)
	oldWndProc = r

	addTray()

	w.Run()

	delTray()
	close(shutdown)
}

func addTray() {
	hTrayIcon = makeIcon()

	tip := utf16.Encode([]rune("DSPlus - DeepSeek V4 Proxy"))

	nid = NOTIFYICONDATAT{
		cbSize:           uint32(unsafe.Sizeof(nid)),
		hWnd:             mainHwnd,
		uID:              1,
		uFlags:           NIF_ICON | NIF_MESSAGE | NIF_TIP,
		uCallbackMessage: WM_TRAYICON,
		hIcon:            hTrayIcon,
	}
	copy(nid.szTip[:], tip)

	pShellNotifyIconW.Call(uintptr(NIM_ADD), uintptr(unsafe.Pointer(&nid)))
}

func delTray() {
	pShellNotifyIconW.Call(uintptr(NIM_DELETE), uintptr(unsafe.Pointer(&nid)))
	if hTrayIcon != 0 {
		pDestroyIcon.Call(hTrayIcon)
	}
}

func makeIcon() uintptr {
	andMask := make([]byte, 128)
	xorMask := make([]byte, 32*32*4)

	for i := range 32 * 32 {
		xorMask[i*4] = 0xF6
		xorMask[i*4+1] = 0x82
		xorMask[i*4+2] = 0x3B
	}

	h, _, _ := pCreateIcon.Call(
		0,
		uintptr(32), uintptr(32),
		uintptr(1), uintptr(32),
		uintptr(unsafe.Pointer(&andMask[0])),
		uintptr(unsafe.Pointer(&xorMask[0])),
	)
	return h
}

func showWindow() {
	pShowWindow.Call(mainHwnd, uintptr(SW_RESTORE))
	pSetForegroundWindow.Call(mainHwnd)
}

func showMenu() {
	hMenu, _, _ := pCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}

	sShow := utf16.Encode([]rune("显示\000"))
	sExit := utf16.Encode([]rune("退出\000"))
	pAppendMenuW.Call(hMenu, MF_STRING, IDM_SHOW, uintptr(unsafe.Pointer(&sShow[0])))
	pAppendMenuW.Call(hMenu, MF_SEPARATOR, 0, 0)
	pAppendMenuW.Call(hMenu, MF_STRING, IDM_EXIT, uintptr(unsafe.Pointer(&sExit[0])))

	pSetForegroundWindow.Call(mainHwnd)

	var pt POINTT
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	pTrackPopupMenu.Call(
		hMenu,
		TPM_RIGHTBUTTON|TPM_BOTTOMALIGN,
		uintptr(pt.X), uintptr(pt.Y),
		0, mainHwnd, 0,
	)

	pDestroyMenu.Call(hMenu)
}

func doExit() {
	if quitting {
		return
	}
	quitting = true
	delTray()
	pPostQuitMessage.Call(0)
}

func wndProc(hwnd uintptr, msg uint32, wParam uintptr, lParam uintptr) uintptr {
	switch msg {
	case WM_CLOSE:
		pShowWindow.Call(hwnd, uintptr(SW_HIDE))
		return 0

	case WM_TRAYICON:
		switch uint32(lParam) {
		case WM_LBUTTONDBLCLK:
			showWindow()
		case WM_RBUTTONUP:
			showMenu()
		}
		return 0

	case WM_COMMAND:
		switch wParam & 0xFFFF {
		case IDM_SHOW:
			showWindow()
		case IDM_EXIT:
			doExit()
		}
		return 0
	}

	ret, _, _ := pCallWindowProcW.Call(oldWndProc, hwnd, uintptr(msg), wParam, lParam)
	return ret
}
