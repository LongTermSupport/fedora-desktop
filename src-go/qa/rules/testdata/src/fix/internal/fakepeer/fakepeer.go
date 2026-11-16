// Package fakepeer is a non-test helper only tests run: its reads are judged.
package fakepeer

import "net"

// Serve answers one line on each connection ln accepts.
func Serve(ln *net.UnixListener) error {
	c, err := ln.AcceptUnix() // want `unbounded connection read`
	if err != nil {
		return err
	}
	_, err = c.Read(make([]byte, 1)) // want `unbounded connection read`
	return err
}
