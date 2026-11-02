package fburl

import "testing"

func TestClassify(t *testing.T) {
	cases := []struct {
		in   string
		want Result
	}{
		// Pages and accounts.
		{"https://www.facebook.com/SomeBrand", page("v:somebrand")},
		{"https://www.facebook.com/some.brand/", page("v:some.brand")},
		{"https://m.facebook.com/somebrand?locale=ar_AR", page("v:somebrand")},
		{"https://ar-ar.facebook.com/somebrand/about/", page("v:somebrand")},
		{"https://cb-iq.facebook.com/somebrand", page("v:somebrand")},
		{"https://web.facebook.com/somebrand/reviews", page("v:somebrand")},
		{"http://facebook.com/somebrand", page("v:somebrand")},
		{"https://fb.com/somebrand", page("v:somebrand")},
		{"https://www.facebook.com/profile.php?id=100064123456789", page("id:100064123456789")},
		{"https://www.facebook.com/profile.php?id=100064123456789&sk=photos", page("id:100064123456789")},
		{"https://www.facebook.com/people/Some-Name/100083123456789/", page("id:100083123456789")},
		{"https://www.facebook.com/p/Some-Name-100083123456789/", page("id:100083123456789")},
		{"https://www.facebook.com/pages/Some-Name/123456789012", page("id:123456789012")},
		{"https://www.facebook.com/pages/category/Restaurant/Some-Name-123456789012/", page("id:123456789012")},
		{"https://www.facebook.com/pages/category/Restaurant/123456789012/", page("id:123456789012")},
		{"https://www.facebook.com/Some-Old-Page-123456789012/", page("id:123456789012")},
		{"https://www.facebook.com/%D9%85%D8%B7%D8%B9%D9%85-%D8%A8%D8%BA%D8%AF%D8%A7%D8%AF-123456789012/", page("id:123456789012")},
		{"https://www.facebook.com/123456789012", page("id:123456789012")},
		{"https://www.facebook.com/pg/somebrand/posts/", page("v:somebrand")},
		{"https://www.facebook.com/watch/somebrand/", page("v:somebrand")},

		// Posts, photos and videos that name their owner.
		{"https://www.facebook.com/somebrand/posts/pfbid0abcDEF", post("v:somebrand")},
		{"https://www.facebook.com/somebrand/posts/123456789/", post("v:somebrand")},
		{"https://www.facebook.com/somebrand/photos/a.111/222/", post("v:somebrand")},
		{"https://www.facebook.com/somebrand/videos/333444555/", post("v:somebrand")},
		{"https://www.facebook.com/somebrand/videos/some-title/333444555/", post("v:somebrand")},
		{"https://www.facebook.com/100064123456789/posts/pfbid0xyz/", post("id:100064123456789")},
		{"https://www.facebook.com/permalink.php?story_fbid=pfbid0xyz&id=100064123456789", post("id:100064123456789")},
		{"https://m.facebook.com/story.php?story_fbid=123&id=100064123456789", post("id:100064123456789")},
		{"https://www.facebook.com/photo/?fbid=123&set=pb.100064123456789.-2207520000", post("id:100064123456789")},
		{"https://www.facebook.com/photo.php?fbid=123&set=a.10150.2077.100064123456789&type=3", post("id:100064123456789")},
		{"https://www.facebook.com/notes/somebrand/some-title/123456/", post("v:somebrand")},

		// No owner in the URL.
		{"https://www.facebook.com/photo/?fbid=123&set=a.456", skip(SkipNoOwner)},
		{"https://www.facebook.com/photo.php?fbid=123", skip(SkipNoOwner)},
		{"https://www.facebook.com/permalink.php?story_fbid=123", skip(SkipNoOwner)},
		{"https://www.facebook.com/reel/123456789/", skip(SkipNoOwner)},
		{"https://www.facebook.com/watch/?v=123456789", skip(SkipNoOwner)},
		{"https://www.facebook.com/watch/live/?v=123456789", skip(SkipNoOwner)},
		{"https://www.facebook.com/videos/123456789/", skip(SkipNoOwner)},
		{"https://www.facebook.com/share/p/AbCdEf/", skip(SkipNoOwner)},
		{"https://www.facebook.com/share/r/AbCdEf/", skip(SkipNoOwner)},
		{"https://www.facebook.com/media/set/?set=a.456", skip(SkipNoOwner)},
		{"https://fb.watch/abcDEF/", skip(SkipNoOwner)},
		{"https://fb.me/abc", skip(SkipNoOwner)},

		// Groups, events, Marketplace.
		{"https://www.facebook.com/groups/123456/", skip(SkipGroup)},
		{"https://www.facebook.com/groups/somegroup/posts/789/", skip(SkipGroup)},
		{"https://www.facebook.com/groups/somegroup/user/100064123456789/", skip(SkipGroup)},
		{"https://www.facebook.com/events/123456/", skip(SkipEvent)},
		{"https://www.facebook.com/marketplace/item/123456/", skip(SkipMarketplace)},
		{"https://www.facebook.com/marketplace/baghdad/", skip(SkipMarketplace)},

		// Facebook's own pages and other hosts.
		{"https://www.facebook.com/", skip(SkipNotAccount)},
		{"https://www.facebook.com/hashtag/baghdad", skip(SkipNotAccount)},
		{"https://www.facebook.com/help/123", skip(SkipNotAccount)},
		{"https://www.facebook.com/login/?next=x", skip(SkipNotAccount)},
		{"https://www.facebook.com/home.php", skip(SkipNotAccount)},
		{"https://www.facebook.com/somethingelse.php?x=1", skip(SkipNotAccount)},
		{"https://www.facebook.com/public/Some-Name", skip(SkipNotAccount)},
		{"https://www.facebook.com/business/help", skip(SkipNotAccount)},
		{"https://www.facebook.com/gaming/somebrand", skip(SkipNotAccount)},
		{"https://l.facebook.com/l.php?u=https%3A%2F%2Fexample.com", skip(SkipNotAccount)},
		{"https://developers.facebook.com/docs", skip(SkipNotAccount)},
		{"https://example.com/facebook.com/somebrand", skip(SkipNotFacebook)},
		{"https://facebook.com.example.com/somebrand", skip(SkipNotFacebook)},
		{"https://notfacebook.com/somebrand", skip(SkipNotFacebook)},
		{"not a url", skip(SkipNotFacebook)},
		{"", skip(SkipNotFacebook)},

		// Malformed names and ids.
		{"https://www.facebook.com/profile.php", skip(SkipInvalid)},
		{"https://www.facebook.com/profile.php?id=abc", skip(SkipInvalid)},
		{"https://www.facebook.com/profile.php?id=0123", skip(SkipInvalid)},
		{"https://www.facebook.com/people/Some-Name/", skip(SkipInvalid)},
		{"https://www.facebook.com/p/no-id-here/", skip(SkipInvalid)},
		{"https://www.facebook.com/some-name-2024", skip(SkipInvalid)},
		{"https://www.facebook.com/%D9%85%D8%B7%D8%B9%D9%85", skip(SkipInvalid)},
		{"https://www.facebook.com/...", skip(SkipInvalid)},
		{"https://www.facebook.com/pages/Some-Name/", skip(SkipNotAccount)},
		{"https://www.facebook.com/pages/category/Restaurant/no-id/", skip(SkipInvalid)},
	}
	for _, c := range cases {
		if got := Classify(c.in); got != c.want {
			t.Errorf("Classify(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestSameRefForUrlVariants(t *testing.T) {
	// One page reached through different hosts, cases and tabs is one ref.
	urls := []string{
		"https://www.facebook.com/SomeBrand",
		"https://m.facebook.com/somebrand/",
		"https://ar-ar.facebook.com/SOMEBRAND/about",
		"https://www.facebook.com/somebrand/posts/123",
	}
	for _, u := range urls {
		if got := Classify(u).Ref; got != "v:somebrand" {
			t.Errorf("Classify(%q).Ref = %q", u, got)
		}
	}
}

func FuzzClassify(f *testing.F) {
	for _, s := range []string{"https://www.facebook.com/profile.php?id=1", "https://www.facebook.com/p/x-1234567/", "https://m.facebook.com/a/b/c"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		r := Classify(s)
		if r.OK() == (r.Skip != "") {
			t.Fatalf("Classify(%q) = %+v: exactly one of Ref and Skip must be set", s, r)
		}
	})
}
