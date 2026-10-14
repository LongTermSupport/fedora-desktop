// Package tmuxccy owns tmux: it may run it, and its -t targets are judged.
package tmuxccy

// Tmux stands for the owner's runner.
type Tmux struct{}

// Run stands for the runner call.
func (t *Tmux) Run(args ...string) {}

// paneTarget is the target of a pane command on a session.
func paneTarget(name string) string { return "=" + name + ":" }

func goodPane(t *Tmux, n string) {
	t.Run("send-keys", "-t", paneTarget(n), "Enter")
}

func goodPaneAppend(t *Tmux, n string) {
	args := []string{"capture-pane", "-p"}
	t.Run(append(args, "-t", paneTarget(n))...)
}

func goodSession(t *Tmux, n string) {
	t.Run("kill-session", "-t", "="+n)
}

func goodSessionLiteral(n string) []string {
	return []string{"attach-session", "-t", "=" + n}
}

func badPaneBare(t *Tmux, n string) {
	t.Run("send-keys", "-t", "="+n) // want `a pane command needs the target paneTarget`
}

func badPanePlain(t *Tmux, n string) {
	t.Run("capture-pane", "-t", n) // want `a pane command needs the target paneTarget`
}

func badSessionColon(t *Tmux, n string) {
	t.Run("kill-session", "-t", paneTarget(n)) // want `a session command needs the target`
}

func badSessionColonLiteral(t *Tmux, n string) {
	t.Run("list-clients", "-t", "="+n+":") // want `a session command needs the target`
}

func badSessionPlain(t *Tmux, n string) {
	t.Run("has-session", "-t", n) // want `a session command needs the target`
}

func mixed(t *Tmux, n string) {
	t.Run("kill-session", "-t", "="+n)      // want `builds both pane and session`
	t.Run("send-keys", "-t", paneTarget(n)) // want `builds both pane and session`
}

func unclassified(t *Tmux, n string) {
	t.Run("list-windows", "-t", n) // want `in neither the paneCommands nor the sessionCommands table`
}
