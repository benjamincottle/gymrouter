package engine

import "context"

// ProvisionLoop runs the provisioning loop (tests start it themselves: enginetest only initialises the engine).
func (e *Engine) ProvisionLoop(ctx context.Context) { e.provisionLoop(ctx) }

// Area is the area the app covers.
var Area = area

// WalkDue reports whether the street network is due to be fetched and built.
func (e *Engine) WalkDue() bool { return e.walkDue() }

// HeavyFree reports whether no whole-network job (a line search, the stop catalogue, building the street network)
// is running.
func (e *Engine) HeavyFree() bool {
	select {
	case e.heavy <- struct{}{}:
		<-e.heavy
		return true
	default:
		return false
	}
}
