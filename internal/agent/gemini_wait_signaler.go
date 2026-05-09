package agent

// GeminiWaitSignaler is a stub for future Gemini-specific wait-signal support.
// It never signals; the silence timeout remains the sole detection mechanism.
type GeminiWaitSignaler struct {
	ch chan struct{}
}

func NewGeminiWaitSignaler(_ string) WaitSignaler {
	return &GeminiWaitSignaler{ch: make(chan struct{})}
}

func (g *GeminiWaitSignaler) Chan() <-chan struct{} { return g.ch }
func (g *GeminiWaitSignaler) Close()               {}
