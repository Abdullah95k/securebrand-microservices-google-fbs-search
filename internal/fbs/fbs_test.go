package fbs

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abdullah95k/securebrand-microservices-google-fbs-search/internal/secret"
)

func TestKeywordsPaginates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer k" || r.URL.Query().Get("enabled") != "true" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Query().Get("after") {
		case "":
			fmt.Fprint(w, `{"keywords":[{"id":1,"term":"Baghdad","language":"en","enabled":true},{"id":2,"term":"x","language":"en","enabled":false}],"next_after":2}`)
		case "2":
			fmt.Fprint(w, `{"keywords":[{"id":3,"term":"بغداد","language":"ar","enabled":true}]}`)
		default:
			t.Errorf("after = %s", r.URL.Query().Get("after"))
		}
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL + "/", Key: secret.New("k"), HTTP: srv.Client()}
	kws, err := c.Keywords(context.Background())
	if err != nil || len(kws) != 2 || kws[1].Term != "بغداد" {
		t.Fatalf("keywords = %+v, %v", kws, err)
	}

	c.Key = secret.New("wrong")
	if _, err := c.Keywords(context.Background()); err == nil || !strings.Contains(err.Error(), "data:read") || strings.Contains(err.Error(), "wrong") {
		t.Fatalf("bad key: %v", err)
	}
}

func TestKnown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.EscapedPath() {
		case "/v1/pages/v:known":
			fmt.Fprint(w, `{"page_ref":"v:known"}`)
		case "/v1/pages/id:123":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	}))
	defer srv.Close()
	c := &Client{Base: srv.URL, Key: secret.New("k"), HTTP: srv.Client()}
	if ok, err := c.Known(context.Background(), "v:known"); !ok || err != nil {
		t.Fatalf("known: %v %v", ok, err)
	}
	if ok, err := c.Known(context.Background(), "id:123"); ok || err != nil {
		t.Fatalf("unknown: %v %v", ok, err)
	}
	if _, err := c.Known(context.Background(), "v:boom"); err == nil {
		t.Fatal("server error must fail")
	}
}
