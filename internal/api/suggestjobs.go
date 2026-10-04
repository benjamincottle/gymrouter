package api

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"net/http"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	"github.com/benjamincottle/gymrouter/internal/engine"
)

// A line suggestion reads the whole timetable for two days, which on a small server takes long enough for a proxy or
// the phone to give up on the request (adding all the built-in gyms at once did). So it runs as a job: POST starts it
// and answers straight away, and the app polls for progress and then the result. Nothing outlives jobKeep.

const (
	jobTimeout = 10 * time.Minute // a search running longer than this has gone wrong
	jobKeep    = 10 * time.Minute // finished jobs are kept this long for the app to collect
	busyRetry  = 3 * time.Second  // another whole-network pass (the stop catalogue) is running; wait for it
)

type suggestJob struct {
	State   string              `json:"state"` // running | done | failed
	Done    int                 `json:"done"`  // steps finished, of Of (reading a day's timetable, searching a gym on it)
	Of      int                 `json:"of"`
	Results []suggestResultResp `json:"results,omitempty"`
	Error   string              `json:"error,omitempty"`
	ended   time.Time
}

type jobStore struct {
	mu   sync.Mutex
	jobs map[string]*suggestJob
}

// start registers a new job, unless one is already running. It also forgets jobs finished long ago.
func (js *jobStore) start(now time.Time) (string, *suggestJob, bool) {
	js.mu.Lock()
	defer js.mu.Unlock()
	if js.jobs == nil {
		js.jobs = map[string]*suggestJob{}
	}
	for id, j := range js.jobs {
		if j.State == "running" {
			return "", nil, false
		}
		if now.Sub(j.ended) > jobKeep {
			delete(js.jobs, id)
		}
	}
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	id := hex.EncodeToString(b)
	j := &suggestJob{State: "running"}
	js.jobs[id] = j
	return id, j, true
}

// update changes a job under the lock.
func (js *jobStore) update(j *suggestJob, f func(*suggestJob)) {
	js.mu.Lock()
	defer js.mu.Unlock()
	f(j)
}

// get returns a copy of a job's state.
func (js *jobStore) get(id string) (suggestJob, bool) {
	js.mu.Lock()
	defer js.mu.Unlock()
	j, ok := js.jobs[id]
	if !ok {
		return suggestJob{}, false
	}
	return *j, true
}

// suggestLines starts a job finding candidate lines from one place to each of several: POST /api/suggest-lines.
func (s *Server) suggestLines(w http.ResponseWriter, r *http.Request) {
	var req suggestReq
	if err := decode(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(req.To) == 0 || len(req.To) > engine.MaxSuggestTargets {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("to needs 1 to %d destinations", engine.MaxSuggestTargets))
		return
	}
	from, err := suggestPlace(req.From)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	targets := make([]engine.SuggestPlace, len(req.To))
	for i, t := range req.To {
		if targets[i], err = suggestPlace(t); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	if req.RadiusM < 0 || req.RadiusM > maxRadiusM {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("radius_m must be 0..%d", maxRadiusM))
		return
	}
	if req.RadiusM == 0 {
		req.RadiusM = suggestRadiusM
	}
	id, job, ok := s.jobs.start(time.Now())
	if !ok {
		w.Header().Set("Retry-After", "10")
		writeError(w, http.StatusTooManyRequests, "already looking up lines; try again in a few seconds")
		return
	}
	go s.runSuggest(job, from, targets, req.RadiusM)
	writeJSON(w, http.StatusAccepted, map[string]string{"job": id})
}

// suggestStatus reports a job's progress, and its result once done: GET /api/suggest-lines/{id}.
func (s *Server) suggestStatus(w http.ResponseWriter, r *http.Request) {
	j, ok := s.jobs.get(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such search (it may have expired)")
		return
	}
	writeJSON(w, http.StatusOK, j)
}

func (s *Server) runSuggest(job *suggestJob, from engine.SuggestPlace, targets []engine.SuggestPlace, radiusM float64) {
	// This runs outside any handler, so the recover middleware doesn't cover it: a panic here would stop the server.
	defer func() {
		if v := recover(); v != nil {
			s.log.Error("suggest lines panicked", "err", fmt.Sprint(v), "stack", string(debug.Stack()))
			s.jobs.update(job, func(j *suggestJob) {
				j.ended = time.Now()
				j.State, j.Error = "failed", "couldn't look up lines"
			})
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), jobTimeout)
	defer cancel()
	progress := func(done, of int) { s.jobs.update(job, func(j *suggestJob) { j.Done, j.Of = done, of }) }
	var results []*engine.SuggestResult
	var err error
	for {
		results, err = s.eng.Suggest(ctx, from, targets, radiusM, progress)
		if !errors.Is(err, engine.ErrBusy) || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
		case <-time.After(busyRetry):
		}
	}
	s.jobs.update(job, func(j *suggestJob) {
		j.ended = time.Now()
		switch {
		case errors.Is(err, engine.ErrNoStops):
			j.State, j.Error = "failed", err.Error()
		case err != nil:
			s.log.Error("suggest lines failed", "err", err)
			j.State, j.Error = "failed", "couldn't look up lines"
		default:
			j.State, j.Results = "done", suggestJSON(results)
		}
	})
}

func suggestJSON(results []*engine.SuggestResult) []suggestResultResp {
	out := make([]suggestResultResp, 0, len(results))
	for _, res := range results {
		one := suggestResultResp{Windows: []suggestWindowResp{}, Lines: []suggestLineResp{}, Itineraries: []suggestItinResp{}}
		for _, wn := range res.Windows {
			one.Windows = append(one.Windows, suggestWindowResp{Label: wn.Label, Date: wn.Date.Format("2006-01-02"),
				Departures: wn.Departures, TypicalS: wn.BestS})
		}
		for _, l := range res.Lines {
			one.Lines = append(one.Lines, suggestLineResp{Line: l.Line.String(), Color: l.Color, Share: math.Round(l.Share*100) / 100,
				Recommended: l.Recommended})
		}
		sort.SliceStable(res.Itineraries, func(a, b int) bool { return res.Itineraries[a].Seen > res.Itineraries[b].Seen })
		for _, it := range res.Itineraries {
			if len(one.Itineraries) >= maxSuggestItineraries {
				break
			}
			one.Itineraries = append(one.Itineraries, suggestItinResp{Desc: it.Desc, Lines: it.Lines, MedianS: it.MedianS,
				BestS: it.BestS, Seen: it.Seen, Of: it.Of, Window: it.Window})
		}
		out = append(out, one)
	}
	return out
}
