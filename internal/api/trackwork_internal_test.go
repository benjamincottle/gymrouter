package api

import (
	"reflect"
	"testing"

	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/lines"
	"github.com/benjamincottle/gymrouter/internal/plan"
)

func TestTrackworkNotice(t *testing.T) {
	d := &gtfs.Day{Routes: []gtfs.Route{{ShortName: "T4", Type: 2, Color: "005AA3"}, {ShortName: "23T4", Type: 714}}}
	key := func(s string) lines.Key { k, _ := lines.Parse(s); return k }
	options := []plan.Option{
		{Lines: []lines.Key{key("replacement-bus 23T4"), key("metro M1"), key("bus 320")}},
		{Lines: []lines.Key{key("replacement-bus 20T4"), key("train T2")}},
		{Lines: []lines.Key{key("replacement-bus 23T4"), key("replacement-bus 5B")}}, // an event shuttle replaces nothing
	}
	got := trackwork(d, options)
	want := []trackworkResp{{Line: lineResp{Mode: "train", Name: "T4", Color: "005AA3"}, Buses: []string{"20T4", "23T4"}}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("trackwork = %+v, want %+v", got, want)
	}
	if got := trackwork(d, options[:0]); got != nil {
		t.Errorf("no options, no notice: got %+v", got)
	}
}
