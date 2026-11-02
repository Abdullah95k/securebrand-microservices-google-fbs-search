package proxies

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const pass = "s3cretPass"

func write(t *testing.T, body string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "proxies.yaml")
	if err := os.WriteFile(p, []byte(body), mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoad(t *testing.T) {
	p := write(t, `fb:
  proxy:
    tiers:
      - name: isp
        kind: static_list
        endpoints:
          - "http://user:`+pass+`@203.0.113.10:8000"
          - "http://user:`+pass+`@203.0.113.11:8000"
      - name: residential
        kind: gateway
`, 0o600)
	exits, err := Load(p, "isp")
	if err != nil {
		t.Fatal(err)
	}
	if len(exits) != 2 || exits[1].Name != "exit 2" || exits[0].URL().Host != "203.0.113.10:8000" {
		t.Fatalf("exits = %v", exits)
	}
	if s := fmt.Sprintf("%v %+v %s", exits, exits[0], exits[1]); strings.Contains(s, pass) || strings.Contains(s, "203.0.113") {
		t.Fatalf("printing exits leaks: %s", s)
	}
}

func TestLoadErrorsNeverLeak(t *testing.T) {
	cases := map[string]struct {
		body string
		mode os.FileMode
		tier string
	}{
		"world readable": {"fb: {}", 0o644, "isp"},
		"bad yaml":       {"fb:\n  proxy: [\"http://user:" + pass + "@203.0.113.1:1\"\n  - :", 0o600, "isp"},
		"bad endpoint":   {"fb:\n  proxy:\n    tiers:\n      - {name: isp, kind: static_list, endpoints: [\"user:" + pass + "@203.0.113.1\"]}\n", 0o600, "isp"},
		"no tier":        {"fb:\n  proxy:\n    tiers: []\n", 0o600, "isp"},
		"gateway tier":   {"fb:\n  proxy:\n    tiers:\n      - {name: isp, kind: gateway}\n", 0o600, "isp"},
		"no endpoints":   {"fb:\n  proxy:\n    tiers:\n      - {name: isp, kind: static_list}\n", 0o600, "isp"},
	}
	for name, c := range cases {
		_, err := Load(write(t, c.body, c.mode), c.tier)
		if err == nil {
			t.Errorf("%s: want an error", name)
			continue
		}
		if strings.Contains(err.Error(), pass) || strings.Contains(err.Error(), "203.0.113") {
			t.Errorf("%s: error leaks: %v", name, err)
		}
	}
	if _, err := Load(filepath.Join(t.TempDir(), "missing.yaml"), "isp"); err == nil {
		t.Error("missing file: want an error")
	}
}
