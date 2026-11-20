//go:build windows

package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

var (
	advapi32            = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW   = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueEx = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey     = advapi32.NewProc("RegCloseKey")
)

const (
	hkeyCurrentUser   = uintptr(0x80000001)
	hkeyLocalMachine  = uintptr(0x80000002)
	keyRead           = 0x20019
	regSZ             = 1
	regExpandSZ       = 2
	regDword          = 4
	internetSettings  = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`
	chromeAppPathKey  = `SOFTWARE\Microsoft\Windows\CurrentVersion\App Paths\chrome.exe`
)

func regReadDWORD(root uintptr, subKey, valueName string) (uint32, bool) {
	keyPtr, _ := syscall.UTF16PtrFromString(subKey)
	var hKey uintptr
	r0, _, _ := procRegOpenKeyExW.Call(root, uintptr(unsafe.Pointer(keyPtr)), 0, keyRead, uintptr(unsafe.Pointer(&hKey)))
	if r0 != 0 {
		return 0, false
	}
	defer procRegCloseKey.Call(hKey)

	valPtr, _ := syscall.UTF16PtrFromString(valueName)
	var typ uint32
	var data uint32
	size := uint32(4)
	r1, _, _ := procRegQueryValueEx.Call(
		hKey,
		uintptr(unsafe.Pointer(valPtr)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&data)),
		uintptr(unsafe.Pointer(&size)),
	)
	if r1 != 0 || typ != regDword {
		return 0, false
	}
	return data, true
}

func regReadString(root uintptr, subKey, valueName string) (string, bool) {
	keyPtr, _ := syscall.UTF16PtrFromString(subKey)
	var hKey uintptr
	r0, _, _ := procRegOpenKeyExW.Call(root, uintptr(unsafe.Pointer(keyPtr)), 0, keyRead, uintptr(unsafe.Pointer(&hKey)))
	if r0 != 0 {
		return "", false
	}
	defer procRegCloseKey.Call(hKey)

	var valPtr *uint16
	if valueName != "" {
		valPtr, _ = syscall.UTF16PtrFromString(valueName)
	}
	var typ uint32
	var size uint32
	r1, _, _ := procRegQueryValueEx.Call(hKey, uintptr(unsafe.Pointer(valPtr)), 0, uintptr(unsafe.Pointer(&typ)), 0, uintptr(unsafe.Pointer(&size)))
	if r1 != 0 || size == 0 || (typ != regSZ && typ != regExpandSZ) {
		return "", false
	}
	buf := make([]uint16, size/2+1)
	r2, _, _ := procRegQueryValueEx.Call(
		hKey,
		uintptr(unsafe.Pointer(valPtr)),
		0,
		uintptr(unsafe.Pointer(&typ)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&size)),
	)
	if r2 != 0 {
		return "", false
	}
	return syscall.UTF16ToString(buf), true
}

func readSystemProxyURL() string {
	enabled, ok := regReadDWORD(hkeyCurrentUser, internetSettings, "ProxyEnable")
	if !ok || enabled == 0 {
		return ""
	}
	raw, ok := regReadString(hkeyCurrentUser, internetSettings, "ProxyServer")
	if !ok {
		return ""
	}
	return parseWindowsProxyServer(raw)
}

func locateChromeExecutable() (string, error) {
	var candidates []string
	for _, envVar := range []string{"ProgramFiles", "ProgramFiles(x86)", "LocalAppData"} {
		if base := os.Getenv(envVar); base != "" {
			candidates = append(candidates, filepath.Join(base, "Google", "Chrome", "Application", "chrome.exe"))
		}
	}
	if p, ok := regReadString(hkeyLocalMachine, chromeAppPathKey, ""); ok && p != "" {
		candidates = append(candidates, p)
	}
	if p, ok := regReadString(hkeyCurrentUser, chromeAppPathKey, ""); ok && p != "" {
		candidates = append(candidates, p)
	}
	for _, c := range candidates {
		if info, err := os.Stat(c); err == nil && !info.IsDir() {
			return c, nil
		}
	}
	if found, err := exec.LookPath("chrome.exe"); err == nil {
		return found, nil
	}
	return "", errors.New("未检测到 Google Chrome，请先安装 Google Chrome。")
}

func terminateProcessByPID(pid int) error {
	if pid <= 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Kill()
}

func parseWindowsProxyServer(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return ""
	}
	if !strings.Contains(trimmed, "=") {
		if strings.Contains(trimmed, "://") {
			return trimmed
		}
		return "http://" + trimmed
	}
	var socks, httpsProxy, httpProxy string
	for _, part := range strings.Split(trimmed, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		k := strings.ToLower(strings.TrimSpace(kv[0]))
		v := strings.TrimSpace(kv[1])
		if v == "" {
			continue
		}
		switch k {
		case "socks":
			if strings.Contains(v, "://") {
				socks = v
			} else {
				socks = "socks5://" + v
			}
		case "https":
			if strings.Contains(v, "://") {
				httpsProxy = v
			} else {
				httpsProxy = "http://" + v
			}
		case "http":
			if strings.Contains(v, "://") {
				httpProxy = v
			} else {
				httpProxy = "http://" + v
			}
		}
	}
	if socks != "" {
		return socks
	}
	if httpProxy != "" {
		return httpProxy
	}
	return httpsProxy
}
