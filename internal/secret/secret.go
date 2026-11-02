// Package secret reads credentials from files and keeps them out of logs,
// errors and output.
package secret

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
)

// Value is a credential. Printing it in any way gives "[redacted]"; only
// Reveal returns the value, at the point it is sent.
type Value struct{ v string }

const redacted = "[redacted]"

func (Value) String() string                { return redacted }
func (Value) GoString() string              { return redacted }
func (Value) MarshalText() ([]byte, error)  { return []byte(redacted), nil }
func (Value) Format(f fmt.State, verb rune) { _, _ = f.Write([]byte(redacted)) }

// Reveal returns the credential, to put in a request header or URL.
func (s Value) Reveal() string { return s.v }

// New wraps a credential that did not come from a file (tests).
func New(v string) Value { return Value{v: v} }

// ErrMissing means the credential file does not exist.
var ErrMissing = errors.New("credential file not found")

// ReadFile reads a credential from path: the first line, trimmed. The file
// must not be readable by group or others. Errors name the path, never the
// content.
func ReadFile(path string) (Value, error) {
	st, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return Value{}, fmt.Errorf("%s: %w", path, ErrMissing)
	}
	if err != nil {
		return Value{}, fmt.Errorf("%s: cannot read the credential file", path)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return Value{}, fmt.Errorf("%s: other users can read it; run: chmod 600 %s", path, path)
	}
	b, err := os.ReadFile(path) // #nosec G304 -- the operator names the file
	if err != nil {
		return Value{}, fmt.Errorf("%s: cannot read the credential file", path)
	}
	line, _, _ := strings.Cut(string(b), "\n")
	v := strings.TrimSpace(line)
	if v == "" {
		return Value{}, fmt.Errorf("%s: the file is empty", path)
	}
	return Value{v: v}, nil
}

// CleanError replaces a transport error with a fixed description of what
// failed. The original text is never kept: a request URL can carry an API
// key (Mojeek takes it as a query parameter), and network errors name the
// proxy's address ("proxyconnect tcp: dial tcp <ip>:<port>: ...").
func CleanError(err error) error {
	if err == nil {
		return nil
	}
	var ue *url.Error
	var ne net.Error
	if errors.As(err, &ue) || errors.As(err, &ne) {
		return errors.New(classify(err))
	}
	// Errors this program builds itself hold no credentials or addresses.
	// Keep them, but not one that looks like a URL with a user name.
	if strings.Contains(err.Error(), "://") || strings.Contains(err.Error(), "@") {
		return errors.New("request failed (details withheld: they could contain credentials)")
	}
	return err
}

// classify names a network error's kind, from its type only.
func classify(err error) string {
	via := ""
	var op *net.OpError
	if errors.As(err, &op) && op.Op == "proxyconnect" {
		via = " (connecting to the proxy)"
	}
	var ne net.Error
	var dns *net.DNSError
	var cert *tls.CertificateVerificationError
	var hostname x509.HostnameError
	var unknownAuth x509.UnknownAuthorityError
	switch {
	case errors.As(err, &ne) && ne.Timeout(), errors.Is(err, context.DeadlineExceeded):
		return "timed out" + via
	case errors.Is(err, context.Canceled):
		return "cancelled"
	case errors.As(err, &dns):
		return "address lookup failed" + via
	case errors.Is(err, syscall.ECONNREFUSED):
		return "connection refused" + via
	case errors.Is(err, syscall.ECONNRESET), errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "connection closed" + via
	case errors.As(err, &cert), errors.As(err, &hostname), errors.As(err, &unknownAuth):
		return "TLS certificate not accepted" + via
	case via != "":
		return "proxy connection failed"
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		if strings.Contains(ue.Err.Error(), "Proxy Authentication Required") {
			return "the proxy refused the user name or password"
		}
		return "request failed (" + ue.Op + ")"
	}
	return "network error"
}
