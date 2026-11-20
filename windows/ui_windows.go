//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	comctl32 = syscall.NewLazyDLL("comctl32.dll")
	dwmapi   = syscall.NewLazyDLL("dwmapi.dll")

	procGetModuleHandleW        = kernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW        = user32.NewProc("RegisterClassExW")
	procCreateWindowExW         = user32.NewProc("CreateWindowExW")
	procDefWindowProcW          = user32.NewProc("DefWindowProcW")
	procShowWindow              = user32.NewProc("ShowWindow")
	procUpdateWindow            = user32.NewProc("UpdateWindow")
	procGetMessageW             = user32.NewProc("GetMessageW")
	procTranslateMessage        = user32.NewProc("TranslateMessage")
	procDispatchMessageW        = user32.NewProc("DispatchMessageW")
	procPostQuitMessage         = user32.NewProc("PostQuitMessage")
	procPostMessageW            = user32.NewProc("PostMessageW")
	procSendMessageW            = user32.NewProc("SendMessageW")
	procBeginPaint              = user32.NewProc("BeginPaint")
	procEndPaint                = user32.NewProc("EndPaint")
	procInvalidateRect          = user32.NewProc("InvalidateRect")
	procGetClientRect           = user32.NewProc("GetClientRect")
	procGetWindowRect           = user32.NewProc("GetWindowRect")
	procSetWindowPos            = user32.NewProc("SetWindowPos")
	procAdjustWindowRectExForDpi = user32.NewProc("AdjustWindowRectExForDpi")
	procAdjustWindowRectEx      = user32.NewProc("AdjustWindowRectEx")
	procGetDpiForWindow         = user32.NewProc("GetDpiForWindow")
	procGetSystemMetrics        = user32.NewProc("GetSystemMetrics")
	procLoadCursorW             = user32.NewProc("LoadCursorW")
	procLoadIconW               = user32.NewProc("LoadIconW")
	procGetWindowTextLengthW    = user32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW          = user32.NewProc("GetWindowTextW")
	procSetWindowTextW          = user32.NewProc("SetWindowTextW")
	procEnableWindow            = user32.NewProc("EnableWindow")
	procSetFocus                = user32.NewProc("SetFocus")
	procMoveWindow              = user32.NewProc("MoveWindow")
	procSetWindowLongPtrW       = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProcW         = user32.NewProc("CallWindowProcW")
	procFillRect                = user32.NewProc("FillRect")
	procDrawTextW               = user32.NewProc("DrawTextW")
	procTrackMouseEvent         = user32.NewProc("TrackMouseEvent")
	procGetCursorPos            = user32.NewProc("GetCursorPos")
	procScreenToClient          = user32.NewProc("ScreenToClient")
	procIsDialogMessageW        = user32.NewProc("IsDialogMessageW")

	procCreateFontW             = gdi32.NewProc("CreateFontW")
	procDeleteObject            = gdi32.NewProc("DeleteObject")
	procSelectObject            = gdi32.NewProc("SelectObject")
	procCreateSolidBrush        = gdi32.NewProc("CreateSolidBrush")
	procCreatePen               = gdi32.NewProc("CreatePen")
	procRoundRect               = gdi32.NewProc("RoundRect")
	procEllipse                 = gdi32.NewProc("Ellipse")
	procSetBkMode               = gdi32.NewProc("SetBkMode")
	procSetTextColor            = gdi32.NewProc("SetTextColor")
	procSetBkColor              = gdi32.NewProc("SetBkColor")
	procCreateCompatibleDC      = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap  = gdi32.NewProc("CreateCompatibleBitmap")
	procDeleteDC                = gdi32.NewProc("DeleteDC")
	procBitBlt                  = gdi32.NewProc("BitBlt")

	procInitCommonControlsEx    = comctl32.NewProc("InitCommonControlsEx")
	procDwmSetWindowAttribute   = dwmapi.NewProc("DwmSetWindowAttribute")
)

const (
	wsOverlapped   = 0x00000000
	wsCaption      = 0x00C00000
	wsSysMenu      = 0x00080000
	wsMinimizeBox  = 0x00020000
	wsClipChildren = 0x02000000
	wsChild        = 0x40000000
	wsVisible      = 0x10000000
	wsTabStop      = 0x00010000

	esAutoHScroll = 0x0080
	emSetCueBanner = 0x1501

	swShow      = 5
	swHide      = 0
	idcArrow    = 32512
	idcHand     = 32649
	smCxScreen  = 0
	smCyScreen  = 1

	wmCreate        = 0x0001
	wmDestroy       = 0x0002
	wmPaint         = 0x000F
	wmEraseBkgnd    = 0x0014
	wmSetCursor     = 0x0020
	wmSetFont       = 0x0030
	wmGetDlgCode    = 0x0087
	wmKeyDown       = 0x0100
	wmChar          = 0x0102
	wmCommand       = 0x0111
	wmCtlColorEdit  = 0x0133
	wmMouseMove     = 0x0200
	wmLButtonDown   = 0x0201
	wmLButtonUp     = 0x0202
	wmMouseLeave    = 0x02A3
	wmDpiChanged    = 0x02E0
	wmActivate      = 0x0006

	wmApp            = 0x8000
	wmAppLaunchDone  = wmApp + 1
	wmAppRefreshDone = wmApp + 2
	wmAppCloseDone   = wmApp + 3

	vkReturn = 0x0D
	enChange = 0x0300

	dtLeft        = 0x00000000
	dtCenter      = 0x00000001
	dtVCenter     = 0x00000004
	dtWordBreak   = 0x00000010
	dtSingleLine  = 0x00000020
	dtEndEllipsis = 0x00008000

	transparentBk = 1
	srcCopy       = 0x00CC0020
	psSolid       = 0
	gwlpWndProc   = ^uintptr(3) // -4

	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	tmeLeave = 0x00000002
)

type rect struct {
	Left, Top, Right, Bottom int32
}

type point struct {
	X, Y int32
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      point
}

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type paintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     rect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type initCommonControlsExStruct struct {
	DwSize uint32
	DwICC  uint32
}

type trackMouseEventStruct struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

type hitAction struct {
	rect   rect
	kind   string
	arg    string
	cursor uintptr
}

type savedSettings struct {
	TargetURL   string `json:"target_url"`
	ProxyMode   string `json:"proxy_mode"`
	ManualProxy string `json:"manual_proxy"`
}

type appUI struct {
	hwnd        uintptr
	hInstance   uintptr
	dpi         int32
	urlEdit     uintptr
	proxyEdit   uintptr
	editBgBrush uintptr

	fontTitle   uintptr
	fontSection uintptr
	fontBody    uintptr
	fontSmall   uintptr
	fontMono    uintptr
	fontBtn     uintptr

	origUrlEditProc   uintptr
	origProxyEditProc uintptr

	mode            ProxyMode
	isLaunching     bool
	isRefreshing    bool
	closingIDs      map[string]bool
	feedbackOK      bool
	feedbackHasData bool
	feedbackError   string
	feedbackOutcome *LaunchOutcome
	activeList      []ActiveInstance

	hits        []hitAction
	hoveredKind string
	hoveredArg  string
	pressedKind string
	pressedArg  string
	tracking    bool

	mu          sync.Mutex
	launchOut   *LaunchOutcome
	launchErr   string
	refreshData []ActiveInstance
	closeErr    string
	closeTarget string
}

var ui = &appUI{
	dpi:        96,
	mode:       ProxyModeSystem,
	closingIDs: map[string]bool{},
}

func rgbColorRef(r, g, b byte) uintptr {
	return uintptr(uint32(r) | (uint32(g) << 8) | (uint32(b) << 16))
}

func (u *appUI) s(dp int32) int32 {
	return (dp*u.dpi + 48) / 96
}

func (u *appUI) settingsPath() string {
	return filepath.Join(filepath.Dir(Engine.ProfilesRootDir()), "settings.json")
}

func (u *appUI) loadSettings() savedSettings {
	raw, err := os.ReadFile(u.settingsPath())
	if err != nil {
		return savedSettings{ProxyMode: string(ProxyModeSystem)}
	}
	var s savedSettings
	if json.Unmarshal(raw, &s) != nil {
		return savedSettings{ProxyMode: string(ProxyModeSystem)}
	}
	if s.ProxyMode == "" {
		s.ProxyMode = string(ProxyModeSystem)
	}
	return s
}

func (u *appUI) saveSettings() {
	urlText := getControlText(u.urlEdit)
	proxyText := getControlText(u.proxyEdit)
	s := savedSettings{
		TargetURL:   urlText,
		ProxyMode:   string(u.mode),
		ManualProxy: proxyText,
	}
	path := u.settingsPath()
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if raw, err := json.MarshalIndent(s, "", "  "); err == nil {
		_ = os.WriteFile(path, raw, 0o644)
	}
}

func main() {
	runtime.LockOSThread()

	icc := initCommonControlsExStruct{DwSize: uint32(unsafe.Sizeof(initCommonControlsExStruct{})), DwICC: 0x0000FFFF}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))

	hInst, _, _ := procGetModuleHandleW.Call(0)
	ui.hInstance = hInst

	hIcon, _, _ := procLoadIconW.Call(hInst, 1)
	hCursor, _, _ := procLoadCursorW.Call(0, idcArrow)

	className, _ := syscall.UTF16PtrFromString("IsolatedBrowserWindowClass")
	winTitle, _ := syscall.UTF16PtrFromString("隔离浏览器")

	wc := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		Style:         0x0003, // CS_HREDRAW | CS_VREDRAW
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     hInst,
		HIcon:         hIcon,
		HCursor:       hCursor,
		HbrBackground: 0,
		LpszClassName: className,
		HIconSm:       hIcon,
	}
	procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren)
	hwnd, _, _ := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(winTitle)),
		style,
		100, 100, 520, 440,
		0, 0, hInst, 0,
	)
	ui.hwnd = hwnd

	// Windows 11 rounded corners + crisp white caption
	var cornerPref uint32 = 2 // DWMWCP_ROUND
	procDwmSetWindowAttribute.Call(hwnd, 33, uintptr(unsafe.Pointer(&cornerPref)), 4)
	var capColor uint32 = 0x00FFFFFF
	procDwmSetWindowAttribute.Call(hwnd, 35, uintptr(unsafe.Pointer(&capColor)), 4)

	ui.updateWindowSize(true)
	procShowWindow.Call(hwnd, swShow)
	procUpdateWindow.Call(hwnd)

	var m msg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			break
		}
		if r, _, _ := procIsDialogMessageW.Call(hwnd, uintptr(unsafe.Pointer(&m))); r != 0 {
			continue
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

func createUIFont(height int32, weight int32, face string) uintptr {
	facePtr, _ := syscall.UTF16PtrFromString(face)
	f, _, _ := procCreateFontW.Call(
		uintptr(uint32(height)), 0, 0, 0,
		uintptr(weight),
		0, 0, 0,
		1, // DEFAULT_CHARSET
		0, 0,
		5, // CLEARTYPE_QUALITY
		0,
		uintptr(unsafe.Pointer(facePtr)),
	)
	return f
}

func (u *appUI) rebuildFonts() {
	for _, f := range []uintptr{u.fontTitle, u.fontSection, u.fontBody, u.fontSmall, u.fontMono, u.fontBtn} {
		if f != 0 {
			procDeleteObject.Call(f)
		}
	}
	u.fontTitle = createUIFont(-u.s(15), 600, "Microsoft YaHei UI")
	u.fontSection = createUIFont(-u.s(13), 600, "Microsoft YaHei UI")
	u.fontBody = createUIFont(-u.s(13), 400, "Microsoft YaHei UI")
	u.fontSmall = createUIFont(-u.s(11), 400, "Microsoft YaHei UI")
	u.fontMono = createUIFont(-u.s(12), 400, "Consolas")
	u.fontBtn = createUIFont(-u.s(14), 600, "Microsoft YaHei UI")

	if u.urlEdit != 0 {
		procSendMessageW.Call(u.urlEdit, wmSetFont, u.fontBody, 1)
	}
	if u.proxyEdit != 0 {
		procSendMessageW.Call(u.proxyEdit, wmSetFont, u.fontMono, 1)
	}
}

func (u *appUI) computeContentHeight() int32 {
	h := int32(16) // top pad
	h += 92        // URL card
	h += 12        // gap
	if u.mode == ProxyModeManual {
		h += 148 // Proxy card expanded
	} else {
		h += 108 // Proxy card compact
	}
	h += 12 // gap

	if u.feedbackHasData {
		if u.feedbackOK && u.feedbackOutcome != nil {
			rows := int32(1)
			if u.feedbackOutcome.TimeSummary != "" {
				rows++
			}
			if u.feedbackOutcome.EnvironmentSummary != "" {
				rows++
			}
			h += 42 + rows*20 + 14
		} else {
			h += 68
		}
		h += 12
	}

	h += 44 // Launch CTA
	h += 12 // gap

	if len(u.activeList) == 0 {
		h += 44
	} else {
		h += 42 + int32(len(u.activeList))*56 + 6
	}
	h += 16 // bottom pad
	return h
}

func (u *appUI) layoutControls() {
	if u.hwnd == 0 {
		return
	}
	pad := u.s(18)
	cardW := u.s(500) - pad*2
	y := u.s(16)

	// URL input inside URL card (card starts at y, input starts at y + 42)
	inputX := pad + u.s(14) + u.s(8)
	inputW := cardW - u.s(28) - u.s(36)
	inputY := y + u.s(45)
	procMoveWindow.Call(u.urlEdit, uintptr(inputX), uintptr(inputY), uintptr(inputW), uintptr(u.s(22)), 1)

	y += u.s(92 + 12)
	if u.mode == ProxyModeManual {
		proxyY := y + u.s(84)
		proxyW := cardW - u.s(28) - u.s(16)
		procMoveWindow.Call(u.proxyEdit, uintptr(pad+u.s(14)+u.s(8)), uintptr(proxyY), uintptr(proxyW), uintptr(u.s(20)), 1)
		procShowWindow.Call(u.proxyEdit, swShow)
	} else {
		procShowWindow.Call(u.proxyEdit, swHide)
	}
}

func (u *appUI) updateWindowSize(center bool) {
	if u.hwnd == 0 {
		return
	}
	dpiRet, _, _ := procGetDpiForWindow.Call(u.hwnd)
	if dpiRet > 0 {
		u.dpi = int32(dpiRet)
	}
	u.rebuildFonts()

	clientW := u.s(500)
	clientH := u.computeContentHeight()
	rc := rect{0, 0, clientW, clientH}
	style := uintptr(wsOverlapped | wsCaption | wsSysMenu | wsMinimizeBox | wsClipChildren)
	if procAdjustWindowRectExForDpi.Find() == nil {
		procAdjustWindowRectExForDpi.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0, uintptr(u.dpi))
	} else {
		procAdjustWindowRectEx.Call(uintptr(unsafe.Pointer(&rc)), style, 0, 0)
	}
	winW := rc.Right - rc.Left
	winH := rc.Bottom - rc.Top

	var cur rect
	procGetWindowRect.Call(u.hwnd, uintptr(unsafe.Pointer(&cur)))
	x, y := cur.Left, cur.Top
	if center {
		sw, _, _ := procGetSystemMetrics.Call(smCxScreen)
		sh, _, _ := procGetSystemMetrics.Call(smCyScreen)
		x = (int32(sw) - winW) / 2
		y = (int32(sh) - winH) / 2
	}
	procSetWindowPos.Call(u.hwnd, 0, uintptr(x), uintptr(y), uintptr(winW), uintptr(winH), swpNoZOrder|swpNoActivate)
	u.layoutControls()
	procInvalidateRect.Call(u.hwnd, 0, 0)
}

func setCueBanner(editHwnd uintptr, text string) {
	ptr, _ := syscall.UTF16PtrFromString(text)
	procSendMessageW.Call(editHwnd, emSetCueBanner, 1, uintptr(unsafe.Pointer(ptr)))
}

func getControlText(hwnd uintptr) string {
	n, _, _ := procGetWindowTextLengthW.Call(hwnd)
	if n == 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(n+1))
	return syscall.UTF16ToString(buf)
}

func setControlText(hwnd uintptr, text string) {
	ptr, _ := syscall.UTF16PtrFromString(text)
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(ptr)))
}

func editSubclassProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	if message == wmKeyDown && wParam == vkReturn {
		ui.startLaunch()
		return 0
	}
	if message == wmChar && wParam == vkReturn {
		return 0
	}
	if message == wmGetDlgCode {
		return 0x0004 // DLGC_WANTALLKEYS so Enter doesn't beep
	}
	orig := ui.origUrlEditProc
	if hwnd == ui.proxyEdit {
		orig = ui.origProxyEditProc
	}
	ret, _, _ := procCallWindowProcW.Call(orig, hwnd, uintptr(message), wParam, lParam)
	return ret
}

func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	switch message {
	case wmCreate:
		ui.hwnd = hwnd
		dpiRet, _, _ := procGetDpiForWindow.Call(hwnd)
		if dpiRet > 0 {
			ui.dpi = int32(dpiRet)
		}
		ui.editBgBrush, _, _ = procCreateSolidBrush.Call(rgbColorRef(247, 249, 248))

		editCls, _ := syscall.UTF16PtrFromString("EDIT")
		ui.urlEdit, _, _ = procCreateWindowExW.Call(
			0, uintptr(unsafe.Pointer(editCls)), 0,
			wsChild|wsVisible|wsTabStop|esAutoHScroll,
			0, 0, 100, 20, hwnd, 101, ui.hInstance, 0,
		)
		ui.proxyEdit, _, _ = procCreateWindowExW.Call(
			0, uintptr(unsafe.Pointer(editCls)), 0,
			wsChild|wsTabStop|esAutoHScroll,
			0, 0, 100, 20, hwnd, 102, ui.hInstance, 0,
		)
		setCueBanner(ui.urlEdit, "https://example.com")
		setCueBanner(ui.proxyEdit, "socks5://127.0.0.1:1080 或 http://127.0.0.1:7890")

		cb := syscall.NewCallback(editSubclassProc)
		ui.origUrlEditProc, _, _ = procSetWindowLongPtrW.Call(ui.urlEdit, gwlpWndProc, cb)
		ui.origProxyEditProc, _, _ = procSetWindowLongPtrW.Call(ui.proxyEdit, gwlpWndProc, cb)

		saved := ui.loadSettings()
		setControlText(ui.urlEdit, saved.TargetURL)
		setControlText(ui.proxyEdit, saved.ManualProxy)
		switch ProxyMode(saved.ProxyMode) {
		case ProxyModeManual:
			ui.mode = ProxyModeManual
		case ProxyModeDirect:
			ui.mode = ProxyModeDirect
		default:
			ui.mode = ProxyModeSystem
		}

		ui.rebuildFonts()
		ui.triggerRefresh()
		procSetFocus.Call(ui.urlEdit)
		return 0

	case wmDpiChanged:
		newDpi := int32(wParam & 0xFFFF)
		if newDpi > 0 {
			ui.dpi = newDpi
		}
		ui.updateWindowSize(false)
		return 0

	case wmActivate:
		if (wParam & 0xFFFF) != 0 {
			ui.triggerRefresh()
		}
		return 0

	case wmCommand:
		hi := (wParam >> 16) & 0xFFFF
		if hi == enChange {
			ui.saveSettings()
		}
		return 0

	case wmCtlColorEdit:
		hdc := wParam
		procSetBkColor.Call(hdc, rgbColorRef(247, 249, 248))
		procSetTextColor.Call(hdc, rgbColorRef(24, 32, 29))
		return ui.editBgBrush

	case wmEraseBkgnd:
		return 1

	case wmPaint:
		var ps paintStruct
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		ui.paint(hdc)
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0

	case wmMouseMove:
		if !ui.tracking {
			tme := trackMouseEventStruct{
				CbSize:    uint32(unsafe.Sizeof(trackMouseEventStruct{})),
				DwFlags:   tmeLeave,
				HwndTrack: hwnd,
			}
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			ui.tracking = true
		}
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))
		if hit := ui.hitTest(x, y); hit != nil {
			if ui.hoveredKind != hit.kind || ui.hoveredArg != hit.arg {
				ui.hoveredKind, ui.hoveredArg = hit.kind, hit.arg
				procInvalidateRect.Call(hwnd, 0, 0)
			}
		} else if ui.hoveredKind != "" {
			ui.hoveredKind, ui.hoveredArg = "", ""
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0

	case wmMouseLeave:
		ui.tracking = false
		if ui.hoveredKind != "" || ui.pressedKind != "" {
			ui.hoveredKind, ui.hoveredArg = "", ""
			ui.pressedKind, ui.pressedArg = "", ""
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0

	case wmSetCursor:
		if (lParam & 0xFFFF) == 1 { // HTCLIENT
			var pt point
			procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
			procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
			if hit := ui.hitTest(pt.X, pt.Y); hit != nil && hit.cursor != 0 {
				hCur, _, _ := procLoadCursorW.Call(0, hit.cursor)
				user32.NewProc("SetCursor").Call(hCur)
				return 1
			}
		}

	case wmLButtonDown:
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))
		if hit := ui.hitTest(x, y); hit != nil {
			ui.pressedKind, ui.pressedArg = hit.kind, hit.arg
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0

	case wmLButtonUp:
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))
		pk, pa := ui.pressedKind, ui.pressedArg
		ui.pressedKind, ui.pressedArg = "", ""
		if hit := ui.hitTest(x, y); hit != nil && hit.kind == pk && hit.arg == pa {
			ui.handleAction(hit.kind, hit.arg)
		} else {
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0

	case wmAppLaunchDone:
		ui.mu.Lock()
		out := ui.launchOut
		errStr := ui.launchErr
		ui.mu.Unlock()
		ui.isLaunching = false
		procEnableWindow.Call(ui.urlEdit, 1)
		procEnableWindow.Call(ui.proxyEdit, 1)
		ui.feedbackHasData = true
		if errStr != "" {
			ui.feedbackOK = false
			ui.feedbackError = errStr
			ui.feedbackOutcome = nil
		} else {
			ui.feedbackOK = true
			ui.feedbackError = ""
			ui.feedbackOutcome = out
		}
		ui.updateWindowSize(false)
		ui.triggerRefresh()
		return 0

	case wmAppRefreshDone:
		ui.mu.Lock()
		list := ui.refreshData
		ui.mu.Unlock()
		ui.isRefreshing = false
		prevLen := len(ui.activeList)
		ui.activeList = list
		if len(list) != prevLen {
			ui.updateWindowSize(false)
		} else {
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0

	case wmAppCloseDone:
		ui.mu.Lock()
		target := ui.closeTarget
		errStr := ui.closeErr
		ui.mu.Unlock()
		delete(ui.closingIDs, target)
		if errStr != "" {
			ui.feedbackHasData = true
			ui.feedbackOK = false
			ui.feedbackError = errStr
			ui.feedbackOutcome = nil
			ui.updateWindowSize(false)
		} else if ui.feedbackOK && ui.feedbackOutcome != nil && ui.feedbackOutcome.ProfileID == target {
			ui.feedbackHasData = false
			ui.feedbackOutcome = nil
			ui.updateWindowSize(false)
		}
		ui.triggerRefresh()
		return 0

	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}

	ret, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
	return ret
}

func (u *appUI) hitTest(x, y int32) *hitAction {
	for i := len(u.hits) - 1; i >= 0; i-- {
		r := u.hits[i].rect
		if x >= r.Left && x <= r.Right && y >= r.Top && y <= r.Bottom {
			return &u.hits[i]
		}
	}
	return nil
}

func (u *appUI) handleAction(kind, arg string) {
	switch kind {
	case "clear_url":
		setControlText(u.urlEdit, "")
		procSetFocus.Call(u.urlEdit)
		u.saveSettings()
	case "mode":
		u.mode = ProxyMode(arg)
		u.feedbackHasData = false
		u.saveSettings()
		u.updateWindowSize(false)
		if u.mode == ProxyModeManual {
			procSetFocus.Call(u.proxyEdit)
		}
	case "launch":
		u.startLaunch()
	case "dismiss_feedback":
		u.feedbackHasData = false
		u.feedbackOutcome = nil
		u.updateWindowSize(false)
	case "refresh":
		u.triggerRefresh()
	case "close_profile":
		u.startClose(arg)
	}
}

func (u *appUI) startLaunch() {
	if u.isLaunching {
		return
	}
	rawURL := getControlText(u.urlEdit)
	rawProxy := getControlText(u.proxyEdit)
	targetURL := ResolveTargetURL(rawURL)
	mode := u.mode

	if mode == ProxyModeManual {
		if _, err := NormalizeManualProxy(rawProxy); err != nil {
			u.feedbackHasData = true
			u.feedbackOK = false
			u.feedbackError = err.Error()
			u.feedbackOutcome = nil
			u.updateWindowSize(false)
			return
		}
	}

	u.saveSettings()
	u.isLaunching = true
	u.feedbackHasData = false
	procEnableWindow.Call(u.urlEdit, 0)
	procEnableWindow.Call(u.proxyEdit, 0)
	u.updateWindowSize(false)

	go func() {
		out, err := Engine.LaunchChrome(targetURL, mode, rawProxy)
		u.mu.Lock()
		u.launchOut = out
		if err != nil {
			u.launchErr = err.Error()
		} else {
			u.launchErr = ""
		}
		u.mu.Unlock()
		procPostMessageW.Call(u.hwnd, wmAppLaunchDone, 0, 0)
	}()
}

func (u *appUI) triggerRefresh() {
	if u.isRefreshing {
		return
	}
	u.isRefreshing = true
	procInvalidateRect.Call(u.hwnd, 0, 0)
	go func() {
		items := Engine.ListActiveInstances()
		u.mu.Lock()
		u.refreshData = items
		u.mu.Unlock()
		procPostMessageW.Call(u.hwnd, wmAppRefreshDone, 0, 0)
	}()
}

func (u *appUI) startClose(profileID string) {
	if u.closingIDs[profileID] {
		return
	}
	u.closingIDs[profileID] = true
	procInvalidateRect.Call(u.hwnd, 0, 0)
	go func() {
		err := Engine.CloseInstance(profileID)
		u.mu.Lock()
		u.closeTarget = profileID
		if err != nil {
			u.closeErr = err.Error()
		} else {
			u.closeErr = ""
		}
		u.mu.Unlock()
		procPostMessageW.Call(u.hwnd, wmAppCloseDone, 0, 0)
	}()
}

// MARK: - Double-Buffered GDI Painting

func drawRoundBox(hdc uintptr, r rect, fillColor, borderColor uintptr, radius int32) {
	brush, _, _ := procCreateSolidBrush.Call(fillColor)
	pen, _, _ := procCreatePen.Call(psSolid, 1, borderColor)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	procRoundRect.Call(hdc, uintptr(r.Left), uintptr(r.Top), uintptr(r.Right), uintptr(r.Bottom), uintptr(radius), uintptr(radius))
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func drawDot(hdc uintptr, cx, cy, radius int32, fillColor uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(fillColor)
	pen, _, _ := procCreatePen.Call(psSolid, 1, fillColor)
	oldBrush, _, _ := procSelectObject.Call(hdc, brush)
	oldPen, _, _ := procSelectObject.Call(hdc, pen)
	procEllipse.Call(hdc, uintptr(cx-radius), uintptr(cy-radius), uintptr(cx+radius), uintptr(cy+radius))
	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(brush)
	procDeleteObject.Call(pen)
}

func drawTextRect(hdc uintptr, text string, r rect, font, color uintptr, flags uint32) {
	if text == "" {
		return
	}
	ptr, _ := syscall.UTF16FromString(text)
	oldFont, _, _ := procSelectObject.Call(hdc, font)
	procSetBkMode.Call(hdc, transparentBk)
	procSetTextColor.Call(hdc, color)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(&ptr[0])), uintptr(len(ptr)-1), uintptr(unsafe.Pointer(&r)), uintptr(flags))
	procSelectObject.Call(hdc, oldFont)
}

func (u *appUI) paint(screenDC uintptr) {
	var client rect
	procGetClientRect.Call(u.hwnd, uintptr(unsafe.Pointer(&client)))
	w := client.Right - client.Left
	h := client.Bottom - client.Top
	if w <= 0 || h <= 0 {
		return
	}

	memDC, _, _ := procCreateCompatibleDC.Call(screenDC)
	memBmp, _, _ := procCreateCompatibleBitmap.Call(screenDC, uintptr(w), uintptr(h))
	oldBmp, _, _ := procSelectObject.Call(memDC, memBmp)

	u.hits = u.hits[:0]

	// Palette
	bgCol := rgbColorRef(244, 246, 245)
	cardCol := rgbColorRef(255, 255, 255)
	cardBorder := rgbColorRef(227, 232, 229)
	fieldCol := rgbColorRef(247, 249, 248)
	fieldBorder := rgbColorRef(220, 226, 223)
	ink := rgbColorRef(24, 32, 29)
	muted := rgbColorRef(98, 112, 106)
	accent := rgbColorRef(15, 142, 110)
	accentHover := rgbColorRef(13, 126, 98)
	accentTint := rgbColorRef(234, 246, 242)
	accentBorder := rgbColorRef(164, 219, 203)
	danger := rgbColorRef(229, 72, 77)
	dangerTint := rgbColorRef(253, 240, 240)
	dangerBorder := rgbColorRef(245, 194, 196)

	bgBrush, _, _ := procCreateSolidBrush.Call(bgCol)
	procFillRect.Call(memDC, uintptr(unsafe.Pointer(&client)), bgBrush)
	procDeleteObject.Call(bgBrush)

	pad := u.s(18)
	cardW := w - pad*2
	y := u.s(16)

	// 1. URL Card
	urlCardH := u.s(92)
	drawRoundBox(memDC, rect{pad, y, pad + cardW, y + urlCardH}, cardCol, cardBorder, u.s(12))
	drawTextRect(memDC, "目标页面", rect{pad + u.s(14), y + u.s(12), pad + cardW/2, y + u.s(32)}, u.fontSection, ink, dtLeft|dtSingleLine)
	drawTextRect(memDC, "留空则打开空白页", rect{pad + cardW/2, y + u.s(12), pad + cardW - u.s(14), y + u.s(32)}, u.fontSmall, muted, dtLeft|dtSingleLine|0x2) // DT_RIGHT = 2

	urlBox := rect{pad + u.s(14), y + u.s(38), pad + cardW - u.s(14), y + u.s(76)}
	drawRoundBox(memDC, urlBox, fieldCol, fieldBorder, u.s(9))

	clearRect := rect{urlBox.Right - u.s(32), urlBox.Top + u.s(6), urlBox.Right - u.s(6), urlBox.Bottom - u.s(6)}
	clearCol := muted
	if u.hoveredKind == "clear_url" {
		clearCol = ink
	}
	drawTextRect(memDC, "×", clearRect, u.fontTitle, clearCol, dtCenter|dtVCenter|dtSingleLine)
	u.hits = append(u.hits, hitAction{rect: clearRect, kind: "clear_url", cursor: idcHand})

	y += urlCardH + u.s(12)

	// 2. Proxy Card
	proxyCardH := u.s(108)
	if u.mode == ProxyModeManual {
		proxyCardH = u.s(148)
	}
	drawRoundBox(memDC, rect{pad, y, pad + cardW, y + proxyCardH}, cardCol, cardBorder, u.s(12))
	drawTextRect(memDC, "网络出口", rect{pad + u.s(14), y + u.s(12), pad + cardW - u.s(14), y + u.s(32)}, u.fontSection, ink, dtLeft|dtSingleLine)

	segBox := rect{pad + u.s(14), y + u.s(38), pad + cardW - u.s(14), y + u.s(72)}
	drawRoundBox(memDC, segBox, fieldCol, fieldBorder, u.s(9))

	modes := []ProxyMode{ProxyModeSystem, ProxyModeManual, ProxyModeDirect}
	segInnerW := (segBox.Right - segBox.Left - u.s(8)) / 3
	for i, m := range modes {
		sx := segBox.Left + u.s(4) + int32(i)*segInnerW
		sr := rect{sx, segBox.Top + u.s(3), sx + segInnerW, segBox.Bottom - u.s(3)}
		selected := u.mode == m
		hovered := u.hoveredKind == "mode" && u.hoveredArg == string(m)
		if selected {
			drawRoundBox(memDC, sr, cardCol, cardBorder, u.s(7))
		} else if hovered {
			drawRoundBox(memDC, sr, rgbColorRef(238, 242, 240), rgbColorRef(238, 242, 240), u.s(7))
		}
		txtCol := muted
		if selected {
			txtCol = ink
		}
		drawTextRect(memDC, string(m), sr, u.fontBody, txtCol, dtCenter|dtVCenter|dtSingleLine)
		u.hits = append(u.hits, hitAction{rect: sr, kind: "mode", arg: string(m), cursor: idcHand})
	}

	hintTop := y + u.s(80)
	if u.mode == ProxyModeManual {
		pBox := rect{pad + u.s(14), y + u.s(78), pad + cardW - u.s(14), y + u.s(112)}
		drawRoundBox(memDC, pBox, fieldCol, fieldBorder, u.s(8))
		hintTop = y + u.s(120)
	}
	drawTextRect(memDC, u.mode.hintText(), rect{pad + u.s(14), hintTop, pad + cardW - u.s(14), y + proxyCardH - u.s(8)}, u.fontSmall, muted, dtLeft|dtSingleLine|dtEndEllipsis)

	y += proxyCardH + u.s(12)

	// 3. Feedback Banner
	if u.feedbackHasData {
		if u.feedbackOK && u.feedbackOutcome != nil {
			out := u.feedbackOutcome
			rows := int32(1)
			if out.TimeSummary != "" {
				rows++
			}
			if out.EnvironmentSummary != "" {
				rows++
			}
			fbH := u.s(42 + rows*20 + 14)
			drawRoundBox(memDC, rect{pad, y, pad + cardW, y + fbH}, accentTint, accentBorder, u.s(11))
			drawDot(memDC, pad+u.s(20), y+u.s(21), u.s(4), accent)
			drawTextRect(memDC, fmt.Sprintf("Chrome 已启动（端口 %d）", out.DevtoolsPort), rect{pad + u.s(30), y + u.s(11), pad + cardW - u.s(130), y + u.s(31)}, u.fontSection, ink, dtLeft|dtSingleLine)

			closeBtnR := rect{pad + cardW - u.s(124), y + u.s(10), pad + cardW - u.s(36), y + u.s(32)}
			closeBg := dangerTint
			if u.hoveredKind == "close_profile" && u.hoveredArg == out.ProfileID {
				closeBg = rgbColorRef(250, 224, 225)
			}
			drawRoundBox(memDC, closeBtnR, closeBg, dangerBorder, u.s(11))
			drawTextRect(memDC, "关闭 Chrome", closeBtnR, u.fontSmall, danger, dtCenter|dtVCenter|dtSingleLine)
			u.hits = append(u.hits, hitAction{rect: closeBtnR, kind: "close_profile", arg: out.ProfileID, cursor: idcHand})

			dismissR := rect{pad + cardW - u.s(30), y + u.s(10), pad + cardW - u.s(10), y + u.s(32)}
			drawTextRect(memDC, "×", dismissR, u.fontSection, muted, dtCenter|dtVCenter|dtSingleLine)
			u.hits = append(u.hits, hitAction{rect: dismissR, kind: "dismiss_feedback", cursor: idcHand})

			ry := y + u.s(40)
			drawTextRect(memDC, "代理  "+out.ProxyLabel, rect{pad + u.s(14), ry, pad + cardW - u.s(14), ry + u.s(18)}, u.fontMono, ink, dtLeft|dtSingleLine|dtEndEllipsis)
			ry += u.s(20)
			if out.TimeSummary != "" {
				drawTextRect(memDC, "时间  "+out.TimeSummary, rect{pad + u.s(14), ry, pad + cardW - u.s(14), ry + u.s(18)}, u.fontMono, ink, dtLeft|dtSingleLine|dtEndEllipsis)
				ry += u.s(20)
			}
			if out.EnvironmentSummary != "" {
				drawTextRect(memDC, "环境  "+out.EnvironmentSummary, rect{pad + u.s(14), ry, pad + cardW - u.s(14), ry + u.s(18)}, u.fontMono, ink, dtLeft|dtSingleLine|dtEndEllipsis)
			}
			y += fbH + u.s(12)
		} else {
			fbH := u.s(68)
			drawRoundBox(memDC, rect{pad, y, pad + cardW, y + fbH}, dangerTint, dangerBorder, u.s(11))
			drawTextRect(memDC, "操作失败", rect{pad + u.s(14), y + u.s(10), pad + cardW - u.s(36), y + u.s(28)}, u.fontSection, danger, dtLeft|dtSingleLine)
			dismissR := rect{pad + cardW - u.s(30), y + u.s(8), pad + cardW - u.s(10), y + u.s(28)}
			drawTextRect(memDC, "×", dismissR, u.fontSection, muted, dtCenter|dtVCenter|dtSingleLine)
			u.hits = append(u.hits, hitAction{rect: dismissR, kind: "dismiss_feedback", cursor: idcHand})
			drawTextRect(memDC, u.feedbackError, rect{pad + u.s(14), y + u.s(30), pad + cardW - u.s(14), y + fbH - u.s(8)}, u.fontSmall, ink, dtLeft|dtWordBreak|dtEndEllipsis)
			y += fbH + u.s(12)
		}
	}

	// 4. Launch CTA Button
	ctaH := u.s(44)
	ctaR := rect{pad, y, pad + cardW, y + ctaH}
	ctaFill := accent
	if u.isLaunching {
		ctaFill = rgbColorRef(115, 180, 162)
	} else if u.hoveredKind == "launch" {
		ctaFill = accentHover
	}
	drawRoundBox(memDC, ctaR, ctaFill, ctaFill, u.s(10))
	ctaLabel := "打开 Chrome"
	if u.isLaunching {
		ctaLabel = "正在构建环境并打开 Chrome…"
	}
	drawTextRect(memDC, ctaLabel, ctaR, u.fontBtn, rgbColorRef(255, 255, 255), dtCenter|dtVCenter|dtSingleLine)
	if !u.isLaunching {
		u.hits = append(u.hits, hitAction{rect: ctaR, kind: "launch", cursor: idcHand})
	}
	y += ctaH + u.s(12)

	// 5. Running Chrome Instances Card
	listCardH := u.s(44)
	if len(u.activeList) > 0 {
		listCardH = u.s(42 + int32(len(u.activeList))*56 + 6)
	}
	drawRoundBox(memDC, rect{pad, y, pad + cardW, y + listCardH}, cardCol, cardBorder, u.s(12))
	drawTextRect(memDC, fmt.Sprintf("运行中的 Chrome（%d）", len(u.activeList)), rect{pad + u.s(14), y + u.s(12), pad + cardW - u.s(80), y + u.s(32)}, u.fontSection, ink, dtLeft|dtSingleLine)

	refR := rect{pad + cardW - u.s(64), y + u.s(10), pad + cardW - u.s(14), y + u.s(32)}
	refText := "刷新"
	if u.isRefreshing {
		refText = "刷新中"
	}
	refCol := muted
	if u.hoveredKind == "refresh" {
		refCol = accent
	}
	drawTextRect(memDC, refText, refR, u.fontSmall, refCol, dtCenter|dtVCenter|dtSingleLine)
	u.hits = append(u.hits, hitAction{rect: refR, kind: "refresh", cursor: idcHand})

	ry := y + u.s(40)
	for _, inst := range u.activeList {
		rowR := rect{pad + u.s(14), ry, pad + cardW - u.s(14), ry + u.s(48)}
		drawRoundBox(memDC, rowR, fieldCol, fieldBorder, u.s(9))
		drawDot(memDC, rowR.Left+u.s(14), rowR.Top+u.s(15), u.s(3), accent)
		drawTextRect(memDC, inst.RegionSummary(), rect{rowR.Left + u.s(24), rowR.Top + u.s(6), rowR.Right - u.s(76), rowR.Top + u.s(24)}, u.fontBody, ink, dtLeft|dtSingleLine|dtEndEllipsis)

		shortTime := FormatShortTime(inst.Timezone)
		subLine := fmt.Sprintf("%s  :%d", inst.ProxySummary(), inst.DevtoolsPort)
		if shortTime != "" {
			subLine = fmt.Sprintf("%s · %s", shortTime, subLine)
		}
		drawTextRect(memDC, subLine, rect{rowR.Left + u.s(24), rowR.Top + u.s(25), rowR.Right - u.s(76), rowR.Bottom - u.s(6)}, u.fontMono, muted, dtLeft|dtSingleLine|dtEndEllipsis)

		cBtnR := rect{rowR.Right - u.s(66), rowR.Top + u.s(11), rowR.Right - u.s(10), rowR.Bottom - u.s(11)}
		cBtnBg := dangerTint
		if u.hoveredKind == "close_profile" && u.hoveredArg == inst.ProfileID {
			cBtnBg = rgbColorRef(250, 224, 225)
		}
		drawRoundBox(memDC, cBtnR, cBtnBg, dangerBorder, u.s(12))
		cLabel := "关闭"
		if u.closingIDs[inst.ProfileID] {
			cLabel = "关闭中"
		}
		drawTextRect(memDC, cLabel, cBtnR, u.fontSmall, danger, dtCenter|dtVCenter|dtSingleLine)
		u.hits = append(u.hits, hitAction{rect: cBtnR, kind: "close_profile", arg: inst.ProfileID, cursor: idcHand})

		ry += u.s(56)
	}

	procBitBlt.Call(screenDC, 0, 0, uintptr(w), uintptr(h), memDC, 0, 0, srcCopy)
	procSelectObject.Call(memDC, oldBmp)
	procDeleteObject.Call(memBmp)
	procDeleteDC.Call(memDC)
}

func (m ProxyMode) hintText() string {
	switch m {
	case ProxyModeSystem:
		if sys := readSystemProxyURL(); sys != "" {
			return fmt.Sprintf("已检测到系统代理 %s，将同步匹配出口语言与时区。", sys)
		}
		return "当前未开启系统代理，将按默认网络出口构建独立浏览器环境。"
	case ProxyModeManual:
		return "支持 http / https / socks4 / socks5（暂不支持带账号密码的代理）。"
	case ProxyModeDirect:
		return "强制直连（绕过系统代理），基于本地真实网络构造独立浏览器环境。"
	default:
		return ""
	}
}
