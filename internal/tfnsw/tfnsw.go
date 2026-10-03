// Package tfnsw fetches feeds from the Transport for NSW Open Data API.
package tfnsw

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

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
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusForbidden:
		return nil, fmt.Errorf("%w: %s", ErrLimited, resp.Header.Get("X-Error-Detail"))
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("tfnsw %s: HTTP %d", path, resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBytes {
		return nil, fmt.Errorf("tfnsw %s: response larger than %d bytes", path, maxBytes)
	}
	return b, nil
}
