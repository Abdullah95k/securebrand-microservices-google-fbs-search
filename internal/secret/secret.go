// Package secret reads credentials from files and keeps them out of logs,
// errors and output.
package secret

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
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

// Empty reports whether no credential is set.
func (s Value) Empty() bool { return s.v == "" }

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

// CleanError drops the request URL from a transport error, since a URL
// can carry an API key (Mojeek takes it as a query parameter) or a proxy's
// user name and password. What is left says what failed.
func CleanError(err error) error {
	if err == nil {
		return nil
	}
	var ue *url.Error
	if errors.As(err, &ue) {
		inner := ue.Err
		// Proxy errors can repeat the proxy URL; keep only their kind.
		var pe *url.Error
		if errors.As(inner, &pe) {
			inner = pe.Err
		}
		msg := inner.Error()
		if strings.Contains(msg, "@") || strings.Contains(msg, "://") {
			msg = "request failed (details withheld: they could contain credentials)"
		}
		return fmt.Errorf("%s: %s", ue.Op, msg)
	}
	msg := err.Error()
	if strings.Contains(msg, "@") || strings.Contains(msg, "://") {
		return errors.New("request failed (details withheld: they could contain credentials)")
	}
	return err
}
