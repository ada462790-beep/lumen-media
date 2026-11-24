// Lumen — eigener Streaming-Server fürs Heimnetz.
// Copyright (C) 2026 Die Lumen-Autoren
//
// Veröffentlicht unter der GNU Affero General Public License v3.0.
// Der vollständige Text steht in der Datei LICENSE.
// SPDX-License-Identifier: AGPL-3.0-only

package main

// Die paar Win32-Aufrufe, die das Fenster braucht — direkt, ohne
// Oberflächen-Bibliothek.

import (
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	dwmapi   = windows.NewLazySystemDLL("dwmapi.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	shell32  = windows.NewLazySystemDLL("shell32.dll")
	ole32    = windows.NewLazySystemDLL("ole32.dll")

	pRegisterClassExW     = user32.NewProc("RegisterClassExW")
	pCreateWindowExW      = user32.NewProc("CreateWindowExW")
	pDefWindowProcW       = user32.NewProc("DefWindowProcW")
	pShowWindow           = user32.NewProc("ShowWindow")
	pUpdateWindow         = user32.NewProc("UpdateWindow")
	pGetMessageW          = user32.NewProc("GetMessageW")
	pTranslateMessage     = user32.NewProc("TranslateMessage")
	pDispatchMessageW     = user32.NewProc("DispatchMessageW")
	pPostMessageW         = user32.NewProc("PostMessageW")
	pPostQuitMessage      = user32.NewProc("PostQuitMessage")
	pDestroyWindow        = user32.NewProc("DestroyWindow")
	pLoadImageW           = user32.NewProc("LoadImageW")
	pLoadCursorW          = user32.NewProc("LoadCursorW")
	pGetWindowLongPtrW    = user32.NewProc("GetWindowLongPtrW")
	pSetWindowLongPtrW    = user32.NewProc("SetWindowLongPtrW")
	pSetWindowPos         = user32.NewProc("SetWindowPos")
	pGetWindowPlacement   = user32.NewProc("GetWindowPlacement")
	pSetWindowPlacement   = user32.NewProc("SetWindowPlacement")
	pMonitorFromWindow    = user32.NewProc("MonitorFromWindow")
	pMonitorFromRect      = user32.NewProc("MonitorFromRect")
	pGetMonitorInfoW      = user32.NewProc("GetMonitorInfoW")
	pGetDpiForWindow      = user32.NewProc("GetDpiForWindow")
	pGetDpiForSystem      = user32.NewProc("GetDpiForSystem")
	pGetSystemMenu        = user32.NewProc("GetSystemMenu")
	pAppendMenuW          = user32.NewProc("AppendMenuW")
	pMessageBoxW          = user32.NewProc("MessageBoxW")
	pFindWindowW          = user32.NewProc("FindWindowW")
	pSetForegroundWindow  = user32.NewProc("SetForegroundWindow")
	pIsIconic             = user32.NewProc("IsIconic")
	pGetKeyState          = user32.NewProc("GetKeyState")
	pGetSystemMetrics     = user32.NewProc("GetSystemMetrics")
	pSetWindowTextW       = user32.NewProc("SetWindowTextW")
	pGetClientRect        = user32.NewProc("GetClientRect")
	pRegisterHotKey       = user32.NewProc("RegisterHotKey")
	pUnregisterHotKey     = user32.NewProc("UnregisterHotKey")
	pCreateSolidBrush     = gdi32.NewProc("CreateSolidBrush")
	pDwmSetWindowAttr     = dwmapi.NewProc("DwmSetWindowAttribute")
	pGetModuleHandleW     = kernel32.NewProc("GetModuleHandleW")
	pGetUserDefaultUILang = kernel32.NewProc("GetUserDefaultUILanguage")
	pGlobalLock           = kernel32.NewProc("GlobalLock")
	pGlobalUnlock         = kernel32.NewProc("GlobalUnlock")
	pShellExecuteW        = shell32.NewProc("ShellExecuteW")
	pCreateStreamOnHGlob  = ole32.NewProc("CreateStreamOnHGlobal")
	pGetHGlobalFromStream = ole32.NewProc("GetHGlobalFromStream")
)

const (
	wsOverlappedWindow = 0x00CF0000
	wsClipChildren     = 0x02000000
	wsClipSiblings     = 0x04000000
	wsChild            = 0x40000000
	hwndBottom         = 1
	cwUseDefault       = ^uintptr(0x7FFFFFFF) // 0x80000000 als uintptr

	swHide           = 0
	swShowNormal     = 1
	swShowMaximized  = 3
	swShow           = 5
	swShowNoActivate = 4
	swRestore        = 9

	wmDestroy       = 0x0002
	wmMove          = 0x0003
	wmSize          = 0x0005
	wmSetFocus      = 0x0007
	wmClose         = 0x0010
	wmGetMinMaxInfo = 0x0024
	wmSettingChange = 0x001A
	wmSysCommand    = 0x0112
	wmHotkey        = 0x0312
	wmAppCommand    = 0x0319
	wmDpiChanged    = 0x02E0
	wmApp           = 0x8000

	sizeMinimized = 1

	gwlStyle = -16

	swpNoSize         = 0x0001
	swpNoMove         = 0x0002
	swpNoZOrder       = 0x0004
	swpNoActivate     = 0x0010
	swpFrameChanged   = 0x0020
	swpNoOwnerZOrder  = 0x0200
	monitorDefaultNul = 0
	monitorDefaultNah = 2

	imageIcon      = 1
	lrDefaultColor = 0
	lrShared       = 0x8000
	smCxIcon       = 11
	smCyIcon       = 12
	smCxSmIcon     = 49
	smCySmIcon     = 50
	idcArrow       = 32512

	mfString    = 0x0000
	mfSeparator = 0x0800

	mbOK          = 0x0000
	mbYesNo       = 0x0004
	mbIconInfo    = 0x0040
	mbIconWarning = 0x0030
	idYes         = 6

	vkReturn  = 0x0D
	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12
	vkF11     = 0x7A
	vkF12     = 0x7B
	vkS       = 0x53
	vkI       = 0x49

	dwmwaUseImmersiveDarkModeAlt = 19
	dwmwaUseImmersiveDarkMode    = 20
	dwmwaCaptionColor            = 35
)

type punkt struct{ X, Y int32 }

type rechteck struct{ Left, Top, Right, Bottom int32 }

type wndClassEx struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type msg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      punkt
	private uint32
}

type fensterLage struct {
	Length    uint32
	Flags     uint32
	ShowCmd   uint32
	MinPos    punkt
	MaxPos    punkt
	NormalPos rechteck
}

type monitorInfo struct {
	Size    uint32
	Monitor rechteck
	Work    rechteck
	Flags   uint32
}

type minMaxInfo struct {
	Reserved     punkt
	MaxSize      punkt
	MaxPosition  punkt
	MinTrackSize punkt
	MaxTrackSize punkt
}

func utf16(s string) *uint16 {
	p, _ := windows.UTF16PtrFromString(s)
	return p
}

func getWindowLongPtr(hwnd uintptr, index int32) uintptr {
	r, _, _ := pGetWindowLongPtrW.Call(hwnd, uintptr(index))
	return r
}

func setWindowLongPtr(hwnd uintptr, index int32, wert uintptr) {
	pSetWindowLongPtrW.Call(hwnd, uintptr(index), wert)
}

func lage(hwnd uintptr) fensterLage {
	l := fensterLage{Length: uint32(unsafe.Sizeof(fensterLage{}))}
	pGetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&l)))
	return l
}

func lageSetzen(hwnd uintptr, l fensterLage) {
	l.Length = uint32(unsafe.Sizeof(fensterLage{}))
	pSetWindowPlacement.Call(hwnd, uintptr(unsafe.Pointer(&l)))
}

func monitorVon(hwnd uintptr) (monitorInfo, bool) {
	m, _, _ := pMonitorFromWindow.Call(hwnd, monitorDefaultNah)
	mi := monitorInfo{Size: uint32(unsafe.Sizeof(monitorInfo{}))}
	r, _, _ := pGetMonitorInfoW.Call(m, uintptr(unsafe.Pointer(&mi)))
	return mi, r != 0
}

func aufEinemBildschirm(r rechteck) bool {
	m, _, _ := pMonitorFromRect.Call(uintptr(unsafe.Pointer(&r)), monitorDefaultNul)
	return m != 0
}

func dpiVon(hwnd uintptr) uint32 {
	if pGetDpiForWindow.Find() == nil && hwnd != 0 {
		if d, _, _ := pGetDpiForWindow.Call(hwnd); d != 0 {
			return uint32(d)
		}
	}
	if pGetDpiForSystem.Find() == nil {
		if d, _, _ := pGetDpiForSystem.Call(); d != 0 {
			return uint32(d)
		}
	}
	return 96
}

func tasteGedrueckt(vk uintptr) bool {
	r, _, _ := pGetKeyState.Call(vk)
	return int16(r) < 0
}

func meldungsfenster(hwnd uintptr, text, titel string, art uintptr) uintptr {
	r, _, _ := pMessageBoxW.Call(hwnd, uintptr(unsafe.Pointer(utf16(text))), uintptr(unsafe.Pointer(utf16(titel))), art)
	return r
}

// shellOeffnen öffnet eine Adresse mit dem, was Windows dafür vorsieht —
// bei http(s) der Standardbrowser.
func shellOeffnen(ziel string, parameter string) error {
	var p *uint16
	if parameter != "" {
		p = utf16(parameter)
	}
	r, _, _ := pShellExecuteW.Call(0, uintptr(unsafe.Pointer(utf16("open"))), uintptr(unsafe.Pointer(utf16(ziel))),
		uintptr(unsafe.Pointer(p)), 0, swShowNormal)
	if r <= 32 {
		return syscall.Errno(r)
	}
	return nil
}

// deutsch: Ist die Sprache der Windows-Oberfläche Deutsch?
func deutsch() bool {
	r, _, _ := pGetUserDefaultUILang.Call()
	return r&0x3FF == 0x07
}

// systemDunkel: Sind Apps unter Windows auf „Dunkel" gestellt? Danach
// richtet sich auch die Web-App (Darstellung „Wie das Gerät").
func systemDunkel() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return true
	}
	defer k.Close()
	hell, _, err := k.GetIntegerValue("AppsUseLightTheme")
	return err != nil || hell == 0
}

// rahmenFaerben färbt die Titelleiste wie das System: dunkel (Windows 10
// ab 20H1 und 11), unter Windows 11 dann im Farbton der Web-App — oder
// hell, wenn Windows hell eingestellt ist.
func rahmenFaerben(hwnd uintptr) {
	if pDwmSetWindowAttr.Find() != nil {
		return
	}
	dunkel := systemDunkel()
	an := int32(0)
	if dunkel {
		an = 1
	}
	if r, _, _ := pDwmSetWindowAttr.Call(hwnd, dwmwaUseImmersiveDarkMode, uintptr(unsafe.Pointer(&an)), 4); r != 0 {
		pDwmSetWindowAttr.Call(hwnd, dwmwaUseImmersiveDarkModeAlt, uintptr(unsafe.Pointer(&an)), 4)
	}
	farbe := uint32(0x00070505) // #050507 als COLORREF (0x00BBGGRR)
	if !dunkel {
		farbe = 0xFFFFFFFF // DWMWA_COLOR_DEFAULT
	}
	pDwmSetWindowAttr.Call(hwnd, dwmwaCaptionColor, uintptr(unsafe.Pointer(&farbe)), 4)
}
