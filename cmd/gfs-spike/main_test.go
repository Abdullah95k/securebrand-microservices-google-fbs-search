package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/spike"
)

func TestAPISources(t *testing.T) {
	dir := t.TempDir()
	if _, err := apiSources("auto", dir); err == nil {
		t.Fatal("no key files: want an error")
	}
	if err := os.WriteFile(filepath.Join(dir, "brave_api_key"), []byte("k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	srcs, err := apiSources("auto", dir)
	if err != nil || len(srcs) != 1 || srcs[0].Name() != "brave" {
		t.Fatalf("auto = %v, %v", srcs, err)
	}
	if _, err := apiSources("brave,mojeek", dir); err == nil {
		t.Fatal("a named source without its key file must fail")
	}
	if err := os.WriteFile(filepath.Join(dir, "vertex_api_key"), []byte("k\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := apiSources("vertex", dir); err == nil {
		t.Fatal("vertex needs its serving config file")
	}
	if err := os.WriteFile(filepath.Join(dir, "vertex_serving_config"), []byte("projects/p/x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if srcs, err := apiSources("auto", dir); err != nil || len(srcs) != 2 {
		t.Fatalf("auto = %v, %v", srcs, err)
	}
	if _, err := apiSources("bing", dir); err == nil {
		t.Fatal("unknown source")
	}
}

func TestParseWindows(t *testing.T) {
	ws, err := parseWindows("week,any")
	if err != nil || len(ws) != 2 {
		t.Fatalf("%v %v", ws, err)
	}
	if _, err := parseWindows("week,year"); err == nil {
		t.Fatal("year")
	}
}

func TestResultsFileIsPrivate(t *testing.T) {
	c := common{out: t.TempDir()}
	row, closeFn, path, err := c.resultsFile("x")
	if err != nil {
		t.Fatal(err)
	}
	if err := row(spike.Row{Time: time.Now(), Source: "brave", URL: "https://www.facebook.com/a", Ref: "v:a", Result: "page"}); err != nil {
		t.Fatal(err)
	}
	closeFn()
	closeFn() // twice is fine
	st, err := os.Stat(path)
	if err != nil || st.Mode().Perm() != 0o600 || st.Size() == 0 {
		t.Fatalf("results file %v %v", st, err)
	}
	c.noResults = true
	if row, _, path, err := c.resultsFile("x"); row != nil || path != "" || err != nil {
		t.Fatal("--no-results-file writes nothing")
	}
}
