package spike

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"strings"
	"testing"
	"time"

	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/fbs"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/source"
)

// fake answers queries from a script, one response per call.
type fake struct {
	script  []source.Response
	queries []source.Query
	windows map[source.Window]bool
}

func (*fake) Name() string                    { return "fake" }
func (f *fake) Supports(w source.Window) bool { return f.windows == nil || f.windows[w] }
func (f *fake) Search(_ context.Context, q source.Query) source.Response {
	f.queries = append(f.queries, q)
	if len(f.script) == 0 {
		return source.Response{Outcome: source.OK}
	}
	r := f.script[0]
	f.script = f.script[1:]
	return r
}

func ok(urls ...string) source.Response { return source.Response{Outcome: source.OK, URLs: urls} }

func keywords(n int) []fbs.Keyword {
	var out []fbs.Keyword
	for i := range n {
		out = append(out, fbs.Keyword{ID: int64(i + 1), Term: fmt.Sprint("term", i), Language: Languages[i%len(Languages)], Enabled: true})
	}
	return out
}

type clock struct {
	now    time.Time
	sleeps []time.Duration
}

func (c *clock) sleep(_ context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.now = c.now.Add(d)
	return nil
}

func config(src source.Source, kws []fbs.Keyword, c *clock) Config {
	return Config{Source: src, Keywords: kws, Windows: []source.Window{source.PastWeek}, MinGap: time.Second, MaxGap: 3 * time.Second,
		MaxQueries: 100, Sleep: c.sleep, Now: func() time.Time { return c.now }, Rand: rand.New(rand.NewPCG(1, 2))}
}

func TestSample(t *testing.T) {
	kws := keywords(40) // 10 per language
	a := Sample(kws, 3, 1)
	b := Sample(kws, 3, 1)
	if len(a) != 12 || fmt.Sprint(a) != fmt.Sprint(b) {
		t.Fatalf("sample = %v", a)
	}
	for i, l := range Languages {
		for _, k := range a[i*3 : i*3+3] {
			if k.Language != l {
				t.Fatalf("order: %v", a)
			}
		}
	}
	if c := Sample(kws, 3, 2); fmt.Sprint(c) == fmt.Sprint(a) {
		t.Error("another seed, another sample")
	}
	if all := Sample(kws, 100, 1); len(all) != 40 {
		t.Errorf("perLang above the list takes all: %d", len(all))
	}
}

func TestRunCounts(t *testing.T) {
	f := &fake{script: []source.Response{
		ok("https://www.facebook.com/brand", "https://www.facebook.com/groups/1", "https://example.com/x", "https://www.facebook.com/reel/1"),
		ok(),
		ok("https://m.facebook.com/brand/posts/2", "https://www.facebook.com/profile.php?id=100064123456789"),
		ok("https://www.facebook.com/other"),
	}}
	c := &clock{now: time.Date(2026, 11, 3, 7, 0, 0, 0, time.UTC)}
	var rows []Row
	cfg := config(f, keywords(4), c)
	cfg.OnRow = func(r Row) error { rows = append(rows, r); return nil }
	st := Run(context.Background(), cfg)
	if st.Stopped != "" || st.Queries != 4 || len(c.sleeps) != 3 {
		t.Fatalf("stats = %+v, sleeps = %v", st, c.sleeps)
	}
	for _, d := range c.sleeps {
		if d < time.Second || d >= 3*time.Second {
			t.Fatalf("gap %v outside [1s, 3s)", d)
		}
	}
	ws := st.Windows[0]
	if ws.Answered != 4 || ws.Empty != 1 || ws.Results != 7 || ws.Facebook != 6 || len(ws.Refs) != 3 ||
		ws.Kinds["page"] != 3 || ws.Kinds["post"] != 1 || ws.Skips["group"] != 1 || ws.Skips["no_owner"] != 1 {
		t.Fatalf("window = %+v", ws)
	}
	if ws.Refs["v:brand"] != "en" || ws.ByLang["ckb"].Facebook != 2 || !ws.ByLang["ckb"].Refs["id:100064123456789"] {
		t.Fatalf("by language = %+v", ws.ByLang)
	}
	if len(rows) != 7 || rows[1].Result != "skip:group" || rows[0].Ref != "v:brand" || rows[0].KeywordID != 1 {
		t.Fatalf("rows = %+v", rows)
	}

	asked := 0
	err := CheckKnown(context.Background(), []*Stats{st, st}, func(_ context.Context, ref string) (bool, error) {
		asked++
		return ref == "v:brand", nil
	})
	if asked != 3 {
		t.Fatalf("each ref is asked once across runs: %d", asked)
	}
	if err != nil || st.NewCount(map[string]bool{"v:brand": true, "v:other": true}) != 1 {
		t.Fatalf("known = %v %v", st.Known, err)
	}
	var sb strings.Builder
	Report(&sb, Header{Enabled: map[string]int{"en": 5}, Sampled: map[string]int{"en": 1}}, []*Stats{st, st})
	out := sb.String()
	for _, want := range []string{"Iraq time (UTC+3)", "2026-11-03 10:00", "queries sent 4: answered 4", "results 7 (1.8 a query; 1 queries with none)",
		"not yet known to fbs 2", "group 1", "fake and fake: 3"} {
		if !strings.Contains(out, want) {
			t.Errorf("report lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "facebook.com/brand") {
		t.Error("the report must not hold URLs")
	}
}

func TestRunStopsAtFirstWall(t *testing.T) {
	f := &fake{script: []source.Response{ok("https://www.facebook.com/a"), {Outcome: source.Captcha, Detail: "a CAPTCHA page"}}}
	c := &clock{}
	st := Run(context.Background(), config(f, keywords(10), c))
	if len(f.queries) != 2 || st.WallAfter != 1 || !strings.Contains(st.Stopped, "captcha after 1 answered") {
		t.Fatalf("queries = %d, stats = %+v", len(f.queries), st)
	}
	f = &fake{script: []source.Response{{Outcome: source.BotCheck, Detail: "JS"}}}
	st = Run(context.Background(), config(f, keywords(10), c))
	if len(f.queries) != 1 || st.Outcomes[source.BotCheck] != 1 || st.Stopped == "" {
		t.Fatalf("bot check: %+v", st)
	}
}

func TestRunRateLimitsBackOffAndNeverRetry(t *testing.T) {
	rl := source.Response{Outcome: source.RateLimited}
	f := &fake{script: []source.Response{rl, ok(), rl, rl, rl}}
	c := &clock{}
	cfg := config(f, keywords(10), c)
	cfg.MinGap, cfg.MaxGap = time.Second, time.Second
	st := Run(context.Background(), cfg)
	// Each query is sent once: term0 (limited), term1, term2 (limited), term3, term4.
	for i, q := range f.queries {
		if q.Term != fmt.Sprint("term", i) {
			t.Fatalf("query %d retried: %v", i, f.queries)
		}
	}
	if st.Stopped != "rate limited 3 times in a row" || st.Queries != 5 {
		t.Fatalf("stats = %+v", st)
	}
	want := []time.Duration{2 * time.Minute, time.Second, time.Second, 2 * time.Minute, time.Second, 4 * time.Minute, time.Second}
	if fmt.Sprint(c.sleeps) != fmt.Sprint(want) {
		t.Fatalf("sleeps = %v, want %v", c.sleeps, want)
	}
}

func TestRunStopsOnRepeatedFailures(t *testing.T) {
	bad := source.Response{Outcome: source.Failed, Detail: "key rejected"}
	f := &fake{script: []source.Response{bad, bad, bad}}
	st := Run(context.Background(), config(f, keywords(10), &clock{}))
	if st.Queries != 3 || st.Failures["key rejected"] != 3 || !strings.Contains(st.Stopped, "3 failures") {
		t.Fatalf("stats = %+v", st)
	}
}

func TestRunCapAndWindows(t *testing.T) {
	f := &fake{windows: map[source.Window]bool{source.AnyTime: true}}
	cfg := config(f, keywords(10), &clock{})
	cfg.Windows = []source.Window{source.PastWeek, source.AnyTime}
	cfg.MaxQueries = 4
	st := Run(context.Background(), cfg)
	if st.Queries != 4 || len(f.queries) != 4 || f.queries[0].Window != source.AnyTime || len(st.Unsupported) != 1 ||
		!strings.Contains(st.Stopped, "cap of 4") {
		t.Fatalf("stats = %+v", st)
	}
}

func TestRunInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cfg := config(&fake{}, keywords(10), &clock{})
	cfg.Sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	st := Run(ctx, cfg)
	if st.Stopped != "interrupted" || st.Queries != 1 {
		t.Fatalf("stats = %+v", st)
	}
}

func TestCheckKnownError(t *testing.T) {
	st := newStats("x", time.Time{})
	st.window(source.PastWeek).Refs["v:a"] = "en"
	if err := CheckKnown(context.Background(), []*Stats{st}, func(context.Context, string) (bool, error) { return false, errors.New("down") }); err == nil || st.Known != nil {
		t.Fatal("want the error, and Known unset")
	}
	if (&Stats{}).NewCount(nil) != -1 {
		t.Fatal("unchecked is -1")
	}
}
