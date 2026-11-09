// Package badhost reads a machine's hostname, or an address's name, where teamchat must not: each
// is a way for the name of a machine to end up in a handle's site.
package badhost

import (
	"context"
	"net"
	"os"
)

// Called is the plain read of the machine's hostname.
func Called() (string, error) {
	return os.Hostname() // want `os.Hostname read outside teamchat/declare/machinehost.go`
}

// AsValue takes the function without calling it, which a caller then calls.
func AsValue() func() (string, error) {
	return os.Hostname // want `os.Hostname read outside teamchat/declare/machinehost.go`
}

// Stored keeps it in a package-level seam.
var Stored = os.Hostname // want `os.Hostname read outside teamchat/declare/machinehost.go`

// Reverse is the reverse lookup of an address.
func Reverse(addr string) ([]string, error) {
	return net.LookupAddr(addr) // want `net.LookupAddr read outside teamchat/declare/machinehost.go`
}

// ReverseDefault is the reverse lookup through the default resolver.
func ReverseDefault(addr string) ([]string, error) {
	return net.DefaultResolver.LookupAddr(context.TODO(), addr) // want `net.LookupAddr read outside teamchat/declare/machinehost.go`
}

// ReverseOwn is the reverse lookup through a resolver of its own.
func ReverseOwn(addr string) ([]string, error) {
	r := &net.Resolver{PreferGo: true}
	return r.LookupAddr(context.TODO(), addr) // want `net.LookupAddr read outside teamchat/declare/machinehost.go`
}
