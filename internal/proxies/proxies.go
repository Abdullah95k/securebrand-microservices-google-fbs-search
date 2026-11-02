// Package proxies reads the static proxy exits of one tier from fbs's
// proxies.yaml (phase0/proxies.yaml on the pilot server), in place: the
// file is never copied, and no endpoint, user name or password ever
// appears in an error, a log line or the output. Exits are named by their
// position ("exit 1", "exit 2", ...).
package proxies

import (
	"fmt"
	"net/url"
	"os"

	"gopkg.in/yaml.v3"
)

// Exit is one proxy endpoint.
type Exit struct {
	// Name is "exit N", safe to print.
	Name string
	url  *url.URL
}

// URL returns the proxy URL, credentials included, for http.Transport.
// Never print it.
func (e Exit) URL() *url.URL { return e.url }

// String prints the safe name only.
func (e Exit) String() string { return e.Name }

type file struct {
	FB struct {
		Proxy struct {
			Tiers []struct {
				Name      string   `yaml:"name"`
				Kind      string   `yaml:"kind"`
				Endpoints []string `yaml:"endpoints"`
			} `yaml:"tiers"`
		} `yaml:"proxy"`
	} `yaml:"fb"`
}

// Load returns the exits of the static_list tier named tier.
func Load(path, tier string) ([]Exit, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("%s: cannot read the proxy file", path)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s: other users can read it; run: chmod 600 %s", path, path)
	}
	b, err := os.ReadFile(path) // #nosec G304 -- the operator names the file
	if err != nil {
		return nil, fmt.Errorf("%s: cannot read the proxy file", path)
	}
	var f file
	if err := yaml.Unmarshal(b, &f); err != nil {
		// yaml errors quote the offending line, which may hold a password.
		return nil, fmt.Errorf("%s: not valid YAML", path)
	}
	for _, t := range f.FB.Proxy.Tiers {
		if t.Name != tier {
			continue
		}
		if t.Kind != "static_list" {
			return nil, fmt.Errorf("%s: tier %q is %q; only static_list tiers are supported", path, tier, t.Kind)
		}
		var out []Exit
		for i, e := range t.Endpoints {
			u, err := url.Parse(e)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") || u.Hostname() == "" {
				return nil, fmt.Errorf("%s: tier %q endpoint %d is not a URL like http://user:pass@host:port", path, tier, i+1)
			}
			out = append(out, Exit{Name: fmt.Sprintf("exit %d", i+1), url: u})
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("%s: tier %q has no endpoints", path, tier)
		}
		return out, nil
	}
	return nil, fmt.Errorf("%s: no tier named %q", path, tier)
}
