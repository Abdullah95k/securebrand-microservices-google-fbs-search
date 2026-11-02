package checks

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// A synthetic robots.txt in the shape of a large site's: named crawlers
// get path rules, everyone else is shut out.
const robots = `# Notice: automated collection is prohibited without permission.
User-agent: Googlebot
User-agent: Bingbot
Disallow: /ajax/
Disallow: /*/posts/*.php$
Allow: /*/posts/

User-agent: Applebot
Disallow:

User-agent: *
Disallow: /
`

func TestRobots(t *testing.T) {
	r := ParseRobots(robots)
	if got := fmt.Sprint(r.Agents()); got != "[googlebot bingbot applebot *]" {
		t.Fatalf("agents = %s", got)
	}
	cases := []struct {
		agent, path string
		want        bool
	}{
		{"Googlebot", "/somebrand", true},
		{"googlebot", "/ajax/x", false},
		{"Bingbot", "/people/x/1/", true},
		{"Googlebot", "/a/posts/1", true},
		{"Googlebot", "/a/posts/x.php", false}, // allow and disallow match; the longer (disallow) wins
		{"Applebot", "/anything", true},
		{"CCBot", "/", false},
		{"CCBot", "/somebrand", false},
		{"MojeekBot", "/profile.php?id=1", false},
	}
	for _, c := range cases {
		if got := r.Allowed(c.agent, c.path); got != c.want {
			t.Errorf("Allowed(%s, %s) = %v", c.agent, c.path, got)
		}
	}
	if !r.Named("GOOGLEBOT") || r.Named("CCBot") {
		t.Error("Named")
	}
	if !ParseRobots("").Allowed("x", "/") {
		t.Error("an empty file allows everything")
	}
}

func TestMatch(t *testing.T) {
	cases := []struct {
		pattern, path string
		want          bool
	}{
		{"/", "/x", true},
		{"/a", "/b", false},
		{"/a$", "/a", true},
		{"/a$", "/ab", false},
		{"/*.php$", "/x/y.php", true},
		{"/*.php$", "/x/y.php?z", false},
		{"/*.php", "/x/y.php?z", true},
		{"/a*b", "/axxb", true},
		{"/a*b", "/axx", false},
		{"/a*", "/a", true},
	}
	for _, c := range cases {
		if got := match(c.pattern, c.path); got != c.want {
			t.Errorf("match(%q, %q) = %v", c.pattern, c.path, got)
		}
	}
}

func TestCommonCrawl(t *testing.T) {
	busy := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/collinfo.json":
			if busy {
				busy = false
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			fmt.Fprintf(w, `[{"id":"CC-MAIN-2026-40","name":"October 2026","cdx-api":"%s/CC-MAIN-2026-40-index"},{"id":"CC-MAIN-2026-35"}]`, "http://"+r.Host)
		case r.URL.Path == "/CC-MAIN-2026-40-index" && r.URL.Query().Get("showNumPages") == "true":
			if r.URL.Query().Get("url") != "facebook.com" || r.URL.Query().Get("matchType") != "domain" {
				t.Errorf("query = %v", r.URL.Query())
			}
			fmt.Fprint(w, `{"pages": 4, "pageSize": 5, "blocks": 18}`)
		case r.URL.Path == "/CC-MAIN-2026-40-index":
			if r.URL.Query().Get("page") != "2" || r.URL.Query().Get("limit") != "100" {
				t.Errorf("query = %v", r.URL.Query())
			}
			fmt.Fprint(w, `{"url":"https://www.facebook.com/somebrand","status":"200"}
{"url":"https://www.facebook.com/groups/1/","status":"200"}
{"url":"https://www.facebook.com/x","status":"302"}
not json
`)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	cc := &CommonCrawl{HTTP: srv.Client(), Base: srv.URL, Wait: time.Millisecond}
	crawl, err := cc.Latest(context.Background())
	if err != nil || crawl.ID != "CC-MAIN-2026-40" {
		t.Fatalf("latest = %+v, %v", crawl, err)
	}
	rep, err := cc.Facebook(context.Background(), crawl, 100)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Blocks != 18 || rep.EstimatedCaptures() != 54000 || rep.Sampled != 3 || rep.Statuses["200"] != 2 ||
		rep.Kinds["page"] != 1 || rep.Kinds["skip:group"] != 1 {
		t.Fatalf("report = %+v", rep)
	}
}

func TestFetchRobots(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/robots.txt" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, robots)
	}))
	defer srv.Close()
	r, err := FetchRobots(context.Background(), srv.Client(), srv.URL+"/robots.txt")
	if err != nil || r.Allowed("CCBot", "/") {
		t.Fatalf("robots = %v, %v", r, err)
	}
	if _, err := FetchRobots(context.Background(), srv.Client(), srv.URL+"/nope"); err == nil {
		t.Fatal("404 must fail")
	}
}
