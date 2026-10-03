package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/benjamincottle/gymrouter/internal/walk"
)

// The area the app covers: Greater Sydney. min_lon, min_lat, max_lon, max_lat.
const mapBBox = "150.55,-34.15,151.35,-33.45"

const (
	walkRefreshAfter = 35 * 24 * time.Hour // BBBike rebuilds weekly; street layouts change slowly
	mapRefreshAfter  = 60 * 24 * time.Hour
	provisionRetry   = 6 * time.Hour
	provisionTick    = time.Hour
	maxWalkSource    = 250 << 20
	mapFile          = "map.pmtiles"
)

// DataStatus reports the self-provisioned files.
type DataStatus struct {
	WalkReady bool   `json:"walk_ready"`
	WalkAgeS  *int64 `json:"walk_age_s,omitempty"`
	WalkError string `json:"walk_error,omitempty"`
	MapReady  bool   `json:"map_ready"`
	MapAgeS   *int64 `json:"map_age_s,omitempty"`
	MapError  string `json:"map_error,omitempty"`
}

func (e *Engine) dataStatus() DataStatus {
	var d DataStatus
	age := func(path string) (*int64, bool) {
		fi, err := os.Stat(path)
		if err != nil {
			return nil, false
		}
		v := int64(e.now().Sub(fi.ModTime()).Seconds())
		return &v, true
	}
	d.WalkAgeS, d.WalkReady = age(filepath.Join(e.cfg.Server.DataDir, walkFile))
	d.WalkReady = d.WalkReady && e.Walker() != nil
	d.MapAgeS, d.MapReady = age(filepath.Join(e.cfg.Server.DataDir, mapFile))
	e.rtMu.Lock()
	d.WalkError, d.MapError = e.walkErr, e.mapErr
	e.rtMu.Unlock()
	return d
}

// provisionLoop fetches the street network and the basemap when they are missing or old, in the background.
// The app works without them (walks fall back to estimates, the map to a blank background), so failures only
// log, show in /api/status and retry later.
func (e *Engine) provisionLoop(ctx context.Context) {
	next := map[string]time.Time{} // when to look at each item again
	step := func(name string, due func() bool, run func(context.Context) error, setErr func(string)) {
		if e.now().Before(next[name]) || !due() {
			return
		}
		if err := run(ctx); err != nil {
			if ctx.Err() != nil {
				return
			}
			e.log.Warn("could not prepare "+name+"; will retry", "err", err)
			setErr(err.Error())
			next[name] = e.now().Add(provisionRetry)
			return
		}
		setErr("")
	}
	t := time.NewTicker(provisionTick)
	defer t.Stop()
	for {
		step("street network", e.walkDue, e.RefreshWalk, func(s string) { e.rtMu.Lock(); e.walkErr = s; e.rtMu.Unlock() })
		step("basemap", e.mapDue, e.RefreshMap, func(s string) { e.rtMu.Lock(); e.mapErr = s; e.rtMu.Unlock() })
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (e *Engine) fileOlderThan(name string, d time.Duration) bool {
	fi, err := os.Stat(filepath.Join(e.cfg.Server.DataDir, name))
	return err != nil || e.now().Sub(fi.ModTime()) > d
}

func (e *Engine) walkDue() bool {
	return e.cfg.Data.WalkSource != "" && (e.Walker() == nil || e.fileOlderThan(walkFile, walkRefreshAfter))
}

// RefreshWalk downloads the OSM extract, builds the street network, saves it and starts using it.
func (e *Engine) RefreshWalk(ctx context.Context) error {
	g, err := e.buildWalk(ctx)
	if err != nil {
		return err
	}
	e.SetWalker(g) // after the big job's memory is released: this reloads the timetable
	return nil
}

func (e *Engine) buildWalk(ctx context.Context) (*walk.Graph, error) {
	select { // one big job at a time keeps the memory peak bounded
	case e.heavy <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	restore := frugalGC()
	defer func() {
		restore()
		<-e.heavy
		debug.FreeOSMemory() // hand back the build's scratch memory
	}()
	start := e.now()
	dir := e.cfg.Server.DataDir
	src := filepath.Join(dir, "walk-source.osm.pbf")
	defer os.Remove(src)
	e.log.Info("downloading the street map", "from", e.cfg.Data.WalkSource)
	if err := download(ctx, e.cfg.Data.WalkSource, src, maxWalkSource); err != nil {
		return nil, fmt.Errorf("downloading street map: %w", err)
	}
	g, err := walk.FromPBF(src)
	if err != nil {
		return nil, fmt.Errorf("building street network: %w", err)
	}
	if err := g.Save(filepath.Join(dir, walkFile)); err != nil {
		return nil, fmt.Errorf("saving street network: %w", err)
	}
	e.log.Info("street network ready", "nodes", g.Nodes(), "edges", g.Edges(),
		"took", e.now().Sub(start).Round(time.Second).String())
	return g, nil
}

func (e *Engine) mapDue() bool {
	return e.cfg.Data.PMTilesBin != "" && e.fileOlderThan(mapFile, mapRefreshAfter)
}

// RefreshMap cuts the Sydney basemap out of the Protomaps daily build with the pmtiles tool, which reads only
// the parts it needs (the build is over 100 GB).
func (e *Engine) RefreshMap(ctx context.Context) error {
	bin, err := exec.LookPath(e.cfg.Data.PMTilesBin)
	if err != nil {
		return fmt.Errorf("pmtiles tool not found (%q): the map stays blank", e.cfg.Data.PMTilesBin)
	}
	dir := e.cfg.Server.DataDir
	tmp := filepath.Join(dir, "map.pmtiles.partial")
	var last error
	// The newest build may not be published yet; fall back a day or two.
	for back := 1; back <= 3; back++ {
		day := e.now().UTC().AddDate(0, 0, -back).Format("20060102")
		_ = os.Remove(tmp)
		runCtx, cancel := context.WithTimeout(ctx, 45*time.Minute)
		e.log.Info("cutting the basemap", "build", day)
		cmd := exec.CommandContext(runCtx, bin, "extract", "https://build.protomaps.com/"+day+".pmtiles", tmp, "--bbox="+mapBBox, "--quiet")
		cmd.Env = []string{} // nothing from our environment (the API key) goes to the tool
		out, err := cmd.CombinedOutput()
		cancel()
		if err == nil {
			if fi, serr := os.Stat(tmp); serr == nil && fi.Size() > 1<<20 {
				if err := os.Rename(tmp, filepath.Join(dir, mapFile)); err != nil {
					return err
				}
				e.log.Info("basemap ready", "build", day, "bytes", fi.Size())
				return nil
			}
			err = errors.New("the tool produced no usable file")
		}
		last = fmt.Errorf("build %s: %w: %.300s", day, err, out)
		if ctx.Err() != nil {
			break
		}
	}
	_ = os.Remove(tmp)
	return last
}

// download fetches a URL (https only) to dest, refusing anything larger than maxBytes.
func download(ctx context.Context, url, dest string, maxBytes int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "gymrouter (self-hosted; https://github.com/benjamincottle/gymrouter)")
	c := &http.Client{Timeout: 20 * time.Minute}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > maxBytes {
		return fmt.Errorf("file is %d bytes, over the %d limit", resp.ContentLength, maxBytes)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	n, err := io.Copy(f, io.LimitReader(resp.Body, maxBytes+1))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n > maxBytes {
		err = fmt.Errorf("file is over the %d byte limit", maxBytes)
	}
	if err != nil {
		os.Remove(dest)
	}
	return err
}
