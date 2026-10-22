package units

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/godbus/dbus/v5"
)

// SystemBusAddress is the system bus root dials. It is never DBUS_SYSTEM_BUS_ADDRESS: root
// decides from what systemd reports, so the bus is not the caller's to name.
const SystemBusAddress = "unix:path=/run/dbus/system_bus_socket"

// SystemBus dials SystemBusAddress, authenticated by EXTERNAL and with its Hello sent; a context
// that ends first ends the dial.
func SystemBus(ctx context.Context) (Conn, error) { return dialBus(ctx, SystemBusAddress) }

// dialBus dials address in a goroutine raced against ctx. The connection lives on its own
// context, which an abandoned dial cancels, so godbus closes it whenever the dial returns.
func dialBus(ctx context.Context, address string) (Conn, error) {
	life, cancel := context.WithCancel(context.WithoutCancel(ctx))
	type dialled struct {
		c   *dbus.Conn
		err error
	}
	done := make(chan dialled, 1)
	go func() {
		c, err := connect(life, address)
		done <- dialled{c, err}
	}()
	select {
	case d := <-done:
		if d.err != nil {
			cancel()
			return nil, fmt.Errorf("connect to the system bus at %s: %w", address, d.err)
		}
		return busConn{d.c, cancel}, nil
	case <-ctx.Done():
		cancel()
		return nil, fmt.Errorf("connect to the system bus at %s: %w", address, ctx.Err())
	}
}

func connect(life context.Context, address string) (*dbus.Conn, error) {
	c, err := dbus.Dial(address, dbus.WithContext(life))
	if err != nil {
		return nil, err
	}
	if err = c.Auth([]dbus.Auth{dbus.AuthExternal(strconv.Itoa(os.Geteuid()))}); err == nil {
		err = c.Hello()
	}
	if err != nil {
		return nil, errors.Join(err, closeConn(c))
	}
	return c, nil
}

// busConn is a *dbus.Conn as a Conn; cancel ends the context it lives on.
type busConn struct {
	c      *dbus.Conn
	cancel context.CancelFunc
}

func (b busConn) Call(ctx context.Context, dest string, path dbus.ObjectPath, method string, args ...any) ([]any, error) {
	call := b.c.Object(dest, path).CallWithContext(ctx, method, 0, args...)
	if call.Err != nil {
		return nil, call.Err
	}
	return call.Body, nil
}

func (b busConn) Signal(ch chan<- *dbus.Signal) { b.c.Signal(ch) }

// Close closes the connection.
func (b busConn) Close() error {
	defer b.cancel()
	return closeConn(b.c)
}

// closeConn closes c; one godbus already closed when the bus went away has nothing left to close.
func closeConn(c *dbus.Conn) error {
	if !c.Connected() {
		return nil
	}
	return c.Close()
}
