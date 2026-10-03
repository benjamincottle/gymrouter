package plan

import (
	"fmt"
	"strings"

	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/raptor"
)

// Clock formats seconds after service-day midnight as HH:MM (hours may exceed 23).
func Clock(t int32) string { return fmt.Sprintf("%02d:%02d", t/3600, t/60%60) }

// Delay returns how late (positive) or early a ride leg departs compared with the timetable.
func Delay(d *gtfs.Day, l raptor.Leg) (secs int32, predicted bool) {
	t := &d.Trips[l.Trip]
	if t.Sched == nil || int(l.BoardIdx) >= len(t.Sched) {
		return 0, false
	}
	return l.Dep - t.Sched[l.BoardIdx].Dep, true
}

// Describe renders an option as one line per leg, for logs, the CLI and golden tests.
func Describe(d *gtfs.Day, o Option) string {
	var b strings.Builder
	alt := ""
	if o.Alternative {
		alt = " [alternative]"
	}
	fmt.Fprintf(&b, "leave %s arrive %s (%dm), %d ride(s), %s%s\n", Clock(o.LeaveAt), Clock(o.Arrive),
		o.Duration()/60, o.Rides, o.WorstRisk(), alt)
	transferInto := map[int]Transfer{}
	for _, t := range o.Transfers {
		transferInto[t.ToLeg] = t
	}
	for i, l := range o.Legs {
		switch l.Kind {
		case raptor.Walk:
			fmt.Fprintf(&b, "  walk %dm\n", (l.Arr-l.Dep+30)/60)
		case raptor.Ride:
			r := d.Routes[l.Route]
			rt := ""
			if secs, ok := Delay(d, l); ok {
				switch m := secs / 60; {
				case m == 0:
					rt = " (on time)"
				case m > 0:
					rt = fmt.Sprintf(" (%dm late)", m)
				default:
					rt = fmt.Sprintf(" (%dm early)", -m)
				}
			}
			switch d.Trips[l.Trip].Status {
			case gtfs.Added:
				rt = " (realtime-only)"
			}
			risk := ""
			if t, ok := transferInto[i]; ok {
				risk = fmt.Sprintf(" — %s connection, %ds spare", t.Risk, t.Slack)
				if t.FallbackTrip >= 0 {
					risk += ", next " + Clock(t.FallbackDep)
				}
			}
			fmt.Fprintf(&b, "  %s %s–%s %s → %s%s%s\n", lines.Of(r.Type, r.ShortName), Clock(l.Dep), Clock(l.Arr),
				stationName(d, l.From), stationName(d, l.To), rt, risk)
		}
	}
	return b.String()
}

func stationName(d *gtfs.Day, s int32) string {
	if s < 0 {
		return "-"
	}
	st := d.Stops[s]
	if st.Parent != "" {
		if pi, ok := d.StopIndex[st.Parent]; ok {
			return d.Stops[pi].Name
		}
	}
	return st.Name
}
