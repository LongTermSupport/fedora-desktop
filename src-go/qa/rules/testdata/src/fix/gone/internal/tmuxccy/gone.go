// Package tmuxccy owns tmux's answers: its one classifier may spell tmux's own wording.
package tmuxccy

import "strings"

// goneAnswer is the one classifier of tmux's no-server and no-session answers.
func goneAnswer(stderr string) bool {
	return strings.HasPrefix(stderr, "no server running on ") ||
		strings.HasPrefix(stderr, "error connecting to ") && strings.HasSuffix(stderr, " (No such file or directory)") ||
		strings.HasPrefix(stderr, "can't find session")
}

// List is the defect as it was: any "No such file or directory" read as an empty server,
// whichever program said it.
func List(stderr string) bool {
	return strings.Contains(stderr, "no server running") || strings.Contains(stderr, "No such file or directory") // want `tmux's answer "no server running"` `tmux's answer "No such file or directory"`
}

// Kill reads its answer through the classifier: clean.
func Kill(stderr string) bool { return goneAnswer(stderr) }
