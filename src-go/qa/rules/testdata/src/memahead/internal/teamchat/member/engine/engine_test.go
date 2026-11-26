package engine

// A test may set memory in a change function to arrange a case: not flagged.
func arrange(e *Engine) error {
	return e.change("t", func(h *held, _ *Store) (bool, error) {
		h.count = 9
		return true, nil
	})
}
