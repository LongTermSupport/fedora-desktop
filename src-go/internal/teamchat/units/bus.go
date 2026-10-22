package units

import (
	"context"
	"fmt"

	"github.com/godbus/dbus/v5"
)

// SystemBus dials the system bus (DBUS_SYSTEM_BUS_ADDRESS when set, else the system socket),
// authenticated and with its Hello sent.
func SystemBus() (Conn, error) {
	c, err := dbus.ConnectSystemBus()
	if err != nil {
		return nil, fmt.Errorf("connect to the system bus: %w", err)
	}
	return busConn{c}, nil
}

// busConn is a *dbus.Conn as a Conn.
type busConn struct{ c *dbus.Conn }

func (b busConn) Call(ctx context.Context, dest string, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	call := b.c.Object(dest, path).CallWithContext(ctx, method, 0, args...)
	if call.Err != nil {
		return nil, call.Err
	}
	return call.Body, nil
}

func (b busConn) Signal(ch chan<- *dbus.Signal) { b.c.Signal(ch) }

// Close closes the connection; one godbus already closed when the bus went away has nothing
// left to close.
func (b busConn) Close() error {
	if !b.c.Connected() {
		return nil
	}
	return b.c.Close()
}
