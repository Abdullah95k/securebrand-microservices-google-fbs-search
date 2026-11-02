// Package source queries search engines for facebook.com results. Each
// source returns the result URLs of one query and an Outcome; it never
// retries, and a CAPTCHA or bot check is reported, never worked around.
package source

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Window limits results to recently indexed ones. Not every source has
// every window.
type Window string

const (
	AnyTime Window = "any"
	PastDay Window = "day"
	// PastWeek is what a daily run uses, so that results indexed late
	// are still seen.
	PastWeek  Window = "week"
	PastMonth Window = "month"
)

// ParseWindow reads a window name.
func ParseWindow(s string) (Window, error) {
	switch w := Window(strings.ToLower(strings.TrimSpace(s))); w {
	case AnyTime, PastDay, PastWeek, PastMonth:
		return w, nil
	}
	return "", fmt.Errorf("unknown window %q (any, day, week or month)", s)
}

// Query is one keyword search.
type Query struct {
	Term string
	// Lang is the keyword's language: en, ar, ckb or kmr.
	Lang   string
	Window Window
}

// Outcome is how a query ended.
type Outcome string

const (
	OK Outcome = "ok"
	// Captcha: a CAPTCHA or "unusual traffic" page. Stop and back off.
	Captcha Outcome = "captcha"
	// BotCheck: any other check that a browser, not a program, is asking,
	// such as Google's "turn on JavaScript" page. Treated like a CAPTCHA.
	BotCheck Outcome = "bot_check"
	// Consent: a cookie-consent wall. Also a stop.
	Consent Outcome = "consent"
	// RateLimited: an API said too many requests (HTTP 429).
	RateLimited Outcome = "rate_limited"
	// Failed: any other error (network, bad key, server error).
	Failed Outcome = "failed"
)

// Wall reports whether the outcome is a CAPTCHA or bot check, after which
// the run stops: never retried, never worked around.
func (o Outcome) Wall() bool { return o == Captcha || o == BotCheck || o == Consent }

// Response is the result of one query.
type Response struct {
	Outcome Outcome
	// URLs are the result URLs in rank order.
	URLs []string
	// Status is the HTTP status, 0 when no response came.
	Status int
	// Via names the proxy exit used ("exit 2"), when there is one.
	Via string
	// Detail says what failed, without credentials.
	Detail string
}

// Source is a search engine.
type Source interface {
	Name() string
	// Supports reports whether the source can filter by w.
	Supports(w Window) bool
	// Search runs one query. It never retries.
	Search(ctx context.Context, q Query) Response
}

// SiteQuery is the query text for engines that take operators: the term,
// quoted, restricted to facebook.com.
func SiteQuery(term string) string {
	return `site:facebook.com "` + strings.ReplaceAll(term, `"`, "") + `"`
}

// maxBody caps how much of a response is read.
const maxBody = 4 << 20

func readBody(r io.Reader) ([]byte, error) {
	return io.ReadAll(io.LimitReader(r, maxBody))
}

// NewHTTPClient is a client with a whole-request timeout that doesn't
// follow redirects, so a redirect to a CAPTCHA page is seen as one.
func NewHTTPClient(tr http.RoundTripper, timeout time.Duration) *http.Client {
	return &http.Client{
		Transport: tr,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func failed(status int, detail string) Response {
	return Response{Outcome: Failed, Status: status, Detail: detail}
}

// statusOutcome maps an API's non-200 status to an outcome.
func statusOutcome(status int, what string) Response {
	switch status {
	case http.StatusTooManyRequests:
		return Response{Outcome: RateLimited, Status: status, Detail: what + " says too many requests"}
	case http.StatusUnauthorized, http.StatusForbidden:
		return failed(status, what+" rejected the API key (check the key file and the plan)")
	case http.StatusPaymentRequired:
		return failed(status, what+" says the plan's credit is used up")
	}
	return failed(status, fmt.Sprintf("%s answered HTTP %d", what, status))
}
