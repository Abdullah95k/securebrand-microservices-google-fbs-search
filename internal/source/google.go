package source

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/fburl"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/proxies"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/secret"
)

// Google sends plain google.com/search requests through the ISP proxy
// exits, in turn. It is a probe for the spike: it sends what a browser
// would ask for and reads the answer as it comes. A CAPTCHA, an "unusual
// traffic" page, a "turn on JavaScript" page or a consent wall comes back
// as a wall Outcome, and nothing tries to get past it: no browser, no
// solver, no retry.
type Google struct {
	Exits []proxies.Exit
	// Clients are the HTTP clients per exit (tests set them; NewGoogle
	// builds them).
	Clients []*http.Client
	// Base defaults to https://www.google.com.
	Base string

	mu   sync.Mutex
	next int
}

// NewGoogle builds one client per proxy exit.
func NewGoogle(exits []proxies.Exit, timeout time.Duration) *Google {
	g := &Google{Exits: exits}
	for _, e := range exits {
		tr := &http.Transport{
			Proxy:                 http.ProxyURL(e.URL()),
			TLSHandshakeTimeout:   15 * time.Second,
			ResponseHeaderTimeout: 30 * time.Second,
			MaxIdleConnsPerHost:   1,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
		}
		g.Clients = append(g.Clients, NewHTTPClient(tr, timeout))
	}
	return g
}

func (*Google) Name() string { return "google" }

func (*Google) Supports(w Window) bool { return true }

var googleTBS = map[Window]string{PastDay: "qdr:d", PastWeek: "qdr:w", PastMonth: "qdr:m"}

func (g *Google) Search(ctx context.Context, q Query) Response {
	if len(g.Clients) == 0 {
		return failed(0, "no proxy exits")
	}
	g.mu.Lock()
	i := g.next % len(g.Clients)
	g.next++
	g.mu.Unlock()
	via := "direct"
	if i < len(g.Exits) {
		via = g.Exits[i].Name
	}

	v := url.Values{}
	v.Set("q", SiteQuery(q.Term))
	hl, lang := "en", "en-US,en;q=0.9"
	if q.Lang == "ar" || q.Lang == "ckb" {
		hl, lang = "ar", "ar,en;q=0.8"
	}
	v.Set("hl", hl)
	if t, ok := googleTBS[q.Window]; ok {
		v.Set("tbs", t)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base(g.Base, "https://www.google.com")+"/search?"+v.Encode(), nil)
	if err != nil {
		r := failed(0, "could not build the request")
		r.Via = via
		return r
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", lang)

	resp, err := g.Clients[i].Do(req)
	if err != nil {
		r := failed(0, secret.CleanError(err).Error())
		r.Via = via
		return r
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := readBody(resp.Body)
	if err != nil {
		r := failed(resp.StatusCode, "reading the answer: "+secret.CleanError(err).Error())
		r.Via = via
		return r
	}
	r := ClassifyGoogle(resp.StatusCode, resp.Header.Get("Location"), body)
	r.Via = via
	return r
}

var (
	hrefRe    = regexp.MustCompile(`href="([^"]+)"`)
	captchaRe = regexp.MustCompile(`(?i)id="captcha-form"|g-recaptcha|/sorry/index|unusual traffic from your computer network|our systems have detected unusual traffic`)
	jsWallRe  = regexp.MustCompile(`(?i)/httpservice/retry/enablejs|enablejs\?|turn on javascript to keep searching|please click <a href="/httpservice`)
)

// ClassifyGoogle reads one google.com/search answer: a wall, or the
// facebook.com result links it holds (rank order, each once). A page that
// carries result links is an answer even if it also has a "turn on
// JavaScript" fallback; a CAPTCHA always wins.
func ClassifyGoogle(status int, location string, body []byte) Response {
	loc := strings.ToLower(location)
	switch {
	case strings.Contains(loc, "/sorry/"):
		return Response{Outcome: Captcha, Status: status, Detail: "redirected to Google's CAPTCHA page"}
	case strings.Contains(loc, "consent.google."):
		return Response{Outcome: Consent, Status: status, Detail: "redirected to Google's consent page"}
	case status == http.StatusTooManyRequests:
		return Response{Outcome: Captcha, Status: status, Detail: "HTTP 429 (Google's CAPTCHA page)"}
	case captchaRe.Match(body):
		return Response{Outcome: Captcha, Status: status, Detail: "a CAPTCHA / unusual traffic page"}
	case status >= 300 && status < 400:
		return failed(status, "redirected elsewhere")
	case status != http.StatusOK:
		return failed(status, "Google answered an error")
	}
	urls := facebookLinks(body)
	if len(urls) == 0 && jsWallRe.Match(body) {
		return Response{Outcome: BotCheck, Status: status, Detail: `a "turn on JavaScript" page: results are only served to a browser`}
	}
	return Response{Outcome: OK, Status: status, URLs: urls}
}

// facebookLinks returns the facebook.com links of a page, each once.
func facebookLinks(body []byte) []string {
	var out []string
	seen := map[string]bool{}
	for _, m := range hrefRe.FindAllSubmatch(body, -1) {
		u := resultLink(html.UnescapeString(string(m[1])))
		if u == "" || seen[u] {
			continue
		}
		if fburl.Classify(u).Skip == fburl.SkipNotFacebook {
			continue
		}
		seen[u] = true
		out = append(out, u)
	}
	return out
}

// resultLink returns the target of a result link: the link itself, or
// the q / url parameter of a /url?... redirect.
func resultLink(h string) string {
	if strings.HasPrefix(h, "/url?") {
		v, err := url.ParseQuery(h[len("/url?"):])
		if err != nil {
			return ""
		}
		if t := v.Get("q"); t != "" {
			h = t
		} else {
			h = v.Get("url")
		}
	}
	if !strings.HasPrefix(h, "http://") && !strings.HasPrefix(h, "https://") {
		return ""
	}
	return h
}
