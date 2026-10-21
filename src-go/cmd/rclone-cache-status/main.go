// Command rclone-cache-status shows the VFS cache use of the rclone mounts, and how much of one
// directory inside a mount is cached.
package main

import (
	"context"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/rclonecache"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	return rclonecache.App{Env: rclonecache.NewEnv()}.Run(context.Background(), args)
}
