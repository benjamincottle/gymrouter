package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestDownloadsOnlyOverHTTPS(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "f")
	if err := download(context.Background(), nil, "http://example.com/x.pbf", dest, 1<<20); err == nil {
		t.Error("downloaded over plain http")
	}
	// A server that redirects to plain http: the redirect isn't followed.
	plain := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("data")) }))
	defer plain.Close()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, plain.URL+"/x.pbf", http.StatusFound)
	}))
	defer srv.Close()
	c := srv.Client()
	c.CheckRedirect = httpsRedirects
	resp, err := c.Get(srv.URL + "/x.pbf")
	if err == nil {
		resp.Body.Close()
		t.Error("followed a redirect to plain http")
	}
}
