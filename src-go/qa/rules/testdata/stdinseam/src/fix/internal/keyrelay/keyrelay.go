// Package keyrelay is the fixture's relay seam: it copies the terminal to a child until its owner
// stops it, reading only once poll finds a key.
package keyrelay

import "io"

// Copy relays stdin to w until stop says to end.
func Copy(w io.Writer, stdin io.Reader, ready func() bool) error {
	buf := make([]byte, 64)
	for ready() {
		n, err := stdin.Read(buf)
		if _, werr := w.Write(buf[:n]); werr != nil {
			return werr
		}
		if err != nil {
			return err
		}
	}
	return nil
}
