// Command ccy-network-policy holds a ccy session's network policy (internal/netpolicy, Plan
// 00232): `launch` runs the session's container under it (the bash launcher calls it),
// `refresh` updates every live session's allow sets (ccy --connect and --disconnect), and
// `where` prints the checkout's policy file on the host.
package main

import (
	"context"
	"net"
	"net/netip"
	"os"
	"os/exec"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/netpolicy"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	dir, err := os.Getwd()
	if err != nil {
		var p term.Printer
		p.Print(os.Stderr, "ccy-network-policy: the working directory: "+err.Error()+"\n")
		return term.ExitFailure
	}
	app := netpolicy.App{
		Args: args, Getenv: os.Getenv, Runner: execx.OS{}, Fg: execx.OS{}, LookPath: exec.LookPath,
		Ifaces: netpolicy.SystemIfaces,
		Resolve: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
		},
		HostsFile: "/etc/hosts", Dir: dir, Stdout: os.Stdout, Stderr: os.Stderr,
	}
	return app.Run(context.Background())
}
