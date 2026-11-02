// Command gfs-spike measures, before anything is built, how many new
// Facebook pages and accounts each search source finds for the fbs
// discovery keywords. See docs/spike.md.
//
//	gfs-spike checks    facebook.com's robots.txt and Common Crawl (free)
//	gfs-spike apis      Brave, Mojeek and Vertex AI Search, with keys in secrets/
//	gfs-spike google    a small probe of google.com through the ISP proxies (opt-in)
package main

import (
	"context"
	"encoding/csv"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/checks"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/fbs"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/proxies"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/secret"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/source"
	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/spike"
)

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{ReplaceAttr: iraqTime}))
	var err error
	switch os.Args[1] {
	case "checks":
		err = runChecks(ctx, os.Args[2:])
	case "apis":
		err = runAPIs(ctx, log, os.Args[2:])
	case "google":
		err = runGoogle(ctx, log, os.Args[2:])
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `usage: gfs-spike checks|apis|google [flags]   (gfs-spike <command> -h for flags; docs/spike.md)`)
}

// iraqTime prints log times in Iraq time.
func iraqTime(_ []string, a slog.Attr) slog.Attr {
	if a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
		return slog.String(slog.TimeKey, a.Value.Time().In(spike.Iraq).Format("2006-01-02 15:04:05")+" Iraq")
	}
	return a
}

func httpClient() *http.Client { return source.NewHTTPClient(http.DefaultTransport, 45*time.Second) }

// ---- checks

func runChecks(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("checks", flag.ExitOnError)
	robotsURL := fs.String("robots-url", "https://www.facebook.com/robots.txt", "robots.txt to read")
	sample := fs.Int("cc-sample", 2000, "Common Crawl captures to sample")
	_ = fs.Parse(args)

	w := os.Stdout
	p := func(format string, a ...any) { _, _ = fmt.Fprintf(w, format, a...) }
	p("Checks, %s Iraq time (UTC+3)\n\n== facebook.com robots.txt\n", time.Now().In(spike.Iraq).Format("2006-01-02 15:04"))
	rb, err := checks.FetchRobots(ctx, httpClient(), *robotsURL)
	if err != nil {
		p("could not read it: %v\n", err)
	} else {
		p("user agents named: %s\n", strings.Join(rb.Agents(), ", "))
		paths := []string{"/", "/somepage", "/people/Some-Name/100000000000001/", "/profile.php?id=100000000000001", "/somepage/posts/1", "/groups/1/"}
		p("may fetch (yes/no) %s\n", strings.Join(paths, "  "))
		for _, a := range []string{"Googlebot", "Bingbot", "CCBot", "MojeekBot", "Bravebot", "SomeOtherBot"} {
			var row []string
			for _, p := range paths {
				row = append(row, map[bool]string{true: "yes", false: "no"}[rb.Allowed(a, p)])
			}
			named := ""
			if !rb.Named(a) {
				named = " (not named: the * rules apply)"
			}
			p("  %-12s %s%s\n", a, strings.Join(row, " "), named)
		}
	}

	p("\n== Common Crawl\n")
	cc := &checks.CommonCrawl{HTTP: httpClient()}
	crawl, err := cc.Latest(ctx)
	if err != nil {
		p("could not read the crawl list: %v\n", err)
		return nil
	}
	rep, err := cc.Facebook(ctx, crawl, *sample)
	if err != nil {
		p("latest crawl %s (%s): could not read the index: %v\n", crawl.ID, crawl.Name, err)
		return nil
	}
	p("latest crawl %s (%s): about %d captures on *.facebook.com (%d index blocks)\n",
		crawl.ID, crawl.Name, rep.EstimatedCaptures(), rep.Blocks)
	p("sampled %d: by HTTP status %s\n", rep.Sampled, counts(rep.Statuses))
	p("sampled 200s, by what they map to: %s\n", counts(rep.Kinds))
	return nil
}

func counts(m map[string]int) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s %d", k, m[k]))
	}
	if len(parts) == 0 {
		return "none"
	}
	return strings.Join(parts, ", ")
}

// ---- shared: keywords, results file, report

type common struct {
	fbsURL, fbsKey, out string
	perLang             int
	seed                uint64
	noResults           bool
}

func (c *common) flags(fs *flag.FlagSet, perLang int) {
	fs.StringVar(&c.fbsURL, "fbs", "http://127.0.0.1:8080", "fbs API address")
	fs.StringVar(&c.fbsKey, "fbs-key-file", "secrets/fbs_api_key", "file holding the fbs API key (scope data:read)")
	fs.StringVar(&c.out, "out", "out", "directory for the report and the results file")
	fs.IntVar(&c.perLang, "per-lang", perLang, "keywords sampled per language")
	fs.Uint64Var(&c.seed, "seed", 1, "sample seed: the same seed picks the same keywords")
	fs.BoolVar(&c.noResults, "no-results-file", false, "don't write the results file (URLs found), only the counts")
}

func (c *common) setup(ctx context.Context) (*fbs.Client, []fbs.Keyword, spike.Header, error) {
	key, err := secret.ReadFile(c.fbsKey)
	if err != nil {
		return nil, nil, spike.Header{}, fmt.Errorf("fbs API key: %w", err)
	}
	client := &fbs.Client{Base: c.fbsURL, Key: key, HTTP: httpClient()}
	all, err := client.Keywords(ctx)
	if err != nil {
		return nil, nil, spike.Header{}, err
	}
	if len(all) == 0 {
		return nil, nil, spike.Header{}, errors.New("fbs lists no enabled keywords")
	}
	sample := spike.Sample(all, c.perLang, c.seed)
	h := spike.Header{Enabled: map[string]int{}, Sampled: map[string]int{}}
	for _, k := range all {
		h.Enabled[k.Language]++
	}
	for _, k := range sample {
		h.Sampled[k.Language]++
	}
	if err := os.MkdirAll(c.out, 0o700); err != nil {
		return nil, nil, spike.Header{}, fmt.Errorf("creating %s: %w", c.out, err)
	}
	return client, sample, h, nil
}

// resultsFile opens out/results-<name>-<time>.csv (mode 600). It holds the
// URLs found, the page or account behind each, the keyword and the time.
func (c *common) resultsFile(name string) (func(spike.Row) error, func(), string, error) {
	if c.noResults {
		return nil, func() {}, "", nil
	}
	path := filepath.Join(c.out, fmt.Sprintf("results-%s-%s.csv", name, time.Now().In(spike.Iraq).Format("20060102-1504")))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return nil, nil, "", fmt.Errorf("creating the results file: %w", err)
	}
	w := csv.NewWriter(f)
	_ = w.Write([]string{"time_iraq", "source", "window", "keyword_id", "language", "url", "ref", "result"})
	row := func(r spike.Row) error {
		return w.Write([]string{r.Time.In(spike.Iraq).Format("2006-01-02 15:04:05"), r.Source, string(r.Window),
			strconv.FormatInt(r.KeywordID, 10), r.Language, r.URL, r.Ref, r.Result})
	}
	var once sync.Once
	closeFn := func() {
		once.Do(func() {
			w.Flush()
			_ = f.Close()
		})
	}
	return row, closeFn, path, nil
}

func (c *common) report(ctx context.Context, client *fbs.Client, h spike.Header, runs []*spike.Stats, name string) error {
	for _, r := range runs {
		if err := r.CheckKnown(ctx, client.Known); err != nil {
			fmt.Fprintln(os.Stderr, "could not check which pages fbs already knows:", err)
			r.Known = nil
		}
	}
	path := filepath.Join(c.out, fmt.Sprintf("report-%s-%s.txt", name, time.Now().In(spike.Iraq).Format("20060102-1504")))
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return fmt.Errorf("creating the report: %w", err)
	}
	defer func() { _ = f.Close() }()
	spike.Report(io.MultiWriter(os.Stdout, f), h, runs)
	fmt.Fprintf(os.Stderr, "\nreport saved to %s\n", path)
	return nil
}

// ---- apis

func runAPIs(ctx context.Context, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("apis", flag.ExitOnError)
	var c common
	c.flags(fs, 10)
	sources := fs.String("sources", "auto", "comma-separated: brave, mojeek, vertex; auto = every one with a key file")
	dir := fs.String("secrets-dir", "secrets", "directory of the key files")
	windows := fs.String("windows", "week,any", "time windows to measure: day, week, month, any")
	maxQ := fs.Int("max-queries", 200, "hard cap on queries per source")
	minGap := fs.Duration("min-gap", 2*time.Second, "shortest pause between queries")
	maxGap := fs.Duration("max-gap", 5*time.Second, "longest pause between queries")
	_ = fs.Parse(args)

	ws, err := parseWindows(*windows)
	if err != nil {
		return err
	}
	srcs, err := apiSources(*sources, *dir)
	if err != nil {
		return err
	}
	client, sample, h, err := c.setup(ctx)
	if err != nil {
		return err
	}
	row, closeRows, path, err := c.resultsFile("apis")
	if err != nil {
		return err
	}
	defer closeRows()
	var runs []*spike.Stats
	for _, s := range srcs {
		log.Info("measuring", "source", s.Name(), "keywords", len(sample))
		runs = append(runs, spike.Run(ctx, spike.Config{Source: s, Keywords: sample, Windows: ws, MinGap: *minGap, MaxGap: *maxGap,
			MaxQueries: *maxQ, OnRow: row, Log: log}))
		if ctx.Err() != nil {
			break
		}
	}
	closeRows()
	if path != "" {
		fmt.Fprintf(os.Stderr, "results file (URLs found; delete it when done): %s\n", path)
	}
	return c.report(context.WithoutCancel(ctx), client, h, runs, "apis")
}

func apiSources(list, dir string) ([]source.Source, error) {
	names := []string{"brave", "mojeek", "vertex"}
	auto := list == "auto"
	if !auto {
		names = strings.Split(list, ",")
	}
	var out []source.Source
	for _, n := range names {
		n = strings.TrimSpace(n)
		key, err := secret.ReadFile(filepath.Join(dir, n+"_api_key"))
		if auto && errors.Is(err, secret.ErrMissing) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("%s: %w", n, err)
		}
		switch n {
		case "brave":
			out = append(out, &source.Brave{Key: key, HTTP: httpClient()})
		case "mojeek":
			out = append(out, &source.Mojeek{Key: key, HTTP: httpClient()})
		case "vertex":
			cfgPath := filepath.Join(dir, "vertex_serving_config")
			b, err := os.ReadFile(cfgPath) // #nosec G304 -- the operator's file
			if err != nil {
				return nil, fmt.Errorf("vertex: %s: write the serving config path into it (docs/spike.md)", cfgPath)
			}
			out = append(out, &source.Vertex{Key: key, HTTP: httpClient(), ServingConfig: strings.TrimSpace(string(b))})
		default:
			return nil, fmt.Errorf("unknown source %q (brave, mojeek or vertex)", n)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no key file found in %s/ (brave_api_key, mojeek_api_key, vertex_api_key): see docs/spike.md", dir)
	}
	return out, nil
}

func parseWindows(s string) ([]source.Window, error) {
	var out []source.Window
	for _, p := range strings.Split(s, ",") {
		w, err := source.ParseWindow(p)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, nil
}

// ---- google

func runGoogle(ctx context.Context, log *slog.Logger, args []string) error {
	fs := flag.NewFlagSet("google", flag.ExitOnError)
	var c common
	c.flags(fs, 3)
	accept := fs.Bool("accept-google-terms-risk", false, "required: Google's terms don't allow automated queries (docs/spike.md)")
	proxyFile := fs.String("proxies", "../securebrand-microservices/phase0/proxies.yaml", "fbs's proxy file (read in place, never copied)")
	tier := fs.String("tier", "isp", "proxy tier to use (a static_list tier)")
	window := fs.String("window", "week", "time window: day, week, month or any")
	maxQ := fs.Int("max-queries", 12, "hard cap on queries")
	minGap := fs.Duration("min-gap", 3*time.Minute, "shortest pause between queries")
	maxGap := fs.Duration("max-gap", 5*time.Minute, "longest pause between queries")
	_ = fs.Parse(args)
	if !*accept {
		return errors.New("google's terms don't allow automated queries: this probe runs only with --accept-google-terms-risk (read docs/spike.md first)")
	}
	if *minGap < time.Minute {
		return errors.New("--min-gap must be at least 1m")
	}
	w, err := source.ParseWindow(*window)
	if err != nil {
		return err
	}
	exits, err := proxies.Load(*proxyFile, *tier)
	if err != nil {
		return err
	}
	client, sample, h, err := c.setup(ctx)
	if err != nil {
		return err
	}
	row, closeRows, path, err := c.resultsFile("google")
	if err != nil {
		return err
	}
	defer closeRows()
	log.Info("probing google", "exits", len(exits), "keywords", len(sample), "max_queries", *maxQ,
		"gap", minGap.String()+"-"+maxGap.String())
	st := spike.Run(ctx, spike.Config{Source: source.NewGoogle(exits, 45*time.Second), Keywords: sample,
		Windows: []source.Window{w}, MinGap: *minGap, MaxGap: *maxGap, MaxQueries: *maxQ, OnRow: row, Log: log})
	closeRows()
	if path != "" {
		fmt.Fprintf(os.Stderr, "results file (URLs found; delete it when done): %s\n", path)
	}
	return c.report(context.WithoutCancel(ctx), client, h, []*spike.Stats{st}, "google")
}
