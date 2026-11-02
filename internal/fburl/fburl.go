// Package fburl turns a facebook.com URL found by a search engine into the
// page or account behind it, in the reference form fbs uses: "v:<vanity>"
// (lower case) or "id:<numeric id>".
//
// It never fetches anything: a URL that does not name its owner (a reel,
// a /watch/?v= video, a share link) is skipped, not resolved. Groups,
// events, Marketplace and Facebook's own pages (help, login, search...) are
// skipped too.
package fburl

import (
	"net/url"
	"strconv"
	"strings"
)

// Kind says how a reference was found.
type Kind string

const (
	// KindPage: the URL is the page or account itself (a vanity,
	// profile.php?id=, /people/, /p/, /pages/...), or one of its tabs.
	KindPage Kind = "page"
	// KindPost: the URL is a post, photo or video, and names its owner.
	KindPost Kind = "post"
)

// Skip says why a URL gave no reference.
type Skip string

const (
	SkipNotFacebook Skip = "not_facebook"
	SkipGroup       Skip = "group"
	SkipEvent       Skip = "event"
	SkipMarketplace Skip = "marketplace"
	// SkipNoOwner: a post, reel, video or share link whose URL doesn't
	// name the page or account that posted it.
	SkipNoOwner Skip = "no_owner"
	// SkipNotAccount: Facebook's own pages (home, help, login, search,
	// hashtags...), or a subdomain that serves no pages.
	SkipNotAccount Skip = "not_account"
	// SkipInvalid: looks like an account URL but its name or id is malformed.
	SkipInvalid Skip = "invalid"
)

// Result is what a URL maps to: Ref and Kind, or Skip.
type Result struct {
	Ref  string
	Kind Kind
	Skip Skip
}

// OK reports whether the URL mapped to a page or account.
func (r Result) OK() bool { return r.Ref != "" }

func page(ref string) Result { return Result{Ref: ref, Kind: KindPage} }
func post(ref string) Result { return Result{Ref: ref, Kind: KindPost} }
func skip(s Skip) Result     { return Result{Skip: s} }

// Classify maps one URL to the page or account behind it.
func Classify(raw string) Result {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return skip(SkipNotFacebook)
	}
	switch s := hostKind(strings.ToLower(u.Hostname())); s {
	case hostContent:
	case hostShort:
		return skip(SkipNoOwner)
	case hostOther:
		return skip(SkipNotAccount)
	default:
		return skip(SkipNotFacebook)
	}
	seg := segments(u.Path)
	if len(seg) == 0 {
		return skip(SkipNotAccount)
	}
	q := u.Query()
	first := strings.ToLower(seg[0])
	switch first {
	case "profile.php":
		return idOr(q.Get("id"), KindPage, SkipInvalid)
	case "people":
		// /people/<Name>/<id>/
		if len(seg) >= 3 {
			return idOr(seg[2], KindPage, SkipInvalid)
		}
		return skip(SkipInvalid)
	case "pages":
		return pagesPath(seg)
	case "p":
		// /p/<Name>-<id>/: pages without a vanity.
		if len(seg) >= 2 {
			if ref, ok := dashID(seg[1]); ok {
				return page(ref)
			}
		}
		return skip(SkipInvalid)
	case "pg":
		// /pg/<vanity>/posts/: the old page URL.
		if len(seg) >= 2 {
			return nameOr(seg[1], KindPage)
		}
		return skip(SkipInvalid)
	case "permalink.php", "story.php":
		return idOr(q.Get("id"), KindPost, SkipNoOwner)
	case "photo.php", "photo", "media":
		if ref, ok := setOwner(q.Get("set")); ok {
			return post(ref)
		}
		return idOr(q.Get("id"), KindPost, SkipNoOwner)
	case "watch":
		// /watch/<vanity>/ is a page's video tab; /watch/?v= names no one.
		if len(seg) >= 2 && !reserved[strings.ToLower(seg[1])] && !watchReserved[strings.ToLower(seg[1])] {
			return nameOr(seg[1], KindPage)
		}
		return skip(SkipNoOwner)
	case "notes":
		// /notes/<vanity>/<title>/<id>/
		if len(seg) >= 3 {
			return nameOr(seg[1], KindPost)
		}
		return skip(SkipNoOwner)
	case "groups", "group.php":
		return skip(SkipGroup)
	case "events", "event.php":
		return skip(SkipEvent)
	case "marketplace", "commerce":
		return skip(SkipMarketplace)
	}
	if noOwner[first] {
		return skip(SkipNoOwner)
	}
	if reserved[first] || strings.HasSuffix(first, ".php") {
		return skip(SkipNotAccount)
	}
	kind := KindPage
	if len(seg) >= 2 && postTabs[strings.ToLower(seg[1])] {
		kind = KindPost
	}
	return nameOr(seg[0], kind)
}

// nameOr maps the name segment of a page URL to a reference: a vanity, a
// numeric id, or "<Name-With-Dashes>-<id>" (older pages without a vanity).
func nameOr(s string, k Kind) Result {
	if ref, ok := dashID(s); ok {
		return Result{Ref: ref, Kind: k}
	}
	return vanityOr(s, k)
}

type hostClass int

const (
	hostNotFacebook hostClass = iota
	hostContent               // serves pages and posts
	hostShort                 // short links that name no owner
	hostOther                 // a Facebook host that serves no pages
)

func hostKind(h string) hostClass {
	switch h {
	case "fb.me", "fb.watch", "fb.gg":
		return hostShort
	case "fb.com", "www.fb.com", "facebook.com":
		return hostContent
	}
	sub, ok := strings.CutSuffix(h, ".facebook.com")
	if !ok {
		return hostNotFacebook
	}
	switch sub {
	case "www", "m", "mbasic", "mobile", "touch", "web", "free":
		return hostContent
	}
	if isLocaleHost(sub) {
		return hostContent // ar-ar., ku-tr., cb-iq. ...
	}
	return hostOther
}

// isLocaleHost matches a language subdomain such as ar-ar or cb-iq.
func isLocaleHost(s string) bool {
	a, b, ok := strings.Cut(s, "-")
	return ok && len(a) >= 2 && len(a) <= 3 && len(b) == 2 && isLower(a) && isLower(b)
}

func isLower(s string) bool {
	for _, r := range s {
		if r < 'a' || r > 'z' {
			return false
		}
	}
	return true
}

func segments(p string) []string {
	var out []string
	for _, s := range strings.Split(p, "/") {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// pagesPath handles /pages/<name>/<id>/ and
// /pages/category/<category>/<Name>-<id>/.
func pagesPath(seg []string) Result {
	if len(seg) >= 4 && strings.EqualFold(seg[1], "category") {
		if ref, ok := idRef(seg[3]); ok {
			return page(ref)
		}
		if ref, ok := dashID(seg[3]); ok {
			return page(ref)
		}
		return skip(SkipInvalid)
	}
	if len(seg) >= 3 {
		if ref, ok := idRef(seg[2]); ok {
			return page(ref)
		}
		return skip(SkipInvalid)
	}
	return skip(SkipNotAccount)
}

// setOwner reads the owner from a photo's set parameter where it carries
// one: "pb.<owner>.-2207520000" (profile photos), or the old four-part album
// form "a.<album>.<n>.<owner>". The newer "a.<album>" names no owner.
func setOwner(set string) (string, bool) {
	parts := strings.Split(set, ".")
	switch {
	case len(parts) >= 2 && parts[0] == "pb":
		return idRef(parts[1])
	case len(parts) == 4 && parts[0] == "a":
		return idRef(parts[3])
	}
	return "", false
}

func idOr(s string, k Kind, otherwise Skip) Result {
	if ref, ok := idRef(s); ok {
		return Result{Ref: ref, Kind: k}
	}
	return skip(otherwise)
}

// idRef is the canonical reference of a numeric id, as fbs writes it.
func idRef(s string) (string, bool) {
	if s == "" || s[0] == '0' || s[0] == '+' || s[0] == '-' {
		return "", false
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil || id <= 0 {
		return "", false
	}
	return "id:" + strconv.FormatInt(id, 10), true
}

// dashID reads "<Name>-<id>" (the name may hold any script); the id has
// at least 6 digits, so a vanity like "page-2024" is not taken for one.
func dashID(s string) (string, bool) {
	i := strings.LastIndexByte(s, '-')
	if i < 1 || len(s)-i-1 < 6 {
		return "", false
	}
	return idRef(s[i+1:])
}

// vanityOr maps a vanity (or a numeric id in its place) to a reference.
// Facebook usernames are ASCII letters, digits and dots.
func vanityOr(s string, k Kind) Result {
	if ref, ok := idRef(s); ok {
		return Result{Ref: ref, Kind: k}
	}
	if strings.Trim(s, ".") == "" || len(s) > 50 {
		return skip(SkipInvalid)
	}
	for _, r := range s {
		if r != '.' && (r < '0' || r > '9') && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
			return skip(SkipInvalid)
		}
	}
	return Result{Ref: "v:" + strings.ToLower(s), Kind: k}
}

// postTabs are second path segments that make a URL a post of the page
// named by the first: /<vanity>/posts/..., /<vanity>/videos/...
var postTabs = set("posts", "photos", "photo", "photo.php", "videos", "video", "reel", "reels", "story", "stories",
	"permalink", "permalink.php", "notes", "activity", "live")

// noOwner are first segments of content URLs that don't name the owner.
var noOwner = set("reel", "reels", "share", "share.php", "sharer", "sharer.php", "videos", "video.php", "stories",
	"story", "posts", "photos", "l.php", "lite", "s")

// watchReserved are /watch/<x>/ sections that are not pages.
var watchReserved = set("live", "latest", "saved", "shows", "topic", "search", "videos", "explore", "home")

// reserved are first segments that are Facebook's own pages, not accounts.
var reserved = set(
	"about", "accounts", "ad_campaign", "ads", "adsmanager", "ajax", "api", "appcenter", "apps", "biz",
	"bookmarks", "business", "business_help", "campaign", "careers", "checkpoint", "climatescienceinfo",
	"communitystandards", "composer", "confirmemail.php", "connect", "contact_importer", "cookie", "cookies",
	"coronavirus_info", "dialog", "directory", "donate", "facebook", "feeds", "find-friends", "friends",
	"fundraisers", "gaming", "games", "hashtag", "help", "home", "home.php", "index.php", "instantgames",
	"jobs", "latest", "legal", "live", "login", "login.php", "logout.php", "me", "memories", "messages",
	"messenger", "mobile", "notifications", "oauth", "offers", "places", "plugins", "policies", "policy.php",
	"privacy", "public", "r.php", "recover", "reg", "rsrc.php", "safety", "saved", "search", "settings",
	"shops", "signup", "terms", "terms.php", "topic", "tr", "transparency", "ufi", "unsupportedbrowser",
	"votinginformationcenter", "watchparty", "zero", "fbml", "v2.0", "privacy_sandbox", "pages_reaction_units",
)

func set(xs ...string) map[string]bool {
	m := make(map[string]bool, len(xs))
	for _, x := range xs {
		m[x] = true
	}
	return m
}
