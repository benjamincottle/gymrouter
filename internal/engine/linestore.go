package engine

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/benjamincottle/gymrouter/internal/lines"
)

// The lines requests have named are kept in the data directory, so a restart loads them straight away instead of
// on the first search that names each one (which waits for the whole timetable to be read again).
const linesFile = "lines.json"

// usedSaveEvery is how stale a line's saved last use may get before it is written again: often enough for pruning,
// rarely enough that requests don't write a file each time.
const usedSaveEvery = time.Hour

// loadSavedLines adds the lines saved by an earlier run (and their last use) to the covered set, except those
// unused for longer than lineKeep. It runs before the first timetable load.
func (e *Engine) loadSavedLines() {
	b, err := os.ReadFile(filepath.Join(e.cfg.Server.DataDir, linesFile))
	if err != nil {
		if !os.IsNotExist(err) {
			e.log.Warn("saved lines unreadable; starting without them", "err", err)
		}
		return
	}
	var saved map[string]time.Time
	if err := json.Unmarshal(b, &saved); err != nil {
		e.log.Warn("saved lines unreadable; starting without them", "err", err)
		return
	}
	type entry struct {
		key  lines.Key
		used time.Time
	}
	var keep []entry
	now := e.now()
	for s, used := range saved {
		if k, err := lines.Parse(s); err == nil && now.Sub(used) <= lineKeep {
			keep = append(keep, entry{k, used})
		}
	}
	sort.Slice(keep, func(a, b int) bool { return keep[a].used.After(keep[b].used) }) // most recent first, if over the cap
	e.linesMu.Lock()
	e.usedMu.Lock()
	for _, en := range keep {
		if len(e.all) >= MaxLoadedLines && !e.all[en.key] {
			continue
		}
		e.all[en.key] = true
		e.lastUsed[en.key] = en.used
	}
	e.usedMu.Unlock()
	e.linesMu.Unlock()
	e.addFeeds(e.Lines())
	if len(keep) > 0 {
		e.log.Info("loading the lines saved by the last run", "lines", len(keep))
	}
}

// saveLines writes the covered lines' last use (the built-in gyms' lines are always covered, so aren't saved).
func (e *Engine) saveLines() {
	e.saveMu.Lock()
	defer e.saveMu.Unlock()
	have := e.Lines()
	out := map[string]time.Time{}
	e.usedMu.Lock()
	for k, at := range e.lastUsed {
		if have[k] && !e.preload[k] {
			out[k.String()] = at
		}
	}
	e.usedMu.Unlock()
	b, err := json.Marshal(out)
	if err == nil {
		err = writeAtomic(filepath.Join(e.cfg.Server.DataDir, linesFile), b)
	}
	if err != nil {
		e.log.Warn("couldn't save the loaded lines", "err", err)
	}
}

func writeAtomic(path string, b []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Prefetch loads lines in the background, so that the first search naming them doesn't wait. The app sends its
// gyms' lines as soon as it knows them (on opening, after setup, when a gym's lines change). Requests that arrive
// while a load is under way are merged into one more load. Lines the timetable doesn't have are skipped: a search
// that names one says so. It fails only when the lines would go over MaxLoadedLines.
func (e *Engine) Prefetch(set lines.Set) error {
	have := e.Lines()
	want := lines.Set{}
	c := e.Catalog()
	for k := range set {
		if have[k] || c == nil || c.Lines[k] {
			want[k] = true
		}
	}
	e.prefetchMu.Lock()
	defer e.prefetchMu.Unlock()
	if len(lines.Union(have, e.pending, want)) > MaxLoadedLines {
		return ErrTooManyLines
	}
	if e.covers(want) {
		e.used(want)
		return nil
	}
	e.pending = lines.Union(e.pending, want)
	if !e.prefetching {
		e.prefetching = true
		go e.prefetchLoop()
	}
	return nil
}

func (e *Engine) prefetchLoop() {
	for {
		e.prefetchMu.Lock()
		set := e.pending
		e.pending = nil
		if len(set) == 0 {
			e.prefetching = false
			e.prefetchMu.Unlock()
			return
		}
		e.prefetchMu.Unlock()
		_ = e.guard("loading lines", func() {
			start := e.now()
			if err := e.Ensure(set); err != nil {
				e.log.Warn("loading lines ahead of a search failed", "err", err)
				return
			}
			e.log.Info("lines loaded ahead of a search", "lines", len(set), "took", e.now().Sub(start).Round(time.Millisecond).String())
		})
	}
}
