// Package par runs independent pieces of work on all the cores.
package par

import (
	"fmt"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
)

// Do calls f(0) … f(n-1) on up to GOMAXPROCS goroutines and waits for them all. The calls must not depend on
// each other; each one writes its own result (e.g. into slot i of a slice).
//
// A panic on a worker would stop the server (nothing recovers on those goroutines), and a worker that quit would
// leave work undone. So each call recovers, the workers carry on, and the first panic is raised again on the
// caller's goroutine, where the request or background guard that called Do deals with it.
func Do(n int, f func(i int)) {
	workers := min(n, runtime.GOMAXPROCS(0))
	if workers <= 1 {
		for i := range n {
			f(i)
		}
		return
	}
	var next atomic.Int64
	var wg sync.WaitGroup
	var once sync.Once
	var failed any
	for range workers {
		wg.Go(func() {
			for {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				func() {
					defer func() {
						if v := recover(); v != nil {
							once.Do(func() { failed = fmt.Sprintf("%v\n%s", v, debug.Stack()) })
						}
					}()
					f(i)
				}()
			}
		})
	}
	wg.Wait()
	if failed != nil {
		panic(failed)
	}
}
