// Command wsi-postprocess runs Claude post-processing on the last speech-to-text
// transcription, for the panel menu's Post-Process Last Transcription items.
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/wsipost"
)

func main() {
	os.Exit(int(run(os.Args[1:])))
}

func run(args []string) term.ExitCode {
	env, err := wsipost.NewEnv()
	if err != nil {
		if _, werr := fmt.Fprintf(os.Stderr, "wsi-postprocess: %v\n", err); werr != nil {
			return term.ExitFailure
		}
		return term.ExitFailure
	}
	return wsipost.App{Env: env}.Run(context.Background(), args)
}
