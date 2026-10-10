package gyms

import (
	"strings"
	"testing"

	"github.com/benjamincottle/gymrouter/internal/lines"
)

func TestBuiltInGymsAreValid(t *testing.T) {
	g := Known()
	have := map[string]bool{}
	for _, x := range g {
		have[x.ID] = true
	}
	for _, id := range []string{
		"lanecove", "chatswood", "rydalmere", "waterloo", "alexandria", "climbfit-macquarie", "climbfit-stleonards",
		"nomad-annandale", "nomad-gladesville", "blochaus-marrickville", "blochaus-leichhardt", "betaone-granville",
		"1up-chullora", "sandbox-silverwater", "sicg-stpeters", "skywood-brookvale",
	} {
		if !have[id] {
			t.Errorf("missing gym %q", id)
		}
	}
	if !AllLines()[lines.Key{Mode: lines.LightRail, Name: "L4"}] || !AllLines()[lines.Key{Mode: lines.Metro, Name: "M1"}] {
		t.Error("AllLines is missing expected lines")
	}
}

func TestValidation(t *testing.T) {
	const ok = "[[gym]]\nid = \"g\"\nname = \"Gym\"\nlat = -33.8\nlon = 151.1\nlines = [\"bus 288\"]\n"
	if _, err := Parse(ok); err != nil {
		t.Fatal(err)
	}
	for name, tc := range map[string]struct{ text, want string }{
		"bad id":      {strings.Replace(ok, `id = "g"`, `id = "G!"`, 1), "id"},
		"duplicate":   {ok + ok, "unique"},
		"no lines":    {strings.Replace(ok, `["bus 288"]`, `[]`, 1), "lines required"},
		"bad line":    {strings.Replace(ok, `"bus 288"`, `"tram 1"`, 1), "unknown mode"},
		"no position": {strings.Replace(strings.Replace(ok, "lat = -33.8", "lat = 0", 1), "lon = 151.1", "lon = 0", 1), "lat/lon"},
		"bad access":  {ok + "[[gym.access]]\nstop = \"1\"\nwalk_s = 10\n", "access entries"},
		"unknown key": {ok + "colour = 1\n", "unknown keys"},
		"bad brand":   {strings.Replace(ok, `name = "Gym"`, "name = \"Gym\"\nbrand = \"acme\"", 1), "unknown brand"},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse(tc.text); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}
