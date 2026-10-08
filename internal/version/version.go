// Package version says which source the running binary was built from.
package version

import "runtime/debug"

// Revision is the git commit the binary was built from. Image builds set it with
// -ldflags "-X github.com/benjamincottle/gymrouter/internal/version.Revision=<sha>" (the build context has no .git);
// a go build in a checkout records it by itself.
var Revision string

// Short is the revision as the app shows it: the commit's first seven characters, "+" when built from uncommitted
// changes, or "dev" when nobody recorded it.
func Short() string {
	rev, dirty := Revision, false
	if rev == "" {
		if bi, ok := debug.ReadBuildInfo(); ok {
			for _, s := range bi.Settings {
				switch s.Key {
				case "vcs.revision":
					rev = s.Value
				case "vcs.modified":
					dirty = s.Value == "true"
				}
			}
		}
	}
	if rev == "" {
		return "dev"
	}
	if len(rev) > 7 {
		rev = rev[:7]
	}
	if dirty {
		rev += "+"
	}
	return rev
}
