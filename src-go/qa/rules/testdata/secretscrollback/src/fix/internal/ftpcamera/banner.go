// Package ftpcamera is a second form: a same-package printer wrapper and a password kept in a
// session field, printed in a banner that stays on screen.
package ftpcamera

import (
	"fmt"
	"io"
)

type stream struct{ w io.Writer }

// f is the package's own printer: its arguments are a terminal write by the derived fact.
func (s stream) f(format string, args ...any) { // want f:"writes parameters 0 1"
	if _, err := fmt.Fprintf(s.w, format, args...); err != nil {
		panic(err)
	}
}

type cli struct{ out stream }

// hotspotPassword is a secret source by the rule's table.
func (c *cli) hotspotPassword(conn string) (string, error) {
	if conn == "" {
		return "", fmt.Errorf("no hotspot")
	}
	return "three-word-psk", nil
}

type session struct {
	c        *cli
	wifiPass string // want wifiPass:"holds a secret from ftpcamera.cli.hotspotPassword"
}

func (s *session) prepare() error {
	pass, err := s.c.hotspotPassword("hotspot")
	s.wifiPass = pass
	return err
}

func (s *session) banner() {
	s.c.out.f("    Wi-Fi pass:  %s\n", s.wifiPass) // want `secret from ftpcamera.cli.hotspotPassword written to the terminal through ftpcamera.stream.f`
	s.c.out.f("    Password:    run 'ftp-camera --pass' to view\n")
}
