// Command ftp-camera runs the camera FTP session: vsftpd for a Sony camera's uploads, the
// EXIF-date sort, the push or copy to the rclone remote, and the orphan-RAW prune.
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ftpcamera"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

// run is one invocation: INT and TERM cancel its context, which ends a session through its
// teardown and a waiting copy with 130.
func run(args []string) term.ExitCode {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	app := ftpcamera.App{Env: ftpcamera.NewEnv()}
	return app.Run(ctx, args)
}
