package checks

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/fburl"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/secret"
)

// CommonCrawl queries the Common Crawl URL index (index.commoncrawl.org).
type CommonCrawl struct {
	HTTP *http.Client
	// Base defaults to https://index.commoncrawl.org.
	Base string
	// Wait is the pause before the one retry of a busy index (default 30s).
	Wait time.Duration
}

// Crawl is one monthly crawl.
type Crawl struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	CDXAPI string `json:"cdx-api"`
}

// CCReport is what the index holds for facebook.com in one crawl.
type CCReport struct {
	Crawl Crawl
	// Blocks is the index's block count for *.facebook.com; each block
	// holds about 3,000 captures.
	Blocks int
	// Sampled captures, from the middle of the index.
	Sampled int
	// Statuses counts the sampled captures by HTTP status.
	Statuses map[string]int
	// Kinds counts the sampled 200 captures by what fburl makes of them.
	Kinds map[string]int
}

// EstimatedCaptures is a rough count of facebook.com captures.
func (r CCReport) EstimatedCaptures() int { return r.Blocks * 3000 }

// Latest returns the newest crawl.
func (c *CommonCrawl) Latest(ctx context.Context) (Crawl, error) {
	var crawls []Crawl
	if err := c.get(ctx, c.base()+"/collinfo.json", func(b []byte) error { return json.Unmarshal(b, &crawls) }); err != nil {
		return Crawl{}, err
	}
	if len(crawls) == 0 || crawls[0].ID == "" {
		return Crawl{}, fmt.Errorf("common crawl: no crawls listed")
	}
	return crawls[0], nil
}

// Facebook reports the crawl's facebook.com captures, sampling up to
// limit of them.
func (c *CommonCrawl) Facebook(ctx context.Context, crawl Crawl, limit int) (CCReport, error) {
	rep := CCReport{Crawl: crawl, Statuses: map[string]int{}, Kinds: map[string]int{}}
	api := crawl.CDXAPI
	if api == "" {
		api = c.base() + "/" + crawl.ID + "-index"
	}
	q := url.Values{"url": {"facebook.com"}, "matchType": {"domain"}}
	var pages struct {
		Pages  int `json:"pages"`
		Blocks int `json:"blocks"`
	}
	q.Set("showNumPages", "true")
	if err := c.get(ctx, api+"?"+q.Encode(), func(b []byte) error { return json.Unmarshal(b, &pages) }); err != nil {
		return rep, err
	}
	rep.Blocks = pages.Blocks
	if pages.Pages == 0 {
		return rep, nil
	}
	q.Del("showNumPages")
	q.Set("output", "json")
	q.Set("fl", "url,status")
	q.Set("page", fmt.Sprint(pages.Pages/2))
	q.Set("limit", fmt.Sprint(limit))
	err := c.get(ctx, api+"?"+q.Encode(), func(b []byte) error {
		sc := bufio.NewScanner(bytes.NewReader(b))
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		for sc.Scan() {
			var rec struct {
				URL    string `json:"url"`
				Status string `json:"status"`
			}
			if json.Unmarshal(sc.Bytes(), &rec) != nil {
				continue
			}
			rep.Sampled++
			rep.Statuses[rec.Status]++
			if rec.Status != "200" {
				continue
			}
			res := fburl.Classify(rec.URL)
			if res.OK() {
				rep.Kinds[string(res.Kind)]++
			} else {
				rep.Kinds["skip:"+string(res.Skip)]++
			}
		}
		return sc.Err()
	})
	return rep, err
}

func (c *CommonCrawl) base() string {
	if c.Base == "" {
		return "https://index.commoncrawl.org"
	}
	return strings.TrimRight(c.Base, "/")
}

// get fetches u, retrying once after Wait if the index is busy (503).
func (c *CommonCrawl) get(ctx context.Context, u string, read func([]byte) error) error {
	wait := c.Wait
	if wait == 0 {
		wait = 30 * time.Second
	}
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return fmt.Errorf("common crawl: bad request")
		}
		req.Header.Set("User-Agent", "securebrand-gfs-spike (one-off coverage check)")
		resp, err := c.HTTP.Do(req)
		if err != nil {
			return fmt.Errorf("common crawl: %w", secret.CleanError(err))
		}
		b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
		_ = resp.Body.Close()
		if err != nil {
			return fmt.Errorf("common crawl: reading the answer: %w", secret.CleanError(err))
		}
		switch {
		case resp.StatusCode == http.StatusOK:
			return read(b)
		case resp.StatusCode == http.StatusNotFound && bytes.Contains(b, []byte("No Captures")):
			return nil
		case (resp.StatusCode == http.StatusServiceUnavailable || resp.StatusCode == http.StatusTooManyRequests) && attempt == 0:
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(wait):
			}
			continue
		}
		return fmt.Errorf("common crawl: HTTP %d", resp.StatusCode)
	}
}

// FetchRobots reads a robots.txt (one request, no retry).
func FetchRobots(ctx context.Context, c *http.Client, u string) (*Robots, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("robots.txt: bad request")
	}
	req.Header.Set("User-Agent", "securebrand-gfs-spike (one-off robots.txt check)")
	resp, err := c.Do(req)
	if err != nil {
		return nil, fmt.Errorf("robots.txt: %w", secret.CleanError(err))
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("robots.txt: HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("robots.txt: %w", secret.CleanError(err))
	}
	return ParseRobots(string(b)), nil
}
