// Package fbs is a small client for the fbs API: it reads the discovery
// keywords and asks whether a page is already known.
package fbs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/secret"
)

// Keyword is a discovery keyword.
type Keyword struct {
	ID       int64  `json:"id"`
	Term     string `json:"term"`
	Language string `json:"language"`
	Group    string `json:"group"`
	Enabled  bool   `json:"enabled"`
}

// Client talks to the fbs API.
type Client struct {
	// Base is the API's root, e.g. http://127.0.0.1:8080.
	Base string
	Key  secret.Value
	HTTP *http.Client
}

// Keywords lists the enabled keywords, all pages of them.
func (c *Client) Keywords(ctx context.Context) ([]Keyword, error) {
	var out []Keyword
	after := int64(0)
	for range 1000 { // 1000 pages of 1000: far more than any list
		v := url.Values{"enabled": {"true"}, "limit": {"1000"}}
		if after > 0 {
			v.Set("after", strconv.FormatInt(after, 10))
		}
		var page struct {
			Keywords  []Keyword `json:"keywords"`
			NextAfter int64     `json:"next_after"`
		}
		if _, err := c.get(ctx, "/v1/discovery/keywords?"+v.Encode(), &page); err != nil {
			return nil, err
		}
		for _, k := range page.Keywords {
			if k.Enabled { // the filter is the server's; check anyway
				out = append(out, k)
			}
		}
		if page.NextAfter == 0 || len(page.Keywords) == 0 {
			return out, nil
		}
		after = page.NextAfter
	}
	return nil, fmt.Errorf("fbs: the keyword list did not end")
}

// Known reports whether fbs knows the page or account ref ("v:x" or
// "id:n"). fbs answers 404 for a page it has not scraped yet, so a page
// added but not crawled yet counts as unknown.
func (c *Client) Known(ctx context.Context, ref string) (bool, error) {
	status, err := c.get(ctx, "/v1/pages/"+url.PathEscape(ref), nil)
	if status == http.StatusNotFound {
		return false, nil
	}
	return err == nil, err
}

func (c *Client) get(ctx context.Context, path string, out any) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(c.Base, "/")+path, nil)
	if err != nil {
		return 0, fmt.Errorf("fbs: bad API address")
	}
	req.Header.Set("Authorization", "Bearer "+c.Key.Reveal())
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return 0, fmt.Errorf("fbs: %w", secret.CleanError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return resp.StatusCode, fmt.Errorf("fbs: reading the answer: %w", secret.CleanError(err))
	}
	switch {
	case resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden:
		return resp.StatusCode, fmt.Errorf("fbs: the API key was refused (HTTP %d): it needs the data:read scope", resp.StatusCode)
	case resp.StatusCode != http.StatusOK:
		return resp.StatusCode, fmt.Errorf("fbs: HTTP %d on %s", resp.StatusCode, strings.SplitN(path, "?", 2)[0])
	}
	if out != nil {
		if err := json.Unmarshal(b, out); err != nil {
			return resp.StatusCode, fmt.Errorf("fbs: unexpected answer on %s", strings.SplitN(path, "?", 2)[0])
		}
	}
	return resp.StatusCode, nil
}
