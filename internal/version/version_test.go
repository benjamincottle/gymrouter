package version

import "testing"

func TestShort(t *testing.T) {
	defer func(r string) { Revision = r }(Revision)
	Revision = "87692b5f0c1d2e3f4a5b6c7d8e9f0a1b2c3d4e5f"
	if got := Short(); got != "87692b5" {
		t.Errorf("stamped: %q", got)
	}
	Revision = ""
	if got := Short(); got == "" { // a test binary has no VCS stamp
		t.Error("unstamped builds still say something")
	}
}
