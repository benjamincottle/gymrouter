package engine

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"

	"github.com/benjamincottle/gymrouter/internal/gtfs"
	"github.com/benjamincottle/gymrouter/internal/raptor"
)

func TestGuardTurnsAPanicIntoALoggedError(t *testing.T) {
	var logs bytes.Buffer
	e := &Engine{log: slog.New(slog.NewTextHandler(&logs, nil))}
	err := e.guard("realtime polling", func() { panic("bad feed") })
	if err == nil || !strings.Contains(err.Error(), "realtime polling") {
		t.Errorf("error after a panic: %v", err)
	}
	if !strings.Contains(logs.String(), "bad feed") || !strings.Contains(logs.String(), "guard_internal_test.go") {
		t.Errorf("the panic and where it happened should be logged: %s", logs.String())
	}
	if err := e.guard("ok", func() {}); err != nil {
		t.Errorf("no panic: %v", err)
	}
}

func TestFootpathWorkerPanicReachesTheCaller(t *testing.T) {
	e := &Engine{}
	d := &gtfs.Day{Stops: []gtfs.Stop{{ID: "a"}, {ID: "b"}}}
	straight := [][]raptor.Footpath{{{To: 1, Secs: 60}}, nil}
	defer func() {
		if recover() == nil {
			t.Error("a worker's panic should be raised again on the calling goroutine")
		}
	}()
	e.streetFootpaths(nil, raptor.Options{WalkSpeedMps: 1.3}, d, []bool{true, false}, straight) // no graph: the worker panics
}
