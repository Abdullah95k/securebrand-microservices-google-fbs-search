package spike

import (
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"
	"text/tabwriter"

	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/fburl"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/source"
)

// Header describes the keyword sample.
type Header struct {
	// Enabled and Sampled count keywords by language.
	Enabled, Sampled map[string]int
}

// Report writes the runs' numbers in plain text: counts only, no URLs.
func Report(w io.Writer, h Header, runs []*Stats) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	if len(runs) > 0 {
		first, last := runs[0].Started, runs[0].Ended
		for _, r := range runs {
			if r.Started.Before(first) {
				first = r.Started
			}
			if r.Ended.After(last) {
				last = r.Ended
			}
		}
		p("Search spike, %s to %s Iraq time (UTC+3)\n", first.In(Iraq).Format("2006-01-02 15:04"), last.In(Iraq).Format("15:04"))
	}
	var parts []string
	totalS, totalE := 0, 0
	for _, l := range Languages {
		parts = append(parts, fmt.Sprintf("%s %d of %d", l, h.Sampled[l], h.Enabled[l]))
		totalS += h.Sampled[l]
		totalE += h.Enabled[l]
	}
	p("Keywords: %d sampled of %d enabled (%s)\n", totalS, totalE, strings.Join(parts, ", "))

	for _, r := range runs {
		p("\n== %s\n", r.Source)
		p("queries sent %d: answered %d, CAPTCHA %d, bot check %d, consent wall %d, rate limited %d, failed %d\n",
			r.Queries, r.Outcomes[source.OK], r.Outcomes[source.Captcha], r.Outcomes[source.BotCheck],
			r.Outcomes[source.Consent], r.Outcomes[source.RateLimited], r.Outcomes[source.Failed])
		if r.Stopped != "" {
			p("STOPPED: %s\n", r.Stopped)
		}
		for _, d := range slices.Sorted(maps.Keys(r.Failures)) {
			p("  failure x%d: %s\n", r.Failures[d], d)
		}
		for _, uw := range r.Unsupported {
			p("(no %q filter on this source: not measured)\n", uw)
		}
		for _, ws := range r.Windows {
			writeWindow(w, r, ws)
		}
		if r.Known != nil {
			all := map[string]bool{}
			for _, ref := range r.AllRefs() {
				all[ref] = true
			}
			p("all windows: %d distinct pages/accounts, %d not yet known to fbs\n", len(all), r.NewCount(all))
		}
	}
	writeOverlap(w, runs)
}

func writeWindow(w io.Writer, r *Stats, ws *WindowStats) {
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	p("\n-- window: %s (answered queries %d)\n", ws.Window, ws.Answered)
	p("results %d (%s a query; %d queries with none)\n", ws.Results, per(ws.Results, ws.Answered), ws.Empty)
	p("on facebook.com %d (%s of results)\n", ws.Facebook, pct(ws.Facebook, ws.Results))
	p("  mapped to a page/account: %d (page or account URL %d, post/photo/video naming its owner %d)\n",
		ws.Kinds[fburl.KindPage]+ws.Kinds[fburl.KindPost], ws.Kinds[fburl.KindPage], ws.Kinds[fburl.KindPost])
	p("  skipped: group %d, event %d, marketplace %d, no owner in URL %d, Facebook's own pages %d, malformed %d\n",
		ws.Skips[fburl.SkipGroup], ws.Skips[fburl.SkipEvent], ws.Skips[fburl.SkipMarketplace], ws.Skips[fburl.SkipNoOwner],
		ws.Skips[fburl.SkipNotAccount], ws.Skips[fburl.SkipInvalid])
	refs := map[string]bool{}
	for ref := range ws.Refs {
		refs[ref] = true
	}
	p("distinct pages/accounts %d (%s a query)", len(refs), per(len(refs), ws.Answered))
	if n := r.NewCount(refs); n >= 0 {
		p(", not yet known to fbs %d (%s a query)", n, per(n, ws.Answered))
	}
	p("\n")
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', tabwriter.AlignRight)
	_, _ = fmt.Fprintln(tw, "language\tanswered\tresults\ton facebook\tdistinct\tnew\t")
	for _, l := range Languages {
		ls := ws.ByLang[l]
		if ls == nil {
			continue
		}
		newCol := "-"
		if n := r.NewCount(ls.Refs); n >= 0 {
			newCol = fmt.Sprint(n)
		}
		_, _ = fmt.Fprintf(tw, "%s\t%d\t%d\t%d\t%d\t%s\t\n", l, ls.Answered, ls.Results, ls.Facebook, len(ls.Refs), newCol)
	}
	_ = tw.Flush()
}

func writeOverlap(w io.Writer, runs []*Stats) {
	if len(runs) < 2 {
		return
	}
	sets := make([]map[string]bool, len(runs))
	for i, r := range runs {
		sets[i] = map[string]bool{}
		for _, ref := range r.AllRefs() {
			sets[i][ref] = true
		}
	}
	_, _ = fmt.Fprintf(w, "\n== overlap (distinct pages/accounts found by both)\n")
	for i := range runs {
		for j := i + 1; j < len(runs); j++ {
			n := 0
			for ref := range sets[i] {
				if sets[j][ref] {
					n++
				}
			}
			_, _ = fmt.Fprintf(w, "%s and %s: %d (of %d and %d)\n", runs[i].Source, runs[j].Source, n, len(sets[i]), len(sets[j]))
		}
	}
}

func per(n, d int) string {
	if d == 0 {
		return "-"
	}
	return fmt.Sprintf("%.1f", float64(n)/float64(d))
}

func pct(n, d int) string {
	if d == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*float64(n)/float64(d))
}
