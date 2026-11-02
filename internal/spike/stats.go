package spike

import (
	"context"
	"slices"
	"time"

	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/fburl"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/source"
)

// Stats is what one run measured. It holds counts and page references;
// no result URLs (those go to the results file only).
type Stats struct {
	Source         string
	Started, Ended time.Time
	Queries        int
	Outcomes       map[source.Outcome]int
	Failures       map[string]int
	// Stopped says why the run ended early; empty when it finished.
	Stopped string
	// WallAfter is how many queries were answered before a CAPTCHA or bot
	// check, when one stopped the run.
	WallAfter   int
	Unsupported []source.Window
	Windows     []*WindowStats
	// Known says, per reference found, whether fbs already knows it. Nil
	// when it wasn't checked.
	Known map[string]bool
}

// WindowStats are the answered queries of one time window.
type WindowStats struct {
	Window   source.Window
	Answered int
	// Empty counts answered queries without a single result.
	Empty   int
	Results int
	// Facebook counts results on a facebook.com host.
	Facebook int
	Kinds    map[fburl.Kind]int
	Skips    map[fburl.Skip]int
	// Refs are the distinct pages and accounts found, with the language
	// of the keyword that found each first.
	Refs   map[string]string
	ByLang map[string]*LangStats
}

// LangStats are one language's share of a window.
type LangStats struct {
	Answered, Results, Facebook int
	Refs                        map[string]bool
}

func newStats(src string, now time.Time) *Stats {
	return &Stats{Source: src, Started: now, Outcomes: map[source.Outcome]int{}, Failures: map[string]int{}}
}

func (s *Stats) window(w source.Window) *WindowStats {
	for _, ws := range s.Windows {
		if ws.Window == w {
			return ws
		}
	}
	ws := &WindowStats{Window: w, Kinds: map[fburl.Kind]int{}, Skips: map[fburl.Skip]int{}, Refs: map[string]string{},
		ByLang: map[string]*LangStats{}}
	s.Windows = append(s.Windows, ws)
	return ws
}

// add counts one answered query's results; row is called for each.
func (ws *WindowStats) add(lang string, urls []string, row func(string, fburl.Result) error) (fb, mapped int) {
	ls := ws.ByLang[lang]
	if ls == nil {
		ls = &LangStats{Refs: map[string]bool{}}
		ws.ByLang[lang] = ls
	}
	ws.Answered++
	ls.Answered++
	if len(urls) == 0 {
		ws.Empty++
	}
	ws.Results += len(urls)
	ls.Results += len(urls)
	for _, u := range urls {
		res := fburl.Classify(u)
		_ = row(u, res) // the results file is a convenience; a write error doesn't stop the run
		if res.Skip == fburl.SkipNotFacebook {
			continue
		}
		fb++
		if !res.OK() {
			ws.Skips[res.Skip]++
			continue
		}
		mapped++
		ws.Kinds[res.Kind]++
		ls.Refs[res.Ref] = true
		if _, ok := ws.Refs[res.Ref]; !ok {
			ws.Refs[res.Ref] = lang
		}
	}
	ws.Facebook += fb
	ls.Facebook += fb
	return fb, mapped
}

// AllRefs are the distinct references found in every window, sorted.
func (s *Stats) AllRefs() []string {
	seen := map[string]bool{}
	for _, ws := range s.Windows {
		for r := range ws.Refs {
			seen[r] = true
		}
	}
	out := make([]string, 0, len(seen))
	for r := range seen {
		out = append(out, r)
	}
	slices.Sort(out)
	return out
}

// CheckKnown asks fbs about every reference found. It stops at the first
// error and returns it; Known keeps what was answered.
func (s *Stats) CheckKnown(ctx context.Context, known func(context.Context, string) (bool, error)) error {
	s.Known = map[string]bool{}
	for _, r := range s.AllRefs() {
		ok, err := known(ctx, r)
		if err != nil {
			return err
		}
		s.Known[r] = ok
	}
	return nil
}

// NewCount counts refs fbs doesn't know; -1 when unchecked.
func (s *Stats) NewCount(refs map[string]bool) int {
	if s.Known == nil {
		return -1
	}
	n := 0
	for r := range refs {
		if known, ok := s.Known[r]; ok && !known {
			n++
		}
	}
	return n
}
