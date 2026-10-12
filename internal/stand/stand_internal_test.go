package stand

import (
	"math"
	"testing"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/walk"
)

const lat0 = -33.9

// east is the point so many metres east of another (and north, for the second number).
func east(p geo.Point, m, north float64) geo.Point {
	return geo.Point{Lat: p.Lat + north/mPerDegLat, Lon: p.Lon + m/(math.Cos(lat0*math.Pi/180)*111320)}
}

// slab is a platform's outline: its west end, how long it runs east, 8 m wide.
func slab(west geo.Point, length float64) []geo.Point {
	return []geo.Point{west, east(west, length, 0), east(west, length, 8), east(west, 0, 8), west}
}

// world is two stations on a line running east. Trains board at Start (platforms S1, and a long S9) and get off at
// Far, whose platform F1 has stairs near its east end (to the north entrance), escalators towards its west end (to
// the south entrance), and a lift between; F2 is across F1; F3 is reached by the stairs; F9 is a long platform.
type world struct {
	day *gtfs.Day
	g   *walk.Graph
	st  *Stations
	at  map[string]int32
}

func newWorld() *world {
	w := &world{day: &gtfs.Day{StopIndex: map[string]int32{}}, g: &walk.Graph{}, at: map[string]int32{}}
	add := func(id, name, parent, kind, code string, p geo.Point) {
		w.at[id] = int32(len(w.day.Stops))
		w.day.StopIndex[id] = int32(len(w.day.Stops))
		w.day.Stops = append(w.day.Stops, gtfs.Stop{ID: id, Name: name, Parent: parent, LocationType: kind, Platform: code, Pos: p})
	}
	path := func(from, to string, mode int, secs int32) {
		w.day.Pathways = append(w.day.Pathways, gtfs.Pathway{From: from, To: to, Mode: mode, Secs: secs, Bidirectional: true})
	}
	platform := func(id, station, code string, west geo.Point, length float64) {
		add(id, station+" Station, Platform "+code, station, "", code, east(west, length/2, 4))
		w.g.Platforms = append(w.g.Platforms, walk.Platform{Ref: code, Outline: slab(west, length)})
	}
	// Start, 5 km west.
	sw := geo.Point{Lat: lat0, Lon: 151.0}
	add("Start", "Start Station", "", "1", "", sw)
	platform("S1", "Start", "1", sw, 163)
	add("S1-stairs", "STR To P1", "Start", "3", "", east(sw, 163-10, 4)) // 10 m from the front (east) end
	path("S1", "S1-stairs", gtfs.PathwayStairs, 30)
	platform("S9", "Start", "9", east(sw, 0, -60), 240)

	// Far.
	fw := geo.Point{Lat: lat0, Lon: 151.05}
	add("Far", "Far Station", "", "1", "", fw)
	platform("F1", "Far", "1", fw, 163)
	platform("F2", "Far", "2", east(fw, 0, 12), 163)
	platform("F3", "Far", "3", east(fw, 0, 60), 163)
	platform("F9", "Far", "9", east(fw, 0, -80), 240)
	add("F1-stairs", "STR To P1/2", "Far", "3", "", east(fw, 163-30, 4))  // 30 m from the east end
	add("F1-lift", "LFT To P1/2", "Far", "3", "", east(fw, 163-60, 4))    // 60 m
	add("F1-esc", "ESC To P1/2", "Far", "3", "", east(fw, 163-120, 4))    // 120 m
	add("F9-stairs", "STR To P9", "Far", "3", "", east(fw, 240-120, -76)) // 120 m from the east end of the long one
	add("F3-stairs", "STR To P3", "Far", "3", "", east(fw, 80, 64))
	add("Far-north", "ENT North St", "Far", "2", "", east(fw, 163-30, 120))
	add("Far-south", "ENT South St", "Far", "2", "", east(fw, 163-120, -160))
	path("F1", "F1-stairs", gtfs.PathwayStairs, 30)
	path("F1", "F1-lift", gtfs.PathwayElevator, 60)
	path("F1", "F1-esc", gtfs.PathwayEscalator, 30)
	path("F1", "F2", gtfs.PathwayWalkway, 20) // across the island
	path("F1-stairs", "Far-north", gtfs.PathwayWalkway, 20)
	path("F1-lift", "Far-north", gtfs.PathwayWalkway, 5)
	path("F1-esc", "Far-south", gtfs.PathwayWalkway, 20)
	path("F1-stairs", "concourse", gtfs.PathwayWalkway, 15) // a point with no position
	path("concourse", "F3-stairs", gtfs.PathwayWalkway, 15)
	path("F3-stairs", "F3", gtfs.PathwayStairs, 30)
	path("F9", "F9-stairs", gtfs.PathwayStairs, 30)
	path("F9-stairs", "Far-south", gtfs.PathwayWalkway, 20)
	add("Far-bus", "Far Station, Stand A", "Far", "", "", east(fw, 163-120, -170)) // a bus stand by the south entrance

	// The street map: F1's track is drawn eastwards, with a list for an 8-carriage train; S1's too.
	w.g.TrainStops = []walk.TrainStop{
		{Ref: "F1", Pos: east(fw, 80, -3), Bearing: 90, Forward: []string{"no", "stairs", "no", "lift", "no", "no", "escalator", "no"}},
		{Ref: "S1", Pos: east(sw, 80, -3), Bearing: 90, Forward: []string{"stairs", "no", "no", "no", "no", "no", "no", "no"}},
	}
	w.st = NewStations(w.day)
	return w
}

func (w *world) ride(board, alight, then string, goal geo.Point) Ride {
	r := Ride{Board: w.at[board], Alight: w.at[alight], Cars: 8, HeadingBoard: 90, HeadingAlight: 90, Then: -1, Goal: goal}
	if then != "" {
		r.Then, r.Goal = w.at[then], w.day.Stops[w.at[then]].Pos
	}
	return r
}

func TestAdvise(t *testing.T) {
	w := newWorld()
	north, south := w.day.Stops[w.at["Far-north"]].Pos, w.day.Stops[w.at["Far-south"]].Pos
	for name, tc := range map[string]struct {
		ride Ride
		ok   bool
		car  int
		by   Source
	}{
		// The escalators measure 120 m back, the 6th carriage; the map's list says the 7th.
		"out by the south entrance":                   {w.ride("S1", "F1", "", east(south, 0, -40)), true, 7, ByList},
		"out by the north entrance":                   {w.ride("S1", "F1", "", east(north, 0, 40)), true, 2, ByList},
		"to the bus stand, off the pathways":          {w.ride("S1", "F1", "Far-bus", geo.Point{}), true, 7, ByList},
		"changing to a platform up the stairs":        {w.ride("S1", "F1", "F3", geo.Point{}), true, 2, ByList},
		"changing to the platform across":             {w.ride("S1", "F1", "F2", geo.Point{}), false, 0, ""},
		"a platform with no list, the train's length": {w.ride("S1", "F3", "", east(north, 0, 40)), true, 5, ByMeasured}, // its stairs are 83 m back
		"a long platform with no list":                {w.ride("S1", "F9", "", south), true, 4, ByMiddle},                // 120 m back, the train 38 m in from the end
	} {
		got, ok := w.st.Advise(w.g, tc.ride)
		if ok != tc.ok || got.Carriage != tc.car || got.By != tc.by || (ok && got.Of != 8) {
			t.Errorf("%s: %+v, %v; want carriage %d by %q, %v", name, got, ok, tc.car, tc.by, tc.ok)
		}
	}

	// The other way along the same platform, its front is the west end: the list turns round with the train.
	back := w.ride("S1", "F1", "", east(south, 0, -40))
	back.HeadingAlight = 270
	if got, ok := w.st.Advise(w.g, back); !ok || got.Carriage != 2 || got.By != ByList {
		t.Errorf("westbound: %+v, %v", got, ok)
	}
	// A list for a train of another length isn't this train's: measured instead.
	short := w.ride("S1", "F1", "", east(south, 0, -40))
	short.Cars = 4
	if got, ok := w.st.Advise(w.g, short); !ok || got.By == ByList || got.Of != 4 {
		t.Errorf("a 4-carriage train: %+v, %v", got, ok)
	}
	// Nothing to say, or nothing known.
	stay := w.ride("S1", "F1", "", south)
	stay.Stay = true
	unknown := w.ride("S1", "F1", "", south)
	unknown.Cars = 0
	for name, r := range map[string]Ride{"the next train leaves from this platform": stay, "a train of unknown length": unknown} {
		if got, ok := w.st.Advise(w.g, r); ok {
			t.Errorf("%s: %+v", name, got)
		}
	}
	if got, ok := w.st.Advise(nil, w.ride("S1", "F1", "", south)); ok {
		t.Errorf("no street map: %+v", got)
	}
}

// One point with stairs and an escalator that cost the same: the list puts them beside different carriages, and the
// one nearer the point is taken, whichever the timetable lists first.
func TestStairsBesideAnEscalator(t *testing.T) {
	north := east(geo.Point{Lat: lat0, Lon: 151.05}, 163-30, 160)
	for name, first := range map[string]int{"stairs listed first": gtfs.PathwayStairs, "the escalator listed first": gtfs.PathwayEscalator} {
		w := newWorld()
		other := gtfs.PathwayEscalator + gtfs.PathwayStairs - first
		var paths []gtfs.Pathway
		for _, p := range w.day.Pathways {
			if p.From == "F1" && p.To == "F1-stairs" {
				paths = append(paths, gtfs.Pathway{From: "F1", To: "F1-stairs", Mode: first, Secs: 30, Bidirectional: true},
					gtfs.Pathway{From: "F1", To: "F1-stairs", Mode: other, Secs: 30, Bidirectional: true})
				continue
			}
			paths = append(paths, p)
		}
		w.day.Pathways = paths
		w.st = NewStations(w.day)
		// The point is 30 m back (the 2nd carriage); the list has stairs at the 2nd and the escalator at the 7th.
		if got, ok := w.st.Advise(w.g, w.ride("S1", "F1", "", north)); !ok || got.Carriage != 2 || got.By != ByList {
			t.Errorf("%s: %+v, %v", name, got, ok)
		}
	}
}

func TestNeverALift(t *testing.T) {
	w := newWorld()
	north := w.day.Stops[w.at["Far-north"]].Pos
	// The lift is the quickest way to the north entrance (65 s against the stairs' 50... make it so).
	for i, p := range w.day.Pathways {
		if p.From == "F1" && p.To == "F1-lift" {
			w.day.Pathways[i].Secs = 1
		}
	}
	w.st = NewStations(w.day)
	got, ok := w.st.Advise(w.g, w.ride("S1", "F1", "", east(north, 0, 40)))
	if !ok || got.Carriage != 2 {
		t.Errorf("with a quick lift: %+v, %v; want the stairs' carriage", got, ok)
	}
	// A lift by another name: the timetable calls the way to it a walkway.
	for i, p := range w.day.Pathways {
		if p.From == "F1" && p.To == "F1-lift" {
			w.day.Pathways[i].Mode = gtfs.PathwayWalkway
		}
	}
	w.st = NewStations(w.day)
	if got, ok := w.st.Advise(w.g, w.ride("S1", "F1", "", east(north, 0, 40))); !ok || got.Carriage != 2 {
		t.Errorf("with a lift called a walkway: %+v, %v", got, ok)
	}
	// A platform whose only way off is a lift: nothing to aim for.
	w.day.Pathways = append(w.day.Pathways, gtfs.Pathway{From: "S9", To: "F1-lift", Mode: gtfs.PathwayElevator, Secs: 30, Bidirectional: true})
	w.st = NewStations(w.day)
	if got, ok := w.st.Advise(w.g, w.ride("S1", "S9", "", north)); ok {
		t.Errorf("only a lift: %+v", got)
	}
}

func TestSpotOnThePlatformYouBoardFrom(t *testing.T) {
	w := newWorld()
	south := w.day.Stops[w.at["Far-south"]].Pos
	front := func(id string, length float64) geo.Point { // the middle of a platform's east end
		return east(w.g.Platforms[w.platformIndex(id)].Outline[0], length, 4)
	}
	// S1's list puts its stairs (10 m back) beside the 1st carriage, so the train's front is at the platform's end,
	// and the 7th carriage's middle is 6.5 carriages back.
	got, ok := w.st.Advise(w.g, w.ride("S1", "F1", "", east(south, 0, -40)))
	if !ok || !got.HasSpot || got.Carriage != 7 {
		t.Fatalf("%+v, %v", got, ok)
	}
	if d := geo.DistanceM(got.Spot, front("S1", 163)); math.Abs(d-6.5*trainCarM) > 3 {
		t.Errorf("the spot is %.0f m from the front end, want about %.0f", d, 6.5*trainCarM)
	}
	// From a long platform with no list, the train is taken to be in the middle of it: 38 m in.
	got, ok = w.st.Advise(w.g, w.ride("S9", "F1", "", east(south, 0, -40)))
	if !ok || !got.HasSpot {
		t.Fatalf("%+v, %v", got, ok)
	}
	if d := geo.DistanceM(got.Spot, front("S9", 240)); math.Abs(d-(38.4+6.5*trainCarM)) > 3 {
		t.Errorf("from the long platform the spot is %.0f m from the front end, want about %.0f", d, 38.4+6.5*trainCarM)
	}
}

// platformIndex finds a platform's outline: the platforms' stops and their outlines were added in step.
func (w *world) platformIndex(id string) int {
	n := 0
	for _, s := range w.day.Stops {
		if !platform(s) || s.Platform == "" {
			continue
		}
		if s.ID == id {
			return n
		}
		n++
	}
	return -1
}

func TestTheMetroSaysWhereItStops(t *testing.T) {
	w := newWorld()
	south := w.day.Stops[w.at["Far-south"]].Pos
	old := metroFront
	defer func() { metroFront = old }()
	// F9 is 240 m and a metro train 132 m: in the middle its front would be 54 m in, and the stairs (120 m back)
	// beside the 3rd carriage. It reports stopping 10 m in: the 5th.
	fw := w.g.Platforms[w.platformIndex("F9")].Outline[0]
	r := w.ride("S1", "F9", "", south)
	r.Metro, r.Cars = true, MetroCars
	if got, ok := w.st.Advise(w.g, r); !ok || got.Carriage != 3 || got.By != ByMiddle {
		t.Errorf("no reported stop: %+v, %v", got, ok)
	}
	metroFront = map[string]geo.Point{"F9": east(fw, 240-10, -3)}
	if got, ok := w.st.Advise(w.g, r); !ok || got.Carriage != 5 || got.By != ByStop || got.Of != MetroCars {
		t.Errorf("with a reported stop: %+v, %v", got, ok)
	}
}

func TestCars(t *testing.T) {
	for id, want := range map[string]int{
		"839P.1414.159.2.A.8.91434157": 8,
		"126T.1414.159.2.T.4.91434077": 4,
		"1--A.947.166.4.D.10.91440394": 10,
		"0250-004-109-006:1000":        0,
		"839P.1414.159.2.A.x.91434157": 0,
		"839P.1414.159.2.A.99.9143415": 0,
		"":                             0,
	} {
		if got := Cars(id, false); got != want {
			t.Errorf("%q: %d, want %d", id, got, want)
		}
	}
	if Cars("0250-004-109-006:1000", true) != MetroCars {
		t.Error("the metro's trains are all one length")
	}
}

func TestTheMetroStopsAreBuiltIn(t *testing.T) {
	if len(metroFront) < 40 {
		t.Fatalf("%d metro stops", len(metroFront))
	}
	for id, p := range metroFront {
		if p.Lat > -33.6 || p.Lat < -34 || p.Lon < 150.8 || p.Lon > 151.3 {
			t.Errorf("%s is at %v", id, p)
		}
	}
}
