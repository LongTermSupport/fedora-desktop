package badhost

import (
	"context"
	"net"
	"os"
)

// Resolver has a method of the same name as the reverse lookup; it is not net's.
type Resolver struct{}

// LookupAddr is a lookup of the package's own.
func (Resolver) LookupAddr(string) []string { return nil }

// Hostname is a name of the package's own.
func Hostname() string { return "member-machine-1" }

// Clean reads nothing that names a machine: the environment, a forward lookup, a parse, a
// name of the package's own.
func Clean(ctx context.Context) (string, error) {
	_ = os.Getenv("HOME")
	_ = os.Getpid()
	_ = net.ParseIP("192.0.2.7")
	if _, err := net.DefaultResolver.LookupHost(ctx, "host-a.example.net"); err != nil {
		return "", err
	}
	_ = Resolver{}.LookupAddr("192.0.2.7")
	_ = os.Getenv("HOSTNAMES")
	_ = "HOOKS_DAEMON_HOSTNAME=box\n" // a file's content, which a test writes to show it is ignored
	_ = "/etc/hosts"
	_ = "the hostname"
	return Hostname(), nil
}
