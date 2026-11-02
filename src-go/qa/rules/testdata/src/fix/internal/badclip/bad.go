// Package badclip keeps a secret short-lived by a clipboard that is meant to erase itself.
package badclip

import "fix/internal/execx"

// copyOnce is ccy-teams' original copy: GNOME read the selection at once and went on serving
// it, so the password stayed pasteable while the person was told it was gone.
func copyOnce() execx.Cmd {
	return execx.Cmd{Name: "wl-copy", Args: []string{"--paste-once"}} // want `self-erasing clipboard: wl-copy --paste-once` `self-erasing clipboard: "paste-once"`
}

func short() execx.Cmd {
	return execx.Cmd{Name: "wl-copy", Args: []string{"-o"}} // want `self-erasing clipboard`
}

func joined() execx.Cmd {
	return execx.Cmd{Name: "/usr/bin/wl-copy", Args: []string{"-no"}} // want `self-erasing clipboard`
}

func xclipOnce() execx.Cmd {
	return execx.Cmd{Name: "xclip", Args: []string{"-selection", "clipboard", "-loops", "1"}} // want `self-erasing clipboard`
}

const told = "The password is on the clipboard for one paste." // want `self-erasing clipboard`

func tell() []string {
	return []string{
		"after that it is gone from the clipboard", // want `self-erasing clipboard`
		"it would use up the one paste",            // want `self-erasing clipboard`
		"it serves a single paste",                 // want `self-erasing clipboard`
		"Gone after the first paste.",              // want `self-erasing clipboard`
	}
}

// offer serves one paste only, so nothing clears the clipboard later. // want `self-erasing clipboard`
func offer() string { return told }

// wrapped is told that the password is served to one // want `self-erasing clipboard: "to one paste" in a comment`
// paste, a claim wrapped over two lines.
func wrapped() {}
