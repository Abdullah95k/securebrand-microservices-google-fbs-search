// Package spike runs the measurement: the same sample of keywords through
// one search source at a time, at a polite pace, counting what comes back
// and what it maps to. It stops at the first CAPTCHA or bot check.
package spike

import (
	"cmp"
	"context"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/fbs"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/fburl"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/source"
)

// Iraq is Iraq's time zone (UTC+3, no daylight saving time).
var Iraq = time.FixedZone("Iraq time (UTC+3)", 3*3600)

// Languages in report order.
var Languages = []string{"en", "ar", "ckb", "kmr"}

// Sample picks up to perLang keywords of each language, at random but the
// same for the same seed, so every source is measured on the same terms.
func Sample(kws []fbs.Keyword, perLang int, seed uint64) []fbs.Keyword {
	by := map[string][]fbs.Keyword{}
	for _, k := range kws {
		by[k.Language] = append(by[k.Language], k)
	}
	var out []fbs.Keyword
	langs := make([]string, 0, len(by))
	for l := range by {
		langs = append(langs, l)
	}
	slices.Sort(langs)
	for _, l := range langs {
		list := slices.Clone(by[l])
		slices.SortFunc(list, func(a, b fbs.Keyword) int { return cmp.Compare(a.ID, b.ID) })
		r := rand.New(rand.NewPCG(seed, uint64(len(l)))) // #nosec G404 -- sampling, not security
		r.Shuffle(len(list), func(i, j int) { list[i], list[j] = list[j], list[i] })
		out = append(out, list[:min(perLang, len(list))]...)
	}
	slices.SortStableFunc(out, func(a, b fbs.Keyword) int {
		return cmp.Or(cmp.Compare(langRank(a.Language), langRank(b.Language)), cmp.Compare(a.ID, b.ID))
	})
	return out
}

func langRank(l string) int {
	if i := slices.Index(Languages, l); i >= 0 {
		return i
	}
	return len(Languages)
}

// Row is one result, for the local results file: only the URL, the page
// or account behind it, the keyword and the time. No titles, no text.
type Row struct {
	Time      time.Time
	Source    string
	Window    source.Window
	KeywordID int64
	Language  string
	URL       string
	Ref       string
	// Result is "page", "post" or "skip:<reason>".
	Result string
}

// Config is one run.
type Config struct {
	Source   source.Source
	Keywords []fbs.Keyword
	Windows  []source.Window
	// MinGap and MaxGap bound the random pause between queries.
	MinGap, MaxGap time.Duration
	// MaxQueries is a hard cap on queries sent.
	MaxQueries int
	// RateLimitPause is the first pause after an API's "too many
	// requests"; it doubles on each one in a row (default 2 minutes).
	RateLimitPause time.Duration
	OnRow          func(Row) error
	Log            *slog.Logger
	// Sleep and Now are replaced in tests.
	Sleep func(ctx context.Context, d time.Duration) error
	Now   func() time.Time
	Rand  *rand.Rand
}

// Stop rules.
const (
	maxRateLimitedInRow = 3
	maxFailedInRow      = 3
)

// Run sends the queries and returns what came back. It returns early,
// with Stats.Stopped set, on a CAPTCHA or bot check, on repeated rate
// limits or failures, or when ctx ends.
func Run(ctx context.Context, cfg Config) *Stats {
	if cfg.Sleep == nil {
		cfg.Sleep = sleep
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.New(rand.NewPCG(rand.Uint64(), rand.Uint64())) // #nosec G404 -- jitter
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.RateLimitPause <= 0 {
		cfg.RateLimitPause = 2 * time.Minute
	}
	st := newStats(cfg.Source.Name(), cfg.Now())
	defer func() { st.Ended = cfg.Now() }()

	var windows []source.Window
	for _, w := range cfg.Windows {
		if cfg.Source.Supports(w) {
			windows = append(windows, w)
		} else {
			st.Unsupported = append(st.Unsupported, w)
		}
	}
	total := min(len(cfg.Keywords)*len(windows), cfg.MaxQueries)
	limitedInRow, failedInRow := 0, 0
	for _, kw := range cfg.Keywords {
		for _, w := range windows {
			if st.Queries >= cfg.MaxQueries {
				st.Stopped = fmt.Sprintf("the cap of %d queries was reached", cfg.MaxQueries)
				return st
			}
			if st.Queries > 0 {
				if err := cfg.Sleep(ctx, gap(cfg.Rand, cfg.MinGap, cfg.MaxGap)); err != nil {
					st.Stopped = "interrupted"
					return st
				}
			}
			resp := cfg.Source.Search(ctx, source.Query{Term: kw.Term, Lang: kw.Language, Window: w})
			if ctx.Err() != nil {
				st.Stopped = "interrupted"
				return st
			}
			st.Queries++
			st.Outcomes[resp.Outcome]++
			log := cfg.Log.With("query", fmt.Sprintf("%d/%d", st.Queries, total), "source", st.Source, "window", w,
				"keyword_id", kw.ID, "lang", kw.Language, "outcome", resp.Outcome)
			if resp.Via != "" {
				log = log.With("via", resp.Via)
			}
			switch {
			case resp.Outcome.Wall():
				log.Warn("stopped: " + resp.Detail)
				st.Stopped = fmt.Sprintf("%s after %d answered queries (%s); no retry, stopped for the day",
					resp.Outcome, st.Outcomes[source.OK], resp.Detail)
				st.WallAfter = st.Outcomes[source.OK]
				return st
			case resp.Outcome == source.RateLimited:
				limitedInRow++
				if limitedInRow >= maxRateLimitedInRow {
					log.Warn("stopped: rate limited 3 times in a row")
					st.Stopped = "rate limited 3 times in a row"
					return st
				}
				pause := cfg.RateLimitPause << (limitedInRow - 1)
				log.Warn("rate limited: this query is dropped, pausing", "pause", pause.String())
				if err := cfg.Sleep(ctx, pause); err != nil {
					st.Stopped = "interrupted"
					return st
				}
				continue
			case resp.Outcome != source.OK:
				failedInRow++
				st.Failures[resp.Detail]++
				log.Warn("failed: "+resp.Detail, "status", resp.Status)
				if failedInRow >= maxFailedInRow {
					st.Stopped = fmt.Sprintf("%d failures in a row (%s)", maxFailedInRow, resp.Detail)
					return st
				}
				continue
			}
			limitedInRow, failedInRow = 0, 0
			ws := st.window(w)
			fb, mapped := ws.add(kw.Language, resp.URLs, func(u string, res fburl.Result) error {
				if cfg.OnRow == nil {
					return nil
				}
				row := Row{Time: cfg.Now(), Source: st.Source, Window: w, KeywordID: kw.ID, Language: kw.Language,
					URL: u, Ref: res.Ref, Result: string(res.Kind)}
				if !res.OK() {
					row.Result = "skip:" + string(res.Skip)
				}
				return cfg.OnRow(row)
			})
			log.Info("answered", "results", len(resp.URLs), "facebook", fb, "pages", mapped)
		}
	}
	return st
}

func gap(r *rand.Rand, lo, hi time.Duration) time.Duration {
	if hi <= lo {
		return lo
	}
	return lo + time.Duration(r.Int64N(int64(hi-lo)))
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
