package main

import "testing"

func TestNormalizeManualProxy(t *testing.T) {
	got, err := NormalizeManualProxy("127.0.0.1:7890")
	if err != nil || got != "http://127.0.0.1:7890" {
		t.Fatalf("expected http://127.0.0.1:7890, got %q, err=%v", got, err)
	}

	got, err = NormalizeManualProxy("SOCKS5://127.0.0.1:10808/")
	if err != nil || got != "socks5://127.0.0.1:10808" {
		t.Fatalf("expected socks5://127.0.0.1:10808, got %q, err=%v", got, err)
	}

	if _, err := NormalizeManualProxy("socks5://user:pass@127.0.0.1:1080"); err == nil {
		t.Fatal("expected error for authenticated proxy")
	}
	if _, err := NormalizeManualProxy("127.0.0.1:99999"); err == nil {
		t.Fatal("expected error for out-of-range port")
	}
}

func TestResolveTargetURL(t *testing.T) {
	if got := ResolveTargetURL("   "); got != "about:blank" {
		t.Fatalf("expected about:blank, got %q", got)
	}
	if got := ResolveTargetURL("example.com"); got != "https://example.com" {
		t.Fatalf("expected https://example.com, got %q", got)
	}
	if got := ResolveTargetURL("https://claude.ai/"); got != "https://claude.ai/" {
		t.Fatalf("expected https://claude.ai/, got %q", got)
	}
}

func TestBuildBrowserEnvironmentAndLocalTime(t *testing.T) {
	env := BuildBrowserEnvironment("browser-1", "socks5://127.0.0.1:10808", ProxyExitInfo{
		IP:       "75.37.237.37",
		Country:  "US",
		City:     "Irvine",
		Timezone: "America/Los_Angeles",
		Source:   "ipinfo",
	})
	if env.Locale != "en-US" || env.Timezone != "America/Los_Angeles" {
		t.Fatalf("unexpected env: %+v", env)
	}
	if formatted := FormatProxyLocalTime(env.Timezone); formatted == "" {
		t.Fatal("expected non-empty proxy local time")
	}
	if short := FormatShortTime(env.Timezone); short == "" {
		t.Fatal("expected non-empty short time")
	}
}

func TestParseWindowsProxyServer(t *testing.T) {
	if got := parseWindowsProxyServer("127.0.0.1:7890"); got != "http://127.0.0.1:7890" {
		t.Fatalf("got %q", got)
	}
	if got := parseWindowsProxyServer("http=127.0.0.1:7890;https=127.0.0.1:7890;socks=127.0.0.1:10808"); got != "socks5://127.0.0.1:10808" {
		t.Fatalf("got %q", got)
	}
}
