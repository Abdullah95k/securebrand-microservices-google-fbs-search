package secret

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

const key = "sk-test-1234567890"

func TestValueNeverPrints(t *testing.T) {
	v := New(key)
	var sb strings.Builder
	for _, s := range []string{
		fmt.Sprint(v), fmt.Sprintf("%v %+v %#v %s %q %x", v, v, v, v, v, v),
		fmt.Sprintf("%v", struct{ K Value }{v}), fmt.Sprintf("%+v", &struct{ K Value }{v}),
	} {
		sb.WriteString(s)
	}
	b, _ := json.Marshal(struct{ K Value }{v})
	sb.Write(b)
	slog.New(slog.NewTextHandler(&sb, nil)).Info("x", "key", v)
	slog.New(slog.NewJSONHandler(&sb, nil)).Info("x", "key", v)
	if strings.Contains(sb.String(), key) {
		t.Fatalf("credential leaked: %s", sb.String())
	}
	if v.Reveal() != key {
		t.Fatal("Reveal must return the value")
	}
}

func TestReadFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "k")
	if err := os.WriteFile(p, []byte("  "+key+"  \nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	v, err := ReadFile(p)
	if err != nil || v.Reveal() != key {
		t.Fatalf("ReadFile = %v, %v", v.Reveal(), err)
	}

	if err := os.Chmod(p, 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = ReadFile(p)
	if err == nil || !strings.Contains(err.Error(), "chmod 600") || strings.Contains(err.Error(), key) {
		t.Fatalf("world-readable file: err = %v", err)
	}

	_, err = ReadFile(filepath.Join(dir, "missing"))
	if !errors.Is(err, ErrMissing) {
		t.Fatalf("missing file: err = %v", err)
	}

	empty := filepath.Join(dir, "empty")
	if err := os.WriteFile(empty, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(empty); err == nil {
		t.Fatal("empty file must fail")
	}
}

func TestCleanError(t *testing.T) {
	cases := []error{
		&url.Error{Op: "Get", URL: "https://api.example.com/search?api_key=" + key, Err: errors.New("dial tcp: timeout")},
		&url.Error{Op: "Get", URL: "https://www.google.com/search?q=x", Err: &url.Error{Op: "proxyconnect", URL: "http://user:" + key + "@203.0.113.1:8000", Err: errors.New("connection refused")}},
		&url.Error{Op: "Get", URL: "https://x", Err: &net.OpError{Op: "proxyconnect", Net: "tcp", Err: errors.New("dial tcp 203.0.113.1:8000: i/o error")}},
		&url.Error{Op: "Get", URL: "https://x", Err: errors.New("dial tcp 203.0.113.1:8000: weird")},
		&url.Error{Op: "Get", URL: "https://x", Err: errors.New("bad proxy http://user:" + key + "@203.0.113.1:8000")},
		fmt.Errorf("wrapped: %w", &url.Error{Op: "Get", URL: "https://x/?key=" + key, Err: errors.New("EOF")}),
		errors.New("parse http://user:" + key + "@host: invalid"),
	}
	for _, in := range cases {
		out := CleanError(in)
		if out == nil || strings.Contains(out.Error(), key) || strings.Contains(out.Error(), "203.0.113.1") {
			t.Errorf("CleanError(%v) = %v", in, out)
		}
	}
	if CleanError(nil) != nil {
		t.Error("nil stays nil")
	}
	plain := errors.New("status 500")
	if !errors.Is(CleanError(plain), plain) {
		t.Error("an error without a URL is kept")
	}
	// What failed survives; addresses don't.
	proxyDown := &url.Error{Op: "Get", URL: "https://www.google.com/search?q=x", Err: &net.OpError{Op: "proxyconnect", Net: "tcp",
		Err: &net.OpError{Op: "dial", Net: "tcp", Addr: &net.TCPAddr{IP: net.IPv4(203, 0, 113, 10), Port: 8000}, Err: syscall.ECONNREFUSED}}}
	if got := CleanError(proxyDown).Error(); got != "connection refused (connecting to the proxy)" {
		t.Errorf("proxy down: %q", got)
	}
	dns := &url.Error{Op: "Get", URL: "https://api.mojeek.com/search?api_key=" + key, Err: &net.DNSError{Err: "no such host", Name: "api.mojeek.com"}}
	if got := CleanError(dns).Error(); got != "address lookup failed" {
		t.Errorf("dns: %q", got)
	}
	readReset := &net.OpError{Op: "read", Net: "tcp", Addr: &net.TCPAddr{IP: net.IPv4(203, 0, 113, 10), Port: 8000}, Err: syscall.ECONNRESET}
	if got := CleanError(readReset).Error(); got != "connection closed" {
		t.Errorf("read reset: %q", got)
	}
	timeout := &url.Error{Op: "Get", URL: "https://x", Err: context.DeadlineExceeded}
	if got := CleanError(timeout).Error(); got != "timed out" {
		t.Errorf("timeout: %q", got)
	}
}
