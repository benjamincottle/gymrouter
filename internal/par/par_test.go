package par

import (
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

func TestDoCallsEachIndexOnce(t *testing.T) {
	for _, n := range []int{0, 1, 7, 1000} {
		got := make([]int32, n)
		Do(n, func(i int) { atomic.AddInt32(&got[i], 1) })
		for i, c := range got {
			if c != 1 {
				t.Fatalf("n=%d: f(%d) called %d times", n, i, c)
			}
		}
	}
}

func TestDoRaisesAWorkersPanicOnTheCaller(t *testing.T) {
	if runtime.GOMAXPROCS(0) < 2 {
		t.Skip("needs more than one core to use workers")
	}
	var done atomic.Int32
	defer func() {
		v := recover()
		if v == nil || !strings.Contains(v.(string), "bad stop") {
			t.Fatalf("recovered %v; want the worker's panic", v)
		}
		if done.Load() != 99 {
			t.Errorf("%d other calls finished; the rest of the work should still run", done.Load())
		}
	}()
	Do(100, func(i int) {
		if i == 42 {
			panic("bad stop")
		}
		done.Add(1)
	})
}
