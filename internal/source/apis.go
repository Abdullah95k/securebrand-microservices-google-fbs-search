package source

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/secret"
)

// Brave is the Brave Search API (web search).
type Brave struct {
	Key  secret.Value
	HTTP *http.Client
	// Base defaults to https://api.search.brave.com.
	Base string
	// Count is results per query: at most 20.
	Count int
}

func (*Brave) Name() string { return "brave" }

func (*Brave) Supports(w Window) bool {
	return w == AnyTime || w == PastDay || w == PastWeek || w == PastMonth
}

var braveFreshness = map[Window]string{PastDay: "pd", PastWeek: "pw", PastMonth: "pm"}

func (b *Brave) Search(ctx context.Context, q Query) Response {
	v := url.Values{}
	v.Set("q", SiteQuery(q.Term))
	v.Set("count", strconv.Itoa(clamp(b.Count, 1, 20, 20)))
	v.Set("safesearch", "off")
	v.Set("result_filter", "web")
	if f, ok := braveFreshness[q.Window]; ok {
		v.Set("freshness", f)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base(b.Base, "https://api.search.brave.com")+"/res/v1/web/search?"+v.Encode(), nil)
	if err != nil {
		return failed(0, "could not build the request")
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("X-Subscription-Token", b.Key.Reveal())
	var body struct {
		Web struct {
			Results []struct {
				URL string `json:"url"`
			} `json:"results"`
		} `json:"web"`
	}
	r := getJSON(b.HTTP, req, "Brave", &body)
	for _, x := range body.Web.Results {
		r.URLs = append(r.URLs, x.URL)
	}
	return r
}

// Mojeek is the Mojeek web search API. It has no recency filter.
type Mojeek struct {
	Key  secret.Value
	HTTP *http.Client
	// Base defaults to https://api.mojeek.com.
	Base string
	// Count is results per query (the plan sets the maximum: 10, 20 or 40).
	Count int
}

func (*Mojeek) Name() string { return "mojeek" }

func (*Mojeek) Supports(w Window) bool { return w == AnyTime }

func (m *Mojeek) Search(ctx context.Context, q Query) Response {
	v := url.Values{}
	v.Set("q", SiteQuery(q.Term))
	v.Set("fmt", "json")
	v.Set("t", strconv.Itoa(clamp(m.Count, 1, 40, 10)))
	// Mojeek takes the key only as a query parameter: secret.CleanError
	// keeps the URL out of every error.
	v.Set("api_key", m.Key.Reveal())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base(m.Base, "https://api.mojeek.com")+"/search?"+v.Encode(), nil)
	if err != nil {
		return failed(0, "could not build the request")
	}
	req.Header.Set("Accept", "application/json")
	var body struct {
		Response struct {
			Status  string `json:"status"`
			Results []struct {
				URL string `json:"url"`
			} `json:"results"`
		} `json:"response"`
	}
	r := getJSON(m.HTTP, req, "Mojeek", &body)
	if r.Outcome == OK && !strings.EqualFold(body.Response.Status, "OK") {
		return failed(r.Status, "Mojeek answered status "+strconv.Quote(clip(body.Response.Status, 40)))
	}
	for _, x := range body.Response.Results {
		r.URLs = append(r.URLs, x.URL)
	}
	return r
}

// Vertex is Google's Vertex AI Search ("AI Applications") website search,
// through searchLite, which takes an API key. The engine's data store must
// be a website data store for facebook.com, so the query is the term alone.
// It has no recency filter.
type Vertex struct {
	Key  secret.Value
	HTTP *http.Client
	// ServingConfig is "projects/<p>/locations/global/collections/
	// default_collection/engines/<engine>/servingConfigs/default_search".
	ServingConfig string
	// Base defaults to https://discoveryengine.googleapis.com.
	Base string
	// Count is results per query: at most 25 for website search.
	Count int
}

func (*Vertex) Name() string { return "vertex" }

func (*Vertex) Supports(w Window) bool { return w == AnyTime }

func (g *Vertex) Search(ctx context.Context, q Query) Response {
	payload, err := json.Marshal(map[string]any{"query": `"` + strings.ReplaceAll(q.Term, `"`, "") + `"`, "pageSize": clamp(g.Count, 1, 25, 10)})
	if err != nil {
		return failed(0, "could not build the request")
	}
	u := base(g.Base, "https://discoveryengine.googleapis.com") + "/v1/" + strings.Trim(g.ServingConfig, "/") + ":searchLite"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		return failed(0, "could not build the request (check the serving config path)")
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Goog-Api-Key", g.Key.Reveal())
	var body struct {
		Results []struct {
			Document struct {
				DerivedStructData struct {
					Link string `json:"link"`
				} `json:"derivedStructData"`
			} `json:"document"`
		} `json:"results"`
	}
	r := getJSON(g.HTTP, req, "Vertex AI Search", &body)
	for _, x := range body.Results {
		if x.Document.DerivedStructData.Link != "" {
			r.URLs = append(r.URLs, x.Document.DerivedStructData.Link)
		}
	}
	return r
}

// getJSON sends req and decodes a 200 answer into out.
func getJSON(c *http.Client, req *http.Request, what string, out any) Response {
	resp, err := c.Do(req)
	if err != nil {
		return failed(0, secret.CleanError(err).Error())
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := readBody(resp.Body)
	if err != nil {
		return failed(resp.StatusCode, "reading the answer: "+secret.CleanError(err).Error())
	}
	if resp.StatusCode != http.StatusOK {
		return statusOutcome(resp.StatusCode, what)
	}
	if err := json.Unmarshal(b, out); err != nil {
		return failed(resp.StatusCode, what+" answered something that is not the expected JSON")
	}
	return Response{Outcome: OK, Status: resp.StatusCode}
}

func base(b, def string) string {
	if b == "" {
		return def
	}
	return strings.TrimRight(b, "/")
}

func clamp(v, lo, hi, def int) int {
	if v <= 0 {
		return def
	}
	return max(lo, min(v, hi))
}

func clip(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}
