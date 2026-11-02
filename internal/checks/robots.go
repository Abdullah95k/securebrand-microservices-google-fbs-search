// Package checks holds the spike's free, read-only checks: what
// facebook.com's robots.txt lets crawlers fetch, and how much of
// facebook.com Common Crawl holds.
package checks

import (
	"bufio"
	"strings"
)

// Robots is a parsed robots.txt (RFC 9309).
type Robots struct {
	groups []group
}

type group struct {
	agents []string // lower case
	rules  []rule
}

type rule struct {
	allow   bool
	pattern string
}

// ParseRobots reads a robots.txt.
func ParseRobots(body string) *Robots {
	r := &Robots{}
	var cur *group
	inRules := false
	sc := bufio.NewScanner(strings.NewReader(body))
	sc.Buffer(make([]byte, 64<<10), 1<<20)
	for sc.Scan() {
		line, _, _ := strings.Cut(sc.Text(), "#")
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		switch key {
		case "user-agent":
			if cur == nil || inRules {
				r.groups = append(r.groups, group{})
				cur = &r.groups[len(r.groups)-1]
				inRules = false
			}
			cur.agents = append(cur.agents, strings.ToLower(val))
		case "allow", "disallow":
			if cur == nil {
				continue
			}
			inRules = true
			if val == "" {
				continue // "Disallow:" allows everything
			}
			cur.rules = append(cur.rules, rule{allow: key == "allow", pattern: val})
		}
	}
	return r
}

// Agents lists the user agents the file names, in order.
func (r *Robots) Agents() []string {
	var out []string
	for _, g := range r.groups {
		out = append(out, g.agents...)
	}
	return out
}

// Named reports whether a group names the agent (not just "*").
func (r *Robots) Named(agent string) bool {
	a := strings.ToLower(agent)
	for _, g := range r.groups {
		for _, x := range g.agents {
			if x == a {
				return true
			}
		}
	}
	return false
}

// Allowed reports whether agent may fetch path (with its query string):
// the groups naming the agent apply, else the "*" groups; the longest
// matching rule wins, and allow wins a tie.
func (r *Robots) Allowed(agent, path string) bool {
	a := strings.ToLower(agent)
	var rules []rule
	for _, g := range r.groups {
		for _, x := range g.agents {
			if x == a {
				rules = append(rules, g.rules...)
				break
			}
		}
	}
	if rules == nil && !r.Named(agent) {
		for _, g := range r.groups {
			for _, x := range g.agents {
				if x == "*" {
					rules = append(rules, g.rules...)
					break
				}
			}
		}
	}
	best, allowed := -1, true
	for _, ru := range rules {
		if !match(ru.pattern, path) {
			continue
		}
		n := len(ru.pattern)
		if n > best || (n == best && ru.allow) {
			best, allowed = n, ru.allow
		}
	}
	return allowed
}

// match matches a robots.txt pattern: a path prefix where * matches any
// run of characters and a trailing $ anchors the end.
func match(pattern, path string) bool {
	anchored := strings.HasSuffix(pattern, "$")
	pattern = strings.TrimSuffix(pattern, "$")
	parts := strings.Split(pattern, "*")
	if !strings.HasPrefix(path, parts[0]) {
		return false
	}
	pos := len(parts[0])
	for i, p := range parts[1:] {
		last := i == len(parts)-2
		if last && anchored {
			return strings.HasSuffix(path[pos:], p)
		}
		j := strings.Index(path[pos:], p)
		if j < 0 {
			return false
		}
		pos += j + len(p)
	}
	return !anchored || pos == len(path)
}
