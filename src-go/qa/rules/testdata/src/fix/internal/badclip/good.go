package badclip

import "fix/internal/execx"

// The copy a person is told about plainly, and the clear that runs only while the clipboard
// still holds the secret.
func copyPlain() execx.Cmd { return execx.Cmd{Name: "wl-copy", Args: nil} }

func read() execx.Cmd { return execx.Cmd{Name: "wl-paste", Args: []string{"--no-newline"}} }

func clear() execx.Cmd { return execx.Cmd{Name: "wl-copy", Args: []string{"--clear"}} }

func typed() execx.Cmd { return execx.Cmd{Name: "wl-copy", Args: []string{"--type", "text/plain"}} }

// Other programs take -o for other things.
func other() execx.Cmd { return execx.Cmd{Name: "curl", Args: []string{"-o", "out"}} }

// One paste at stop: the speech-to-text insert, which keeps no secret.
const words = "The transcript is typed in one paste at stop. The password is on the clipboard until you press Enter."

func xclipPlain() execx.Cmd {
	return execx.Cmd{Name: "xclip", Args: []string{"-selection", "clipboard", "-loops", "0"}}
}
