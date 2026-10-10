package engine

import "context"

// ProvisionLoop runs the provisioning loop (tests start it themselves: enginetest only initialises the engine).
func (e *Engine) ProvisionLoop(ctx context.Context) { e.provisionLoop(ctx) }
