package api

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"sort"
	"time"

	"github.com/benjamincottle/gymrouter/internal/config"
	"github.com/benjamincottle/gymrouter/internal/engine"
	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/plan"
	"github.com/benjamincottle/gymrouter/internal/raptor"
)

// Request limits.
const (
	maxAccess    = 20
	maxTransfers = 100
	maxWalkS     = 3600
	maxRadiusM   = 2000
)

type accessReq struct {
	Stop  string `json:"stop"`
	WalkS int32  `json:"walk_s"`
}

// placeReq is either a gym or a private place (coordinates and optional curated stops).
type placeReq struct {
	Gym    string      `json:"gym,omitempty"`
	Lat    *float64    `json:"lat,omitempty"`
	Lon    *float64    `json:"lon,omitempty"`
	Access []accessReq `json:"access,omitempty"`
}

type riskReq struct {
	SafeS  int32 `json:"safe_s"`
	TightS int32 `json:"tight_s"`
}

type transferReq struct {
	From string `json:"from"`
	To   string `json:"to"`
	Secs int32  `json:"secs"`
}

type prefsReq struct {
	WalkSpeedMps float64       `json:"walk_speed_mps,omitempty"`
	MinChangeS   *int32        `json:"min_change_s,omitempty"`
	MaxWalkM     float64       `json:"max_walk_m,omitempty"`
	LeaveBufferS int32         `json:"leave_buffer_s,omitempty"`
	Risk         *riskReq      `json:"risk,omitempty"`
	Transfers    []transferReq `json:"transfers,omitempty"`
}

type planReq struct {
	From      placeReq   `json:"from"`
	To        placeReq   `json:"to"`
	Time      *time.Time `json:"time,omitempty"` // earliest time to leave; default now
	WindowMin int        `json:"window_min,omitempty"`
	Prefs     prefsReq   `json:"prefs"`
}

type stopResp struct {
	ID      string  `json:"id"`
	Name    string  `json:"name"`
	Station string  `json:"station,omitempty"`
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
}

type lineResp struct {
	Mode      string `json:"mode"`
	Name      string `json:"name"`
	Color     string `json:"color,omitempty"`
	TextColor string `json:"text_color,omitempty"`
}

type legResp struct {
	Kind     string     `json:"kind"` // walk | ride
	From     *stopResp  `json:"from,omitempty"`
	To       *stopResp  `json:"to,omitempty"`
	Dep      time.Time  `json:"dep"`
	Arr      time.Time  `json:"arr"`
	Line     *lineResp  `json:"line,omitempty"`
	TripID   string     `json:"trip_id,omitempty"`
	Headsign string     `json:"headsign,omitempty"`
	Status   string     `json:"status,omitempty"` // scheduled | predicted | added
	DelayS   *int32     `json:"delay_s,omitempty"`
	SchedDep *time.Time `json:"sched_dep,omitempty"`
}

type transferResp struct {
	FromLeg     int        `json:"from_leg"`
	ToLeg       int        `json:"to_leg"`
	WalkS       int32      `json:"walk_s"`
	SlackS      int32      `json:"slack_s"`
	Risk        string     `json:"risk"`
	FallbackDep *time.Time `json:"fallback_dep,omitempty"`
}

type optionResp struct {
	LeaveAt     time.Time      `json:"leave_at"`
	Arrive      time.Time      `json:"arrive"`
	DurationS   int32          `json:"duration_s"`
	Rides       int            `json:"rides"`
	Risk        string         `json:"risk"`
	Alternative bool           `json:"alternative,omitempty"`
	Lines       []string       `json:"lines"`
	Legs        []legResp      `json:"legs"`
	Transfers   []transferResp `json:"transfers"`
}

type planResp struct {
	ServiceDate string       `json:"service_date"`
	Realtime    bool         `json:"realtime"`
	RealtimeAt  *time.Time   `json:"realtime_at,omitempty"`
	Options     []optionResp `json:"options"`
}

type badRequest struct{ msg string }

func (e badRequest) Error() string { return e.msg }

func badf(format string, a ...any) error { return badRequest{fmt.Sprintf(format, a...)} }

func (s *Server) plan(w http.ResponseWriter, r *http.Request) {
	var req planReq
	if err := decode(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp, err := s.runPlan(req)
	var br badRequest
	switch {
	case errors.As(err, &br):
		writeError(w, http.StatusBadRequest, br.msg)
	case err != nil:
		s.log.Error("plan failed", "err", err)
		writeError(w, http.StatusInternalServerError, "planning failed")
	default:
		writeJSON(w, http.StatusOK, resp)
	}
}

func (s *Server) runPlan(req planReq) (*planResp, error) {
	cfg := s.eng.Config()
	fromGym, err := gymOf(cfg, req.From)
	if err != nil {
		return nil, err
	}
	toGym, err := gymOf(cfg, req.To)
	if err != nil {
		return nil, err
	}
	if fromGym == nil && toGym == nil {
		return nil, badf("one end of the trip must be a gym")
	}
	set := lines.Set{}
	for _, g := range []*config.Gym{fromGym, toGym} {
		if g != nil {
			set = lines.Union(set, g.LineSet)
		}
	}
	if err := validatePrefs(req.Prefs); err != nil {
		return nil, err
	}
	window := int32(req.WindowMin) * 60
	if window < 0 || window > cfg.Routing.MaxWindowS {
		return nil, badf("window_min must be 0..%d", cfg.Routing.MaxWindowS/60)
	}

	leave := s.eng.Now()
	if req.Time != nil {
		leave = *req.Time
		if d := leave.Sub(s.eng.Now()); d < -6*time.Hour || d > 8*24*time.Hour {
			return nil, badf("time must be within the next week")
		}
	}
	snap, err := s.eng.SnapshotFor(leave)
	if err != nil {
		return nil, err
	}
	opts := s.eng.RoutingOptions()
	if req.Prefs.WalkSpeedMps > 0 {
		opts.WalkSpeedMps = req.Prefs.WalkSpeedMps
	}
	maxWalk := cfg.Routing.MaxWalkM
	if req.Prefs.MaxWalkM > 0 {
		maxWalk = req.Prefs.MaxWalkM
	}
	access, err := s.access(snap, req.From, fromGym, maxWalk, opts)
	if err != nil {
		return nil, err
	}
	egress, err := s.access(snap, req.To, toGym, maxWalk, opts)
	if err != nil {
		return nil, err
	}
	th := plan.Thresholds{Safe: cfg.Risk.SafeS, Tight: cfg.Risk.TightS}
	if rk := req.Prefs.Risk; rk != nil {
		th = plan.Thresholds{Safe: rk.SafeS, Tight: rk.TightS}
	}
	minChange := cfg.Routing.MinChangeS
	if req.Prefs.MinChangeS != nil {
		minChange = *req.Prefs.MinChangeS
	}
	var overrides []plan.TransferOverride
	for _, t := range req.Prefs.Transfers {
		overrides = append(overrides, plan.TransferOverride{From: t.From, To: t.To, Secs: t.Secs})
	}
	day := snap.Day
	allowed := make([]bool, len(day.Routes))
	for i, rt := range day.Routes {
		allowed[i] = set.Has(rt.Type, rt.ShortName)
	}
	options := plan.Plan(snap.Net, plan.Request{
		Access: access, Egress: egress, Depart: snap.Secs(leave), Window: window,
		MinChange: minChange, Overrides: overrides, Thresholds: th,
		MaxRides: cfg.Routing.MaxRides, AltSlack: cfg.Routing.AlternativesS,
		Allow: func(r int32) bool { return allowed[r] },
	})

	resp := &planResp{ServiceDate: snap.Date.Format("2006-01-02"), Realtime: snap.Realtime, Options: []optionResp{}}
	if snap.Realtime {
		at := snap.RealtimeAt
		resp.RealtimeAt = &at
	}
	for _, o := range options {
		resp.Options = append(resp.Options, optionJSON(snap, o, req.Prefs.LeaveBufferS))
	}
	return resp, nil
}

func gymOf(cfg *config.Config, p placeReq) (*config.Gym, error) {
	if p.Gym == "" {
		if p.Lat == nil || p.Lon == nil {
			return nil, badf("each place needs a gym id or lat/lon")
		}
		if math.IsNaN(*p.Lat) || *p.Lat < -90 || *p.Lat > 90 || math.IsNaN(*p.Lon) || *p.Lon < -180 || *p.Lon > 180 {
			return nil, badf("lat/lon out of range")
		}
		if len(p.Access) > maxAccess {
			return nil, badf("at most %d access stops", maxAccess)
		}
		for _, a := range p.Access {
			if a.WalkS < 0 || a.WalkS > maxWalkS {
				return nil, badf("walk_s must be 0..%d", maxWalkS)
			}
		}
		return nil, nil
	}
	g, ok := cfg.Gym(p.Gym)
	if !ok {
		return nil, badf("unknown gym")
	}
	return g, nil
}

func validatePrefs(p prefsReq) error {
	switch {
	case p.WalkSpeedMps < 0 || p.WalkSpeedMps > 3:
		return badf("walk_speed_mps must be 0..3")
	case p.MinChangeS != nil && (*p.MinChangeS < 0 || *p.MinChangeS > 900):
		return badf("min_change_s must be 0..900")
	case p.MaxWalkM < 0 || p.MaxWalkM > maxRadiusM:
		return badf("max_walk_m must be 0..%d", maxRadiusM)
	case p.LeaveBufferS < 0 || p.LeaveBufferS > 1800:
		return badf("leave_buffer_s must be 0..1800")
	case p.Risk != nil && (p.Risk.TightS < 0 || p.Risk.SafeS < p.Risk.TightS || p.Risk.SafeS > 1800):
		return badf("risk: need 0 <= tight_s <= safe_s <= 1800")
	case len(p.Transfers) > maxTransfers:
		return badf("at most %d transfers", maxTransfers)
	}
	for _, t := range p.Transfers {
		if t.From == "" || t.To == "" || t.Secs < 0 || t.Secs > maxWalkS {
			return badf("transfers need from, to and 0 <= secs <= %d", maxWalkS)
		}
	}
	return nil
}

// access resolves a place to stops with walking times: curated stops if given, otherwise nearby stops.
func (s *Server) access(snap *engine.Snapshot, p placeReq, g *config.Gym, maxWalk float64, o raptor.Options) ([]raptor.Access, error) {
	pos := geo.Point{}
	var curated []accessReq
	if g != nil {
		pos = geo.Point{Lat: g.Lat, Lon: g.Lon}
		for _, a := range g.Access {
			curated = append(curated, accessReq{Stop: a.Stop, WalkS: a.WalkS})
		}
	} else {
		pos = geo.Point{Lat: *p.Lat, Lon: *p.Lon}
		curated = p.Access
	}
	if len(curated) > 0 {
		var out []raptor.Access
		for _, a := range curated {
			if si, ok := snap.Day.StopIndex[a.Stop]; ok {
				out = append(out, raptor.Access{Stop: si, Secs: a.WalkS})
			}
		}
		if len(out) == 0 {
			return nil, badf("none of the access stops exist in the timetable")
		}
		return out, nil
	}
	out := snap.Net.StopsNear(pos, maxWalk, o)
	if len(out) == 0 {
		return nil, badf("no stops within %.0f m", maxWalk)
	}
	return out, nil
}

func stopJSON(d *gtfs.Day, s int32) *stopResp {
	if s < 0 {
		return nil
	}
	st := d.Stops[s]
	out := &stopResp{ID: st.ID, Name: st.Name, Lat: st.Pos.Lat, Lon: st.Pos.Lon}
	if st.Parent != "" {
		if pi, ok := d.StopIndex[st.Parent]; ok {
			out.Station = d.Stops[pi].Name
		}
	}
	return out
}

func optionJSON(snap *engine.Snapshot, o plan.Option, buffer int32) optionResp {
	d := snap.Day
	leave := o.LeaveAt - buffer
	out := optionResp{
		LeaveAt: snap.Clock(leave), Arrive: snap.Clock(o.Arrive), DurationS: o.Arrive - leave,
		Rides: o.Rides, Risk: string(o.WorstRisk()), Alternative: o.Alternative,
		Legs: []legResp{}, Transfers: []transferResp{},
	}
	for _, k := range o.Lines {
		out.Lines = append(out.Lines, k.String())
	}
	for i, l := range o.Legs {
		dep := l.Dep
		if i == 0 {
			dep = leave
		}
		lr := legResp{From: stopJSON(d, l.From), To: stopJSON(d, l.To), Dep: snap.Clock(dep), Arr: snap.Clock(l.Arr)}
		if l.Kind == raptor.Walk {
			lr.Kind = "walk"
		} else {
			lr.Kind = "ride"
			r := d.Routes[l.Route]
			k := lines.Of(r.Type, r.ShortName)
			lr.Line = &lineResp{Mode: string(k.Mode), Name: k.Name, Color: r.Color, TextColor: r.TextColor}
			t := &d.Trips[l.Trip]
			lr.TripID, lr.Headsign = t.ID, t.Headsign
			switch t.Status {
			case gtfs.Predicted:
				lr.Status = "predicted"
			case gtfs.Added:
				lr.Status = "added"
			default:
				lr.Status = "scheduled"
			}
			if delay, ok := plan.Delay(d, l); ok {
				lr.DelayS = &delay
				sd := snap.Clock(l.Dep - delay)
				lr.SchedDep = &sd
			}
		}
		out.Legs = append(out.Legs, lr)
	}
	for _, t := range o.Transfers {
		tr := transferResp{FromLeg: t.FromLeg, ToLeg: t.ToLeg, WalkS: t.Walk, SlackS: t.Slack, Risk: string(t.Risk)}
		if t.FallbackTrip >= 0 {
			fd := snap.Clock(t.FallbackDep)
			tr.FallbackDep = &fd
		}
		out.Transfers = append(out.Transfers, tr)
	}
	return out
}

type nearReq struct {
	Lat     float64 `json:"lat"`
	Lon     float64 `json:"lon"`
	RadiusM float64 `json:"radius_m,omitempty"`
}

type nearStopResp struct {
	stopResp
	WalkS int32    `json:"walk_s"`
	Lines []string `json:"lines"`
}

// stopsNear lists stops served by configured lines near a point, for setting up a home.
func (s *Server) stopsNear(w http.ResponseWriter, r *http.Request) {
	var req nearReq
	if err := decode(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if req.Lat < -90 || req.Lat > 90 || req.Lon < -180 || req.Lon > 180 || req.RadiusM < 0 || req.RadiusM > maxRadiusM {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("need valid lat/lon and radius_m 0..%d", maxRadiusM))
		return
	}
	if req.RadiusM == 0 {
		req.RadiusM = s.eng.Config().Routing.MaxWalkM
	}
	snap, err := s.eng.SnapshotFor(s.eng.Now())
	if err != nil {
		s.log.Error("stops near failed", "err", err)
		writeError(w, http.StatusInternalServerError, "timetable unavailable")
		return
	}
	o := s.eng.RoutingOptions()
	near := snap.Net.StopsNear(geo.Point{Lat: req.Lat, Lon: req.Lon}, req.RadiusM, o)
	sort.Slice(near, func(a, b int) bool { return near[a].Secs < near[b].Secs })
	out := []nearStopResp{}
	for _, a := range near {
		ls := lines.Set{}
		for _, ri := range snap.Net.RoutesAt(a.Stop) {
			rt := snap.Day.Routes[ri]
			ls[lines.Of(rt.Type, rt.ShortName)] = true
		}
		out = append(out, nearStopResp{stopResp: *stopJSON(snap.Day, a.Stop), WalkS: a.Secs, Lines: ls.Strings()})
	}
	writeJSON(w, http.StatusOK, map[string]any{"stops": out})
}
