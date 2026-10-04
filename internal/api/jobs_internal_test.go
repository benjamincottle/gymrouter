package api

import (
	"testing"
	"time"
)

func TestJobStoreRunsOneAtATimeAndForgets(t *testing.T) {
	var js jobStore
	now := time.Now()
	id, j, ok := js.start(now)
	if !ok || id == "" {
		t.Fatal("first job should start")
	}
	if _, _, ok := js.start(now); ok {
		t.Error("a second job started while one runs")
	}
	js.update(j, func(j *suggestJob) { j.State, j.ended = "done", now })
	if got, ok := js.get(id); !ok || got.State != "done" {
		t.Errorf("finished job: %+v %v", got, ok)
	}
	if _, _, ok := js.start(now.Add(time.Minute)); !ok {
		t.Error("a new job should start once the last has finished")
	}
	if _, ok := js.get(id); !ok {
		t.Error("a recent job was forgotten")
	}
	js.mu.Lock()
	for _, j := range js.jobs {
		j.State = "done" // let the next start run
	}
	js.mu.Unlock()
	if _, _, ok := js.start(now.Add(jobKeep + time.Hour)); !ok {
		t.Fatal("start")
	}
	if _, ok := js.get(id); ok {
		t.Error("an old job was kept")
	}
}
