package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// keepAliveEvery is how often a quiet event stream sends a comment, so nothing on the way closes it as idle.
const keepAliveEvery = 20 * time.Second

// events streams one event each time new live data is applied (server-sent events; see docs/api.md), so the app
// plans again at once instead of on a timer. It runs until the client goes or the server shuts down (StopStreams).
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	rc := http.NewResponseController(w)
	// The server's read and write timeouts are for ordinary requests; this one stays open.
	if err := rc.SetWriteDeadline(time.Time{}); err != nil {
		writeError(w, http.StatusInternalServerError, "streaming isn't available")
		return
	}
	_ = rc.SetReadDeadline(time.Time{})
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	if rc.Flush() != nil {
		return
	}
	tick := time.NewTicker(keepAliveEvery)
	defer tick.Stop()
	changed := s.eng.LiveChanged()
	for {
		var err error
		select {
		case <-r.Context().Done():
			return
		case <-s.closing:
			return
		case <-tick.C:
			_, err = io.WriteString(w, ": keep-alive\n\n")
		case <-changed:
			changed = s.eng.LiveChanged()
			var ev struct {
				RealtimeAt *time.Time `json:"realtime_at,omitempty"`
			}
			if snap, e := s.eng.SnapshotFor(s.eng.Now()); e == nil && snap.Realtime {
				at := snap.RealtimeAt
				ev.RealtimeAt = &at
			}
			b, _ := json.Marshal(ev)
			_, err = fmt.Fprintf(w, "event: realtime\ndata: %s\n\n", b)
		}
		if err != nil || rc.Flush() != nil {
			return
		}
	}
}

// StopStreams ends the open event streams, so the server can shut down (register it with
// http.Server.RegisterOnShutdown). Safe to call more than once.
func (s *Server) StopStreams() {
	s.closeOnce.Do(func() { close(s.closing) })
}
