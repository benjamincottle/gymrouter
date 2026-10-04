package api

import (
	"errors"
	"fmt"
	"math"
	"net/http"
	"time"

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
	maxWalks     = 40
	maxTransfers = 100
	maxWalkS     = 3600
	maxRadiusM   = 2000
)

type accessReq struct {
	Stop  string `json:"stop"`
	WalkS int32  `json:"walk_s"`
}

// placeReq is a place (coordinates and optional curated stops) or, as the origin only, the vehicle the
// traveller is on.
type placeReq struct {
	Lat    *float64    `json:"lat,omitempty"`
	Lon    *float64    `json:"lon,omitempty"`
	Access []accessReq `json:"access,omitempty"`
	// Walks are walks the traveller has timed between this place and particular stops (stop or station IDs). They
	// beat any other time for that stop, and make it usable even if it isn't otherwise one of the place's stops.
	Walks  []accessReq `json:"walks,omitempty"`
	OnTrip *onTripReq  `json:"on_trip,omitempty"`
}

type onTripReq struct {
	TripID   string `json:"trip_id"`
	FromStop string `json:"from_stop"` // where the traveller boarded
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
	Lines     []string   `json:"lines"`          // the lines to route on, e.g. "bus 288" (a gym's set)
	Time      *time.Time `json:"time,omitempty"` // earliest time to leave (latest arrival with arrive_by); default now
	ArriveBy  bool       `json:"arrive_by,omitempty"`
	WindowMin int        `json:"window_min,omitempty"`
	Prefs     prefsReq   `json:"prefs"`
}

type stopResp struct {
	ID        string  `json:"id"`
	Name      string  `json:"name"`
	Station   string  `json:"station,omitempty"`
	StationID string  `json:"station_id,omitempty"`
	Lat       float64 `json:"lat"`
	Lon       float64 `json:"lon"`
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
	Stops    int32      `json:"stops,omitempty"` // ride legs: stops travelled, counting the one you get off at
	// Path is the walk along the streets, [lon, lat] pairs, for the first and last legs when known.
	Path [][2]float64 `json:"path,omitempty"`
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
	ServiceDate string     `json:"service_date"`
	Realtime    bool       `json:"realtime"`
	RealtimeAt  *time.Time `json:"realtime_at,omitempty"`
	// Walking says how walks to and from stops were timed: "streets" (along real streets and paths) or
	// "estimate" (straight line, while the street network is still being prepared). Curated walks apply either way.
	Walking string       `json:"walking"`
	Options []optionResp `json:"options"`
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
	if err := checkPlace(req.From); err != nil {
		return nil, err
	}
	if err := checkPlace(req.To); err != nil {
		return nil, err
	}
	if req.To.OnTrip != nil {
		return nil, badf("on_trip is only valid for the origin")
	}
	onboard := req.From.OnTrip != nil
	if onboard && (req.ArriveBy || req.Time != nil) {
		return nil, badf("on_trip plans from now; time and arrive_by don't apply")
	}
	if req.ArriveBy && req.Time == nil {
		return nil, badf("arrive_by needs a time")
	}
	set, err := parseLines(req.Lines)
	if err != nil {
		return nil, err
	}
	if err := s.ensure(set); err != nil {
		return nil, err
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
	var ob *plan.Onboard
	var access []raptor.Access
	var fromAp, toAp engine.Approach
	if onboard {
		ob, err = boarded(snap, req.From.OnTrip, snap.Secs(leave))
		if err != nil {
			return nil, err
		}
		access = plan.OnboardAccess(snap.Net, ob, snap.Secs(leave))
		if len(access) == 0 {
			return nil, badf("that trip has already finished")
		}
		window = 0
	} else if access, fromAp, err = s.access(snap, req.From, maxWalk, opts); err != nil {
		return nil, err
	}
	egress, toAp, err := s.access(snap, req.To, maxWalk, opts)
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
	preq := plan.Request{
		Access: access, Egress: egress, Depart: snap.Secs(leave), Window: window,
		MinChange: minChange, Overrides: overrides, Thresholds: th,
		MaxRides: cfg.Routing.MaxRides, AltSlack: cfg.Routing.AlternativesS,
		Allow:   func(r int32) bool { return allowed[r] },
		Onboard: ob,
	}
	var options []plan.Option
	if req.ArriveBy {
		options = plan.ArriveBy(snap.Net, preq, snap.Secs(leave), min(cfg.Routing.MaxWindowS, arriveByLookback))
	} else {
		options = plan.Plan(snap.Net, preq)
	}

	resp := &planResp{ServiceDate: snap.Date.Format("2006-01-02"), Realtime: snap.Realtime, Options: []optionResp{}, Walking: "estimate"}
	if s.eng.Walker() != nil {
		resp.Walking = "streets"
	}
	if snap.Realtime {
		at := snap.RealtimeAt
		resp.RealtimeAt = &at
	}
	buffer := req.Prefs.LeaveBufferS
	if onboard {
		buffer = 0 // already on the way
	}
	for _, o := range options {
		resp.Options = append(resp.Options, optionJSON(snap, o, buffer, fromAp, toAp, s.eng.WalkPath))
	}
	return resp, nil
}

// arriveByLookback is how far before the deadline arrive-by searches for departures.
const arriveByLookback = 150 * 60

// boarded finds the trip the traveller is on and where they boarded.
func boarded(snap *engine.Snapshot, ot *onTripReq, now int32) (*plan.Onboard, error) {
	d := snap.Day
	for _, ti := range d.TripIndex[ot.TripID] {
		t := &d.Trips[ti]
		if t.Status == gtfs.Cancelled || len(t.StopTimes) == 0 || t.StopTimes[len(t.StopTimes)-1].Arr < now-3600 {
			continue // not this day's run
		}
		for i, st := range t.StopTimes {
			if d.Stops[st.Stop].ID == ot.FromStop {
				return &plan.Onboard{Trip: ti, BoardCall: int32(i)}, nil
			}
		}
	}
	return nil, badf("trip or boarding stop not found in today's timetable")
}

func checkPlace(p placeReq) error {
	if p.OnTrip != nil {
		if p.Lat != nil || p.Lon != nil || len(p.Access) > 0 || len(p.Walks) > 0 || p.OnTrip.TripID == "" || p.OnTrip.FromStop == "" ||
			len(p.OnTrip.TripID)+len(p.OnTrip.FromStop) > 200 {
			return badf("on_trip needs trip_id and from_stop, and nothing else")
		}
		return nil
	}
	if p.Lat == nil || p.Lon == nil {
		return badf("each place needs lat/lon")
	}
	if math.IsNaN(*p.Lat) || *p.Lat < -90 || *p.Lat > 90 || math.IsNaN(*p.Lon) || *p.Lon < -180 || *p.Lon > 180 {
		return badf("lat/lon out of range")
	}
	if len(p.Access) > maxAccess {
		return badf("at most %d access stops", maxAccess)
	}
	if len(p.Walks) > maxWalks {
		return badf("at most %d walks", maxWalks)
	}
	for _, a := range append(p.Access, p.Walks...) {
		if a.WalkS < 0 || a.WalkS > maxWalkS {
			return badf("walk_s must be 0..%d", maxWalkS)
		}
		if a.Stop == "" || len(a.Stop) > 64 {
			return badf("each walk needs a stop")
		}
	}
	return nil
}

// Limits on the lines a request may name.
const (
	maxRequestLines = 60
	maxLineLen      = 40
)

// parseLines validates a request's line list.
func parseLines(ss []string) (lines.Set, error) {
	if len(ss) == 0 {
		return nil, badf("lines are required")
	}
	if len(ss) > maxRequestLines {
		return nil, badf("at most %d lines", maxRequestLines)
	}
	for _, s := range ss {
		if len(s) > maxLineLen {
			return nil, badf("line name too long")
		}
	}
	set, err := lines.ParseSet(ss...)
	if err != nil {
		return nil, badf("%v", err)
	}
	return set, nil
}

// ensure makes the server cover the lines, which can take a few seconds the first time.
func (s *Server) ensure(set lines.Set) error {
	err := s.eng.Ensure(set)
	if errors.Is(err, engine.ErrTooManyLines) {
		return badf("%v", err)
	}
	return err
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

// access resolves a place to stops with walking times: curated stops if given, otherwise nearby stops
// reached along the streets (or by a straight-line estimate until the street network exists). Timed walks
// then override those times and add their stops.
func (s *Server) access(snap *engine.Snapshot, p placeReq, maxWalk float64, o raptor.Options) ([]raptor.Access, engine.Approach, error) {
	out, ap, err := s.baseAccess(snap, p, maxWalk, o)
	if len(p.Walks) == 0 {
		return out, ap, err
	}
	timed := withWalks(snap.Day, out, p.Walks)
	if len(timed) == 0 {
		return nil, ap, err
	}
	if len(out) == 0 && ap.Access == nil { // only timed stops: draw the street routes anyway
		ap = s.eng.PathsFrom(snap.Net, geo.Point{Lat: *p.Lat, Lon: *p.Lon}, math.Min(maxWalk*3, 6000))
	}
	return timed, ap, nil
}

// withWalks applies timed walks (by stop or parent station ID) to access stops, adding stops not yet there.
func withWalks(d *gtfs.Day, access []raptor.Access, walks []accessReq) []raptor.Access {
	secs := map[string]int32{}
	for _, w := range walks {
		secs[w.Stop] = w.WalkS
	}
	timed := func(si int32) (int32, bool) {
		st := d.Stops[si]
		if v, ok := secs[st.ID]; ok {
			return v, true
		}
		v, ok := secs[st.Parent]
		return v, ok && st.Parent != ""
	}
	out := make([]raptor.Access, 0, len(access)+len(walks))
	have := map[int32]bool{}
	for _, a := range access {
		if v, ok := timed(a.Stop); ok {
			a.Secs = v
		}
		out = append(out, a)
		have[a.Stop] = true
	}
	for si, st := range d.Stops {
		if v, ok := timed(int32(si)); ok && !have[int32(si)] && st.LocationType != "1" { // board at platforms, not the station
			out = append(out, raptor.Access{Stop: int32(si), Secs: v})
		}
	}
	return out
}

func (s *Server) baseAccess(snap *engine.Snapshot, p placeReq, maxWalk float64, o raptor.Options) ([]raptor.Access, engine.Approach, error) {
	if len(p.Access) > 0 {
		var out []raptor.Access
		for _, a := range p.Access {
			if si, ok := snap.Day.StopIndex[a.Stop]; ok {
				out = append(out, raptor.Access{Stop: si, Secs: a.WalkS})
			}
		}
		if len(out) == 0 {
			return nil, engine.Approach{}, badf("none of the access stops exist in the timetable")
		}
		// The times are the person's own; draw the routes along the streets anyway.
		var far int32
		for _, a := range out {
			far = max(far, a.Secs)
		}
		reachM := math.Min(math.Max(float64(far)*o.WalkSpeedMps*1.5, 1500), 6000)
		return out, s.eng.PathsFrom(snap.Net, geo.Point{Lat: *p.Lat, Lon: *p.Lon}, reachM), nil
	}
	ap := s.eng.Approach(snap.Net, geo.Point{Lat: *p.Lat, Lon: *p.Lon}, maxWalk, o)
	if len(ap.Access) == 0 {
		return nil, ap, badf("no stops on those lines within %.0f m", maxWalk)
	}
	return ap.Access, ap, nil
}

func stopJSON(d *gtfs.Day, s int32) *stopResp {
	if s < 0 {
		return nil
	}
	st := d.Stops[s]
	out := &stopResp{ID: st.ID, Name: st.Name, Lat: st.Pos.Lat, Lon: st.Pos.Lon}
	if st.Parent != "" {
		if pi, ok := d.StopIndex[st.Parent]; ok {
			out.Station, out.StationID = d.Stops[pi].Name, st.Parent
		}
	}
	return out
}

func optionJSON(snap *engine.Snapshot, o plan.Option, buffer int32, fromAp, toAp engine.Approach, walkPath func(a, b geo.Point) ([]geo.Point, bool)) optionResp {
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
		if i == 0 && l.Kind == raptor.Walk {
			dep = leave
		}
		lr := legResp{From: stopJSON(d, l.From), To: stopJSON(d, l.To), Dep: snap.Clock(dep), Arr: snap.Clock(l.Arr)}
		if l.Kind == raptor.Walk {
			lr.Kind = "walk"
			switch {
			case l.From < 0 && l.To >= 0:
				if pts, ok := fromAp.Path(l.To); ok {
					lr.Path = coords(pts)
				}
			case l.From >= 0 && l.To >= 0:
				if pts, ok := changePath(d.Stops[l.From], d.Stops[l.To], walkPath); ok {
					lr.Path = coords(pts)
				}
			case l.To < 0 && l.From >= 0:
				if pts, ok := toAp.Path(l.From); ok {
					for i, j := 0, len(pts)-1; i < j; i, j = i+1, j-1 { // the path runs from the place to the stop
						pts[i], pts[j] = pts[j], pts[i]
					}
					lr.Path = coords(pts)
				}
			}
		} else {
			lr.Kind = "ride"
			r := d.Routes[l.Route]
			k := lines.Of(r.Type, r.ShortName)
			lr.Line = &lineResp{Mode: string(k.Mode), Name: k.Name, Color: r.Color, TextColor: r.TextColor}
			t := &d.Trips[l.Trip]
			lr.TripID, lr.Headsign = t.ID, t.Headsign
			lr.Stops = l.AlightIdx - l.BoardIdx
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

// stopsNear lists stops near a point with every line that serves them, for choosing a place's stops.
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
	cat := s.eng.Catalog()
	if cat == nil {
		w.Header().Set("Retry-After", "5")
		writeError(w, http.StatusServiceUnavailable, "still reading the timetable; try again in a few seconds")
		return
	}
	near, streets := s.eng.NearbyStops(cat, geo.Point{Lat: req.Lat, Lon: req.Lon}, req.RadiusM, s.eng.RoutingOptions())
	out := make([]nearStopResp, 0, len(near))
	for _, n := range near {
		ls := make([]string, len(n.Lines))
		for i, k := range n.Lines {
			ls[i] = k.String()
		}
		out = append(out, nearStopResp{
			stopResp: stopResp{ID: n.ID, Name: n.Name, Station: n.Station, StationID: n.StationID, Lat: n.Pos.Lat, Lon: n.Pos.Lon},
			WalkS:    n.WalkS, Lines: ls,
		})
	}
	walking := "estimate"
	if streets {
		walking = "streets"
	}
	writeJSON(w, http.StatusOK, map[string]any{"stops": out, "walking": walking})
}

type suggestPlaceReq struct {
	Lat    *float64    `json:"lat"`
	Lon    *float64    `json:"lon"`
	Access []accessReq `json:"access,omitempty"`
}

type suggestReq struct {
	From    suggestPlaceReq   `json:"from"`
	To      []suggestPlaceReq `json:"to"` // one or more destinations, answered in the same order
	RadiusM float64           `json:"radius_m,omitempty"`
}

type suggestLineResp struct {
	Line        string  `json:"line"`
	Color       string  `json:"color,omitempty"`
	Share       float64 `json:"share"`
	Recommended bool    `json:"recommended"`
}

type suggestWindowResp struct {
	Label      string `json:"label"`
	Date       string `json:"date"`
	Departures int    `json:"departures"`
	TypicalS   int32  `json:"typical_s,omitempty"`
}

type suggestItinResp struct {
	Desc    string   `json:"desc"`
	Lines   []string `json:"lines"`
	MedianS int32    `json:"median_s"`
	BestS   int32    `json:"best_s"`
	Seen    int      `json:"seen"`
	Of      int      `json:"of"`
	Window  string   `json:"window"`
}

// maxSuggestItineraries bounds how many example itineraries come back.
const maxSuggestItineraries = 8

type suggestResultResp struct {
	Windows     []suggestWindowResp `json:"windows"`
	Lines       []suggestLineResp   `json:"lines"`
	Itineraries []suggestItinResp   `json:"itineraries"`
}

// suggestPlace validates one end of a suggestion request.
func suggestPlace(p suggestPlaceReq) (engine.SuggestPlace, error) {
	if err := checkPlace(placeReq{Lat: p.Lat, Lon: p.Lon, Access: p.Access}); err != nil {
		return engine.SuggestPlace{}, err
	}
	out := engine.SuggestPlace{Pos: geo.Point{Lat: *p.Lat, Lon: *p.Lon}}
	for _, a := range p.Access {
		out.Access = append(out.Access, engine.StopWalk{Stop: a.Stop, WalkS: a.WalkS})
	}
	return out, nil
}

// suggestRadiusM is how far (straight line) a suggestion search looks for stops at each end.
const suggestRadiusM = 1200

// Hops shorter than this (platform to platform) aren't worth a route: a straight line is the walk.
const minChangePathM = 40

// changePath is the street route to draw for a change between two stops, if there is a sensible one. Within one
// station the walk is timed by the station's own pathways, so a street route is only used when it is about as long
// as the stops are far apart (a platform to a bus stand outside); a route that loops far round is not the walk.
func changePath(a, b gtfs.Stop, walkPath func(a, b geo.Point) ([]geo.Point, bool)) ([]geo.Point, bool) {
	dist := geo.DistanceM(a.Pos, b.Pos)
	same := a.Parent != "" && a.Parent == b.Parent
	if dist < minChangePathM && same {
		return nil, false
	}
	pts, ok := walkPath(a.Pos, b.Pos)
	if !ok {
		return nil, false
	}
	if same {
		var n float64
		for i := 1; i < len(pts); i++ {
			n += geo.DistanceM(pts[i-1], pts[i])
		}
		if n > 2*dist+50 {
			return nil, false
		}
	}
	return pts, true
}
