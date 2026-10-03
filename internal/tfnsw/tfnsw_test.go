package tfnsw

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/benjamincottle/gymrouter/internal/lines"
)

func TestFeedsFor(t *testing.T) {
	got := FeedsFor(lines.MustSet("bus 288", "metro M1", "bus 52"))
	if len(got) != 2 || got[0].Name != "metro" || got[1].Name != "buses" {
		t.Errorf("FeedsFor: %+v", got)
	}
	if len(FeedsFor(lines.Set{})) != 0 {
		t.Error("no lines should need no feeds")
	}
}

func TestClientSendsKeyAndMapsErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "apikey secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/ok":
			_, _ = w.Write([]byte("hello"))
		case "/limited":
			w.Header().Set("X-Error-Detail", "Account Over Rate Limit")
			w.WriteHeader(http.StatusForbidden)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	c := NewClient("secret")
	c.Base = srv.URL
	ctx := context.Background()
	if b, err := c.Get(ctx, "/ok", 100); err != nil || string(b) != "hello" {
		t.Errorf("ok: %q %v", b, err)
	}
	if _, err := c.Get(ctx, "/ok", 3); err == nil {
		t.Error("want size-limit error")
	}
	if _, err := c.Get(ctx, "/limited", 100); !errors.Is(err, ErrLimited) {
		t.Errorf("want ErrLimited, got %v", err)
	}
	if _, err := c.Get(ctx, "/missing", 100); err == nil {
		t.Error("want error for 404")
	}
}

func TestDownloadConditionalAndAtomic(t *testing.T) {
	var zipBytes bytes.Buffer
	zw := zip.NewWriter(&zipBytes)
	w, _ := zw.Create("stops.txt")
	_, _ = w.Write([]byte("stop_id\n"))
	_ = zw.Close()
	served := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/broken" {
			_, _ = w.Write([]byte("<html>not a zip</html>"))
			return
		}
		if r.Header.Get("If-None-Match") == `"v1"` {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		served++
		w.Header().Set("ETag", `"v1"`)
		_, _ = w.Write(zipBytes.Bytes())
	}))
	defer srv.Close()
	c := NewClient("k")
	c.Base = srv.URL
	dest := filepath.Join(t.TempDir(), "feed.zip")
	ctx := context.Background()

	if changed, err := c.Download(ctx, "/feed", dest, 1<<20); err != nil || !changed {
		t.Fatalf("first download: %v %v", changed, err)
	}
	if changed, err := c.Download(ctx, "/feed", dest, 1<<20); err != nil || changed || served != 1 {
		t.Fatalf("second download should be not-modified: %v %v served=%d", changed, err, served)
	}
	// A bad response must not replace the good file.
	if _, err := c.Download(ctx, "/broken", dest, 1<<20); err == nil {
		t.Fatal("want error for non-zip body")
	}
	if b, _ := os.ReadFile(dest); !bytes.Equal(b, zipBytes.Bytes()) {
		t.Error("good file was replaced")
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(dest), ".download-*")); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}
