package tmuxccy

// A test spells out the argv it expects, so it may use any target form.
func testBare(t *Tmux, n string) {
	t.Run("send-keys", "-t", "="+n)
}
