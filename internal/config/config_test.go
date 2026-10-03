package config

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/benjamincottle/gymrouter/internal/lines"
)

func TestExampleConfigParses(t *testing.T) {
	c, err := Load("../../config.example.toml")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Gyms) != 3 || c.Realtime.ActiveFor.Duration != 10*time.Minute || !c.Server.TrustProxy {
		t.Errorf("unexpected: %+v", c)
	}
	g, ok := c.Gym("lanecove")
	if !ok || !g.LineSet[lines.Key{Mode: lines.Metro, Name: "M1"}] {
		t.Errorf("lanecove lines: %+v", g)
	}
	if !c.AllLines()[lines.Key{Mode: lines.LightRail, Name: "L4"}] {
		t.Error("AllLines missing L4")
	}
}

const minimal = `
[[gym]]
id = "g"
name = "Gym"
lat = -33.8
lon = 151.1
lines = ["bus 288"]
`

func TestDefaultsApply(t *testing.T) {
	c, err := Parse(minimal)
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Listen != ":8080" || c.Risk.SafeS != 180 || c.Realtime.DailyBudget != 40000 {
		t.Errorf("defaults not applied: %+v", c)
	}
}

func TestGymsAreOptional(t *testing.T) {
	c, err := Parse("[server]\nlisten = \":1\"\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(c.Gyms) != 0 || len(c.AllLines()) != 0 {
		t.Errorf("unexpected presets: %+v", c.Gyms)
	}
}

func TestValidation(t *testing.T) {
	for name, tc := range map[string]struct{ text, want string }{
		"unknown key":  {minimal + "\n[server]\nlisten_addr = \":1\"\n", "unknown keys: server.listen_addr"},
		"bad line":     {strings.Replace(minimal, `"bus 288"`, `"tram 1"`, 1), "unknown mode"},
		"no lines":     {strings.Replace(minimal, `["bus 288"]`, `[]`, 1), "lines required"},
		"bad id":       {strings.Replace(minimal, `id = "g"`, `id = "G!"`, 1), "id"},
		"duplicate id": {minimal + minimal, "duplicate id"},
		"budget":       {minimal + "[realtime]\ndaily_budget = 100000\n", "daily_budget"},
		"fast polling": {minimal + "[realtime]\nvehicles_every = \"1s\"\n", "at least 10s"},
		"risk order":   {minimal + "[risk]\nsafe_s = 30\ntight_s = 60\n", "tight_s <= safe_s"},
		"public url":   {minimal + "[server]\npublic_url = \"javascript:alert(1)\"\n", "public_url"},
		"bad access":   {minimal + "[[gym.access]]\nwalk_s = 10\n", "access entries"},
		"bad duration": {minimal + "[realtime]\nactive_for = \"soon\"\n", "duration"},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := Parse(tc.text)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestLoadMissingFile(t *testing.T) {
	if _, err := Load("/nonexistent/config.toml"); !os.IsNotExist(err) {
		t.Errorf("want not-exist error, got %v", err)
	}
}
