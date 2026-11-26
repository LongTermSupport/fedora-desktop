// Package other is outside the engine: not judged.
package other

type held struct{ count int }

// Engine is another package's engine.
type Engine struct{ h *held }

func (e *Engine) change(f func(h *held) bool) bool { return f(e.h) }

func (e *Engine) bump() bool {
	return e.change(func(h *held) bool {
		h.count++
		return true
	})
}
