package engine

import (
	"reflect"
	"testing"
)

func TestVerdict(t *testing.T) {
	for _, c := range []struct {
		name         string
		h            Health
		loaded, live bool
		state        string
		issues       []string
	}{
		{"all good", Health{OK: true, MissingLines: []string{"bus 287"}}, true, false, "ok", nil},
		{"nothing loaded", Health{}, false, false, "error", []string{IssueNoTimetable}},
		{"old timetable, download failing", Health{StaticError: "HTTP 500"}, true, false, "error",
			[]string{IssueOldTimetable, IssueTimetable}},
		{"live data failing", Health{OK: true}, true, true, "warning", []string{IssueLiveData}},
		{"unknown trackwork", Health{OK: true, UnknownTrackwork: []string{"bus 10M"}}, true, false, "warning",
			[]string{IssueTrackwork}},
		{"downloads failing", Health{OK: true, Data: DataStatus{WalkError: "x", MapError: "y"}}, true, false, "warning",
			[]string{IssueStreets, IssueBasemap}},
	} {
		state, issues := verdict(c.h, c.loaded, c.live)
		if state != c.state || !reflect.DeepEqual(issues, c.issues) {
			t.Errorf("%s: %s %v, want %s %v", c.name, state, issues, c.state, c.issues)
		}
	}
}
