// Package fakeallowed is a fake whose accept loop the test's allowance excuses: that one function
// is not judged, and every other read in the package still is.
package fakeallowed

import "net"

// Box serves the connections its listener accepts.
type Box struct{ l net.Listener }

// Start serves l until it is closed.
func Start(l net.Listener) *Box {
	b := &Box{l: l}
	go b.accept()
	return b
}

// Other is a second unbounded read in the same package: not excused.
func (b *Box) Other(c net.Conn) error {
	_, err := c.Read(make([]byte, 1)) // want `unbounded connection read`
	return err
}

// accept is the excused accept loop: it ends when Close closes the listener.
func (b *Box) accept() {
	for {
		c, err := b.l.Accept()
		if err != nil {
			return
		}
		if err := c.Close(); err != nil {
			return
		}
	}
}

// Accept is a function of the excused method's name on no receiver: not excused.
func Accept(l net.Listener) error {
	c, err := l.Accept() // want `unbounded connection read`
	if err != nil {
		return err
	}
	return c.Close()
}
