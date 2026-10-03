// Package realtime decodes GTFS-realtime feeds and applies trip updates to a loaded service day.
package realtime

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/benjamincottle/gymrouter/internal/gtfsrt/pb"
)

// StopUpdate is a prediction for one call of a trip. Absent values are nil.
type StopUpdate struct {
	Seq      *int32
	StopID   string
	ArrDelay *int32
	DepDelay *int32
	ArrTime  *int64 // unix seconds
	DepTime  *int64
	Skipped  bool
	NoData   bool
}

// TripUpdate is a prediction for one trip.
type TripUpdate struct {
	TripID, RouteID, StartDate, StartTime string
	Cancelled, Added                      bool
	Delay                                 *int32 // trip-level delay, used when no stop has a prediction
	Stops                                 []StopUpdate
	Timestamp                             int64
}

// Vehicle is a live vehicle position.
type Vehicle struct {
	ID, Label       string
	TripID, RouteID string
	StartDate       string
	Lat, Lon        float64
	Bearing         *float32
	StopID          string
	Status          string // INCOMING_AT, STOPPED_AT, IN_TRANSIT_TO
	Timestamp       int64
}

// Feed is a decoded GTFS-realtime message.
type Feed struct {
	Timestamp int64
	Trips     []TripUpdate
	Vehicles  []Vehicle
}

// Decode parses a GTFS-realtime FeedMessage. Unknown fields (e.g. TfNSW extensions) are ignored.
func Decode(b []byte) (*Feed, error) {
	var m pb.FeedMessage
	if err := proto.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("decode gtfs-realtime: %w", err)
	}
	f := &Feed{Timestamp: int64(m.GetHeader().GetTimestamp())}
	for _, e := range m.GetEntity() {
		if e.GetIsDeleted() {
			continue
		}
		if tu := e.GetTripUpdate(); tu != nil {
			f.Trips = append(f.Trips, tripUpdate(tu))
		}
		if vp := e.GetVehicle(); vp != nil && vp.GetPosition() != nil {
			f.Vehicles = append(f.Vehicles, vehicle(vp))
		}
	}
	return f, nil
}

func tripUpdate(tu *pb.TripUpdate) TripUpdate {
	td := tu.GetTrip()
	out := TripUpdate{
		TripID: td.GetTripId(), RouteID: td.GetRouteId(), StartDate: td.GetStartDate(), StartTime: td.GetStartTime(),
		Timestamp: int64(tu.GetTimestamp()),
	}
	switch td.GetScheduleRelationship() {
	case pb.TripDescriptor_CANCELED, pb.TripDescriptor_DELETED:
		out.Cancelled = true
	case pb.TripDescriptor_ADDED, pb.TripDescriptor_NEW:
		out.Added = true
	}
	if tu.Delay != nil {
		d := tu.GetDelay()
		out.Delay = &d
	}
	for _, s := range tu.GetStopTimeUpdate() {
		su := StopUpdate{StopID: s.GetStopId()}
		if s.StopSequence != nil {
			v := int32(s.GetStopSequence())
			su.Seq = &v
		}
		switch s.GetScheduleRelationship() {
		case pb.TripUpdate_StopTimeUpdate_SKIPPED:
			su.Skipped = true
		case pb.TripUpdate_StopTimeUpdate_NO_DATA:
			su.NoData = true
		}
		if a := s.GetArrival(); a != nil {
			if a.Delay != nil {
				v := a.GetDelay()
				su.ArrDelay = &v
			}
			if a.Time != nil {
				v := a.GetTime()
				su.ArrTime = &v
			}
		}
		if dp := s.GetDeparture(); dp != nil {
			if dp.Delay != nil {
				v := dp.GetDelay()
				su.DepDelay = &v
			}
			if dp.Time != nil {
				v := dp.GetTime()
				su.DepTime = &v
			}
		}
		out.Stops = append(out.Stops, su)
	}
	return out
}

func vehicle(vp *pb.VehiclePosition) Vehicle {
	v := Vehicle{
		ID: vp.GetVehicle().GetId(), Label: vp.GetVehicle().GetLabel(),
		TripID: vp.GetTrip().GetTripId(), RouteID: vp.GetTrip().GetRouteId(), StartDate: vp.GetTrip().GetStartDate(),
		Lat: float64(vp.GetPosition().GetLatitude()), Lon: float64(vp.GetPosition().GetLongitude()),
		StopID: vp.GetStopId(), Timestamp: int64(vp.GetTimestamp()),
	}
	if vp.GetPosition().Bearing != nil {
		b := vp.GetPosition().GetBearing()
		v.Bearing = &b
	}
	if vp.CurrentStatus != nil {
		v.Status = vp.GetCurrentStatus().String()
	}
	return v
}
