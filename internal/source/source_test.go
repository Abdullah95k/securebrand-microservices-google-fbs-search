package source

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/secret"
)

const testKey = "test-key-abcdef123456"

func client() *http.Client { return NewHTTPClient(http.DefaultTransport, 5*time.Second) }

func TestBrave(t *testing.T) {
	var got *http.Request
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = io.WriteString(w, `{"web":{"results":[{"url":"https://www.facebook.com/a"},{"url":"https://www.facebook.com/b"}]}}`)
	}))
	defer srv.Close()
	b := &Brave{Key: secret.New(testKey), HTTP: client(), Base: srv.URL}
	r := b.Search(context.Background(), Query{Term: `بغداد "x"`, Lang: "ar", Window: PastWeek})
	if r.Outcome != OK || len(r.URLs) != 2 || r.URLs[1] != "https://www.facebook.com/b" {
		t.Fatalf("response = %+v", r)
	}
	q := got.URL.Query()
	if q.Get("q") != `site:facebook.com "بغداد x"` || q.Get("freshness") != "pw" || q.Get("count") != "20" {
		t.Fatalf("query = %v", q)
	}
	if got.Header.Get("X-Subscription-Token") != testKey || strings.Contains(got.URL.String(), testKey) {
		t.Fatal("the key goes in the header only")
	}
	r = b.Search(context.Background(), Query{Term: "x", Window: AnyTime})
	if r.Outcome != OK || got.URL.Query().Has("freshness") {
		t.Fatalf("any time sends no freshness: %v", got.URL.Query())
	}
}

func TestAPIStatuses(t *testing.T) {
	for status, want := range map[int]Outcome{429: RateLimited, 401: Failed, 403: Failed, 402: Failed, 500: Failed} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(status)
		}))
		for _, s := range []Source{
			&Brave{Key: secret.New(testKey), HTTP: client(), Base: srv.URL},
			&Mojeek{Key: secret.New(testKey), HTTP: client(), Base: srv.URL},
			&Vertex{Key: secret.New(testKey), HTTP: client(), Base: srv.URL, ServingConfig: "projects/p/x"},
		} {
			r := s.Search(context.Background(), Query{Term: "x", Window: AnyTime})
			if r.Outcome != want || r.Status != status || strings.Contains(r.Detail, testKey) {
				t.Errorf("%s on %d: %+v", s.Name(), status, r)
			}
		}
		srv.Close()
	}
}

func TestMojeek(t *testing.T) {
	var got *http.Request
	body := `{"response":{"status":"OK","results":[{"url":"https://www.facebook.com/a"}]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_, _ = io.WriteString(w, body)
	}))
	defer srv.Close()
	m := &Mojeek{Key: secret.New(testKey), HTTP: client(), Base: srv.URL}
	r := m.Search(context.Background(), Query{Term: "Erbil", Window: AnyTime})
	if r.Outcome != OK || len(r.URLs) != 1 || got.URL.Query().Get("api_key") != testKey || got.URL.Query().Get("fmt") != "json" {
		t.Fatalf("response = %+v, query = %v", r, got.URL.Query())
	}
	body = `{"response":{"status":"ERROR"}}`
	if r := m.Search(context.Background(), Query{Term: "Erbil"}); r.Outcome != Failed {
		t.Fatalf("status ERROR: %+v", r)
	}
	// A network error must not print the URL, which holds the key.
	srv.Close()
	r = m.Search(context.Background(), Query{Term: "Erbil"})
	if r.Outcome != Failed || strings.Contains(r.Detail, testKey) || r.Detail == "" {
		t.Fatalf("network error: %+v", r)
	}
}

func TestVertex(t *testing.T) {
	var got *http.Request
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_, _ = io.WriteString(w, `{"results":[{"document":{"derivedStructData":{"link":"https://www.facebook.com/a"}}},{"document":{}}]}`)
	}))
	defer srv.Close()
	v := &Vertex{Key: secret.New(testKey), HTTP: client(), Base: srv.URL,
		ServingConfig: "projects/p/locations/global/collections/default_collection/engines/e/servingConfigs/default_search"}
	r := v.Search(context.Background(), Query{Term: "Duhok", Window: AnyTime})
	if r.Outcome != OK || len(r.URLs) != 1 {
		t.Fatalf("response = %+v", r)
	}
	if got.URL.Path != "/v1/projects/p/locations/global/collections/default_collection/engines/e/servingConfigs/default_search:searchLite" ||
		got.Header.Get("X-Goog-Api-Key") != testKey || got.URL.Query().Has("key") || payload["query"] != `"Duhok"` {
		t.Fatalf("request = %s %v %v", got.URL, got.Header, payload)
	}
}

func TestWindows(t *testing.T) {
	if (&Mojeek{}).Supports(PastWeek) || (&Vertex{}).Supports(PastWeek) || !(&Brave{}).Supports(PastWeek) || !(&Google{}).Supports(PastDay) {
		t.Fatal("window support")
	}
	for _, s := range []string{"any", "day", "Week", " month "} {
		if _, err := ParseWindow(s); err != nil {
			t.Error(err)
		}
	}
	if _, err := ParseWindow("year"); err == nil {
		t.Error("year is not a window")
	}
}

func TestClassifyGoogle(t *testing.T) {
	results := `<html><body>
<a href="/url?q=https://www.facebook.com/somebrand/&amp;sa=U&amp;ved=x">A</a>
<a href="https://www.facebook.com/profile.php?id=100064123456789&amp;sk=about">B</a>
<a href="https://www.facebook.com/somebrand/">dup</a>
<a href="/url?q=https://www.facebook.com/somebrand/&amp;sa=U">dup</a>
<a href="https://maps.google.com/x">maps</a>
<a href="/search?q=next">next</a>
</body></html>`
	cases := []struct {
		name     string
		status   int
		location string
		body     string
		want     Outcome
		urls     int
	}{
		{"results", 200, "", results, OK, 2},
		{"empty", 200, "", "<html>no results</html>", OK, 0},
		{"sorry redirect", 302, "https://www.google.com/sorry/index?continue=x", "", Captcha, 0},
		{"429", 429, "", "<html>sorry</html>", Captcha, 0},
		{"captcha form", 200, "", `<form id="captcha-form">`, Captcha, 0},
		{"unusual traffic", 200, "", `Our systems have detected unusual traffic from your computer network.`, Captcha, 0},
		{"js wall", 200, "", `<noscript><meta http-equiv="refresh" content="0;url=/httpservice/retry/enablejs?sei=x"></noscript>`, BotCheck, 0},
		{"consent", 302, "https://consent.google.com/ml?continue=x", "", Consent, 0},
		{"other redirect", 302, "https://www.google.iq/search?q=x", "", Failed, 0},
		{"server error", 500, "", "", Failed, 0},
	}
	for _, c := range cases {
		r := ClassifyGoogle(c.status, c.location, []byte(c.body))
		if r.Outcome != c.want || len(r.URLs) != c.urls {
			t.Errorf("%s: %+v", c.name, r)
		}
	}
	r := ClassifyGoogle(200, "", []byte(results))
	if r.URLs[0] != "https://www.facebook.com/somebrand/" || r.URLs[1] != "https://www.facebook.com/profile.php?id=100064123456789&sk=about" {
		t.Errorf("urls = %v", r.URLs)
	}
	if !Captcha.Wall() || !BotCheck.Wall() || !Consent.Wall() || RateLimited.Wall() || OK.Wall() {
		t.Error("Wall()")
	}
}

func TestGoogleRotatesExitsAndNeverFollowsRedirects(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		if r.URL.Query().Get("tbs") != "qdr:w" || r.URL.Query().Get("hl") != "ar" {
			t.Errorf("query = %v", r.URL.Query())
		}
		http.Redirect(w, r, "/sorry/index?continue=x", http.StatusFound)
	}))
	defer srv.Close()
	g := &Google{Clients: []*http.Client{client(), client()}, Base: srv.URL}
	r := g.Search(context.Background(), Query{Term: "x", Lang: "ckb", Window: PastWeek})
	if r.Outcome != Captcha || hits != 1 {
		t.Fatalf("response = %+v, hits = %d", r, hits)
	}
	_ = g.Search(context.Background(), Query{Term: "x", Lang: "ar", Window: PastWeek})
	if g.next != 2 {
		t.Fatal("exits rotate")
	}
}
