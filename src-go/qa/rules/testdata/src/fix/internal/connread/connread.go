// Package connread is the fixture of project_test_conn_read: its tests read connections and
// listeners with and without a bound.
package connread

import (
	"io"
	"net"
)

// Echo copies what c sends back to it. A read outside a test file is not judged.
func Echo(c net.Conn) error {
	_, err := io.Copy(c, c)
	return err
}
