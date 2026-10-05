// Package tfnsw fetches feeds from the Transport for NSW Open Data API.
package tfnsw

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/benjamincottle/gymrouter/internal/lines"
)

// BaseURL is the TfNSW Open Data API gateway.
const BaseURL = "https://api.transport.nsw.gov.au"

// Static feed paths.
const (
	CompleteGTFS = "/v1/publictransport/timetables/complete/gtfs"
	TrainsGTFS   = "/v1/gtfs/schedule/sydneytrains"
)

// Feed is a pair of GTFS-realtime endpoints for one mode.
type Feed struct {
	Name        string
	TripUpdates string
	VehiclePos  string
	Modes       []lines.Mode
}

// Feeds are the realtime feeds for the modes the app can route on (verified in the spike).
var Feeds = []Feed{
	{"sydneytrains", "/v2/gtfs/realtime/sydneytrains", "/v2/gtfs/vehiclepos/sydneytrains", []lines.Mode{lines.Train, lines.RegionalTrain}},
	{"metro", "/v2/gtfs/realtime/metro", "/v2/gtfs/vehiclepos/metro", []lines.Mode{lines.Metro}},
	{"buses", "/v1/gtfs/realtime/buses", "/v1/gtfs/vehiclepos/buses", []lines.Mode{lines.Bus, lines.ReplacementBus}},
	{"lightrail-parramatta", "/v1/gtfs/realtime/lightrail/parramatta", "/v1/gtfs/vehiclepos/lightrail/parramatta", []lines.Mode{lines.LightRail}},
	{"ferries", "/v1/gtfs/realtime/ferries/sydneyferries", "/v1/gtfs/vehiclepos/ferries/sydneyferries", []lines.Mode{lines.Ferry}},
}

// FeedsFor returns the realtime feeds needed for a set of lines.
func FeedsFor(set lines.Set) []Feed {
	modes := map[lines.Mode]bool{}
	for k := range set {
		modes[k.Mode] = true
	}
	var out []Feed
	for _, f := range Feeds {
		for _, m := range f.Modes {
			if modes[m] {
				out = append(out, f)
				break
			}
		}
	}
	return out
}

// ErrLimited is returned when TfNSW rejects a request for exceeding the rate limit or daily quota.
var ErrLimited = errors.New("tfnsw: over rate limit or quota")

// Client calls the API with a key. The key is only ever sent in the Authorization header.
type Client struct {
	Key  string
	HTTP *http.Client
	Base string
}

// NewClient returns a client with sensible timeouts.
func NewClient(key string) *Client {
	return &Client{Key: key, HTTP: &http.Client{Timeout: 60 * time.Second}, Base: BaseURL}
}

// Get fetches path and returns the body (at most maxBytes).
func (c *Client) Get(ctx context.Context, path string, maxBytes int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "apikey "+c.Key)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tfnsw %s: request failed", pathOnly(path)) // the URL may hold personal data
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: %s", ErrLimited, errorDetail(resp))
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("tfnsw %s: HTTP %d", pathOnly(path), resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("tfnsw %s: response larger than %d bytes", pathOnly(path), maxBytes)
	}
	return b, nil
}

// Download fetches a zip at path into dest, replacing it atomically once the download is complete
// and opens as a zip. It sends the previous ETag (kept in dest+".etag") and returns changed=false when
// the server reports the file is unchanged.
func (c *Client) Download(ctx context.Context, path, dest string, maxBytes int64) (changed bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+path, nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "apikey "+c.Key)
	etagFile := dest + ".etag"
	if _, statErr := os.Stat(dest); statErr == nil {
		if etag, err := os.ReadFile(etagFile); err == nil && len(etag) > 0 {
			req.Header.Set("If-None-Match", strings.TrimSpace(string(etag)))
		}
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotModified:
		now := time.Now()
		_ = os.Chtimes(dest, now, now) // mtime records when the copy was last confirmed current
		return false, nil
	case resp.StatusCode == http.StatusForbidden:
		return false, fmt.Errorf("%w: %s", ErrLimited, errorDetail(resp))
	case resp.StatusCode != http.StatusOK:
		return false, fmt.Errorf("tfnsw %s: HTTP %d", path, resp.StatusCode)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".download-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name()) // no-op after a successful rename
	n, err := io.Copy(tmp, io.LimitReader(resp.Body, maxBytes+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return false, err
	}
	if n > maxBytes {
		return false, fmt.Errorf("tfnsw %s: larger than %d bytes", path, maxBytes)
	}
	zr, err := zip.OpenReader(tmp.Name())
	if err != nil {
		return false, fmt.Errorf("tfnsw %s: not a valid zip: %w", path, err)
	}
	zr.Close()
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return false, err
	}
	if etag := resp.Header.Get("ETag"); etag != "" {
		_ = os.WriteFile(etagFile, []byte(etag), 0o644)
	} else {
		_ = os.Remove(etagFile)
	}
	return true, nil
}

// errorDetail is TfNSW's reason for a refusal, trimmed: it ends up in /api/status, so keep it short and printable.
func errorDetail(resp *http.Response) string {
	d := strings.Map(func(r rune) rune {
		if unicode.IsPrint(r) {
			return r
		}
		return -1
	}, resp.Header.Get("X-Error-Detail"))
	if r := []rune(d); len(r) > 120 {
		d = string(r[:120]) + "…"
	}
	return d
}

func pathOnly(p string) string {
	path, _, _ := strings.Cut(p, "?")
	return path
}

// GeocodePath builds a Trip Planner stop_finder query for free text (addresses, places, stops).
func GeocodePath(q string) string {
	v := url.Values{}
	v.Set("outputFormat", "rapidJSON")
	v.Set("type_sf", "any")
	v.Set("name_sf", q)
	v.Set("coordOutputFormat", "EPSG:4326")
	v.Set("TfNSWSF", "true")
	v.Set("version", "10.2.1.42")
	return "/v1/tp/stop_finder?" + v.Encode()
}

// Place is a geocoding result.
type Place struct {
	Name string  `json:"name"`
	Type string  `json:"type"`
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
}

// ParseGeocode reads a stop_finder response.
func ParseGeocode(b []byte, max int) ([]Place, error) {
	var r struct {
		Locations []struct {
			Name  string    `json:"name"`
			Type  string    `json:"type"`
			Coord []float64 `json:"coord"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return nil, fmt.Errorf("stop_finder: %w", err)
	}
	out := []Place{}
	for _, l := range r.Locations {
		if len(l.Coord) != 2 || len(out) >= max {
			continue
		}
		out = append(out, Place{Name: l.Name, Type: l.Type, Lat: l.Coord[0], Lon: l.Coord[1]})
	}
	return out, nil
}
