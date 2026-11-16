// Package badrelay is a relay of the terminal outside the relay seam: still a read of stdin.
package badrelay

import "io"

// Relay copies stdin to w, a key at a time, until a read fails.
func Relay(w io.Writer, stdin io.Reader) error {
	buf := make([]byte, 64)
	for {
		n, err := stdin.Read(buf) // want `Read reads stdin outside internal/stdinread`
		if _, werr := w.Write(buf[:n]); werr != nil {
			return werr
		}
		if err != nil {
			return err
		}
	}
}
