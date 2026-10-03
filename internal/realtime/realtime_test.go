package realtime

import (
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/benjamincottle/gymrouter/internal/geo"
	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/gtfsrt/pb"
)

var sydney = mustLoc("Australia/Sydney")

func mustLoc(name string) *time.Location {
	l, err := time.LoadLocation(name)
	if err != nil {
		panic(err)
	}
	return l
}

func TestDecode(t *testing.T) {
	m := &pb.FeedMessage{
		Header: &pb.FeedHeader{GtfsRealtimeVersion: proto.String("2.0"), Timestamp: proto.Uint64(1790000000)},
		Entity: []*pb.FeedEntity{
			{Id: proto.String("1"), TripUpdate: &pb.TripUpdate{
				Trip: &pb.TripDescriptor{TripId: proto.String("t1"), RouteId: proto.String("r"), StartDate: proto.String("20261008")},
				StopTimeUpdate: []*pb.TripUpdate_StopTimeUpdate{
					{StopId: proto.String("S1"), Arrival: &pb.TripUpdate_StopTimeEvent{Delay: proto.Int32(60)}},
					{StopId: proto.String("S2"), ScheduleRelationship: pb.TripUpdate_StopTimeUpdate_SKIPPED.Enum()},
				},
			}},
			{Id: proto.String("2"), TripUpdate: &pb.TripUpdate{
				Trip: &pb.TripDescriptor{TripId: proto.String("t2"), ScheduleRelationship: pb.TripDescriptor_CANCELED.Enum()},
			}},
			{Id: proto.String("3"), Vehicle: &pb.VehiclePosition{
				Trip:     &pb.TripDescriptor{TripId: proto.String("t1")},
				Position: &pb.Position{Latitude: proto.Float32(-33.8), Longitude: proto.Float32(151.1), Bearing: proto.Float32(90)},
				Vehicle:  &pb.VehicleDescriptor{Id: proto.String("v1")},
			}},
		},
	}
	b, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	f, err := Decode(b)
	if err != nil {
		t.Fatal(err)
	}
	if f.Timestamp != 1790000000 || len(f.Trips) != 2 || len(f.Vehicles) != 1 {
		t.Fatalf("decoded %+v", f)
	}
	tu := f.Trips[0]
	if tu.TripID != "t1" || tu.StartDate != "20261008" || *tu.Stops[0].ArrDelay != 60 || !tu.Stops[1].Skipped {
		t.Errorf("trip update %+v", tu)
	}
	if !f.Trips[1].Cancelled {
		t.Error("cancellation lost")
	}
	if v := f.Vehicles[0]; v.TripID != "t1" || v.Bearing == nil || *v.Bearing != 90 || v.Lat > -33.79 {
		t.Errorf("vehicle %+v", v)
	}
	if _, err := Decode([]byte("not protobuf \xff\xff")); err == nil {
		t.Error("want error for garbage input")
	}
}

// Thursday 8 Oct 2026, Sydney (AEDT, UTC+11).
var testDate = time.Date(2026, 10, 8, 0, 0, 0, 0, sydney)

func testDay() *gtfs.Day {
	stops := []gtfs.Stop{{ID: "S1", Pos: geo.Point{Lat: -33.8, Lon: 151}}, {ID: "S2"}, {ID: "S3"}, {ID: "S4"}}
	idx := map[string]int32{"S1": 0, "S2": 1, "S3": 2, "S4": 3}
	call := func(s int32, seq int32, tm int32) gtfs.StopTime {
		return gtfs.StopTime{Stop: s, Seq: seq, Arr: tm, Dep: tm + 30}
	}
	calls := func(start int32) []gtfs.StopTime {
		return []gtfs.StopTime{call(0, 1, start), call(1, 2, start+300), call(2, 3, start+600), call(3, 4, start+900)}
	}
	d := &gtfs.Day{
		Date: testDate, Stops: stops, StopIndex: idx,
		Routes: []gtfs.Route{{ID: "BUS288", ShortName: "288", Type: 700}, {ID: "NTH_1a", ShortName: "T9", Type: 2}},
		Trips: []gtfs.Trip{
			{ID: "bus-1", Route: 0, StopTimes: calls(57600)},                        // 16:00
			{ID: "N721.1307.1.A.8.1", Route: 1, StopTimes: calls(58200)},            // 16:10, run N721
			{ID: "N721.1307.1.A.8.2", Route: 1, StopTimes: calls(72000)},            // 20:00, same run later
			{ID: "bus-1", Route: 0, StopTimes: calls(57600 - 86400), DayOffset: -1}, // yesterday's copy
		},
		TripIndex: map[string][]int32{"bus-1": {0, 3}, "N721.1307.1.A.8.1": {1}, "N721.1307.1.A.8.2": {2}},
	}
	return d
}

func i32(v int32) *int32 { return &v }
func i64(v int64) *int64 { return &v }

func at(secs int32) *int64 { return i64(ServiceMidnight(testDate, sydney).Unix() + int64(secs)) }

func TestApplyDelayPropagatesAndSkips(t *testing.T) {
	d := testDay()
	nd, st := Apply(d, sydney, []TripUpdate{{
		TripID: "bus-1", StartDate: "20261008",
		Stops: []StopUpdate{
			{StopID: "S2", ArrDelay: i32(120)},
			{StopID: "S3", Skipped: true},
		},
	}})
	if st.Matched != 1 || st.Updates != 1 {
		t.Fatalf("stats %+v", st)
	}
	tr := nd.Trips[0]
	if tr.Status != gtfs.Predicted || tr.Sched == nil {
		t.Fatalf("status %v", tr.Status)
	}
	s := tr.StopTimes
	if s[0].Arr != 57600 { // before the first prediction: on schedule
		t.Errorf("S1 arr %d", s[0].Arr)
	}
	if s[1].Arr != 57900+120 || s[1].Dep != 57930+120 {
		t.Errorf("S2 %d/%d", s[1].Arr, s[1].Dep)
	}
	if s[2].Arr != gtfs.NoTime || s[2].Dep != gtfs.NoTime {
		t.Errorf("S3 should be skipped: %+v", s[2])
	}
	if s[3].Arr != 58500+120 { // delay carries past the skipped stop
		t.Errorf("S4 arr %d", s[3].Arr)
	}
	if d.Trips[0].StopTimes[1].Arr != 57900 || d.Trips[0].Status != gtfs.Scheduled {
		t.Error("Apply modified the input day")
	}
	if nd.Trips[3].Status != gtfs.Scheduled {
		t.Error("start_date should select today's copy, not yesterday's")
	}
}

func TestApplyAbsoluteTimesAndMonotonic(t *testing.T) {
	d := testDay()
	nd, _ := Apply(d, sydney, []TripUpdate{{
		TripID: "bus-1", StartDate: "20261008",
		Stops: []StopUpdate{
			{StopID: "S1", DepTime: at(57600 + 400)}, // leaves 6m40s late (vs 30 s dwell)
			{StopID: "S2", ArrTime: at(57900 + 100)}, // prediction earlier than possible → clamped
		},
	}})
	s := nd.Trips[0].StopTimes
	if s[0].Dep != 58000 {
		t.Errorf("S1 dep %d", s[0].Dep)
	}
	if s[1].Arr < s[0].Dep {
		t.Errorf("S2 arr %d before S1 dep %d", s[1].Arr, s[0].Dep)
	}
}

func TestApplyCancelled(t *testing.T) {
	nd, st := Apply(testDay(), sydney, []TripUpdate{{TripID: "bus-1", StartDate: "20261008", Cancelled: true}})
	if st.Cancelled != 1 || nd.Trips[0].Status != gtfs.Cancelled || nd.Trips[0].StopTimes[0].Dep != gtfs.NoTime {
		t.Errorf("cancel: %+v %+v", st, nd.Trips[0])
	}
}

func TestApplyTrainMatchedByRun(t *testing.T) {
	// Different timetable version in the trip ID; predicted time close to the 16:10 run.
	nd, st := Apply(testDay(), sydney, []TripUpdate{{
		TripID: "N721.1309.9.A.8.99", RouteID: "NTH_1a",
		Stops: []StopUpdate{{StopID: "S2", ArrTime: at(58500 + 180)}},
	}})
	if st.MatchedByRun != 1 {
		t.Fatalf("stats %+v", st)
	}
	if nd.Trips[1].Status != gtfs.Predicted || nd.Trips[1].StopTimes[1].Arr != 58500+180 {
		t.Errorf("16:10 run not updated: %+v", nd.Trips[1])
	}
	if nd.Trips[2].Status != gtfs.Scheduled {
		t.Error("20:00 run wrongly updated")
	}
}

func TestApplyAddedTripAndUnknownRoutes(t *testing.T) {
	nd, st := Apply(testDay(), sydney, []TripUpdate{
		{TripID: "extra", RouteID: "BUS288", Added: true, Stops: []StopUpdate{
			{StopID: "S1", DepTime: at(60000)}, {StopID: "S4", ArrTime: at(60900)},
		}},
		{TripID: "elsewhere", RouteID: "OTHER", Stops: []StopUpdate{{StopID: "S1", ArrDelay: i32(60)}}},
		{TripID: "no-times", RouteID: "BUS288", Stops: []StopUpdate{{StopID: "S1", ArrDelay: i32(60)}}},
		{TripID: "N999.1309.1.A.8.5", RouteID: "NTH_1a"}, // other timetable version, no predictions
	})
	if st.Added != 1 || st.Updates != 2 || st.Unmatched != 1 || st.Empty != 1 {
		t.Fatalf("stats %+v", st)
	}
	ids := nd.TripIndex["extra"]
	if len(ids) != 1 || nd.Trips[ids[0]].Status != gtfs.Added || len(nd.Trips[ids[0]].StopTimes) != 2 {
		t.Errorf("added trip: %+v", nd.Trips)
	}
}

func TestServiceMidnightAcrossDST(t *testing.T) {
	// DST starts 4 Oct 2026 at 02:00 in Sydney. GTFS times count from noon minus 12 h,
	// which that day is 23:00 the previous evening (local), not 00:00.
	m := ServiceMidnight(time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC), sydney).In(sydney)
	if m.Day() != 3 || m.Hour() != 23 {
		t.Errorf("service midnight %v, want 2026-10-03 23:00 local", m)
	}
	m = ServiceMidnight(time.Date(2026, 10, 8, 0, 0, 0, 0, time.UTC), sydney).In(sydney)
	if m.Day() != 8 || m.Hour() != 0 {
		t.Errorf("service midnight %v, want 2026-10-08 00:00 local", m)
	}
}
