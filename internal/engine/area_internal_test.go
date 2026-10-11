package engine

import (
	"testing"

	"github.com/benjamincottle/gymrouter/internal/gyms"
)

// A built-in gym has to be inside the area: outside it the map is blank and every walk is a straight-line estimate.
func TestBuiltInGymsAreInsideTheArea(t *testing.T) {
	for _, g := range gyms.Known() {
		if !area.Contains(g.Lat, g.Lon) {
			t.Errorf("%s (%v, %v) is outside the area %s", g.Name, g.Lat, g.Lon, area)
		}
	}
}
