//go:build !windows

package main

import (
	"errors"
	"os"
	"strings"
)

func readSystemProxyURL() string {
	return ""
}

func locateChromeExecutable() (string, error) {
	return "", errors.New("not running on Windows")
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
