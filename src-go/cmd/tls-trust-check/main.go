// Command tls-trust-check handshakes with one https url and verifies it against exactly the
// roots SSL_CERT_FILE and SSL_CERT_DIR name, as a rustls client given them does. The flow is
// internal/tlstrust.
package main

import (
	"context"
	"io"
	"os"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tlstrust"
)

func main() {
	os.Exit(int(run(os.Args[1:], os.Stdout, os.Stderr)))
}

func run(args []string, stdout, stderr io.Writer) term.ExitCode {
	return tlstrust.App{
		Args: args, Getenv: os.Getenv, Now: time.Now, Stdout: stdout, Stderr: stderr,
	}.Run(context.Background())
}
