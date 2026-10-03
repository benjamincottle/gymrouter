package config

import (
	"os"
	"strings"
	"testing"
	"time"
)

func TestExampleConfigParses(t *testing.T) {
	c, err := Load("../../config.example.toml")
	if err != nil {
		t.Fatal(err)
	}
	if c.Realtime.ActiveFor.Duration != 10*time.Minute || !c.Server.TrustProxy || c.Server.StaticRefreshH != 5 {
		t.Errorf("unexpected: %+v", c)
	}
}

func TestDefaultsApply(t *testing.T) {
	c, err := Parse("")
	if err != nil {
		t.Fatal(err)
	}
	if c.Server.Listen != ":8080" || c.Risk.SafeS != 180 || c.Routing.MaxRides != 4 || c.Realtime.DailyBudget != 40000 {
		t.Errorf("defaults not applied: %+v", c)
	}
}

func TestValidation(t *testing.T) {
	for name, tc := range map[string]struct{ text, want string }{
		"unknown key":     {"[server]\nlisten_addr = \":1\"\n", "unknown keys: server.listen_addr"},
		"routing in file": {"[routing]\nmax_rides = 2\n", "unknown keys: routing"},
		"risk in file":    {"[risk]\nsafe_s = 100\n", "unknown keys: risk"},
		"gyms in file":    {"[[gym]]\nid = \"g\"\n", "unknown keys: gym"},
		"refresh hour":    {"[server]\nstatic_refresh_hour = 30\n", "static_refresh_hour"},
		"budget":          {"[realtime]\ndaily_budget = 100000\n", "daily_budget"},
		"fast polling":    {"[realtime]\nvehicles_every = \"1s\"\n", "at least 10s"},
		"public url":      {"[server]\npublic_url = \"javascript:alert(1)\"\n", "public_url"},
		"bad duration":    {"[realtime]\nactive_for = \"soon\"\n", "duration"},
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
