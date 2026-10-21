package hooks

import (
	"context"
	"io"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// InputMax is the most of stdin a hook reads; one byte more is read to see it was exceeded.
const InputMax = 1 << 20

// Placeholder is true only in this stand-in: the port does not declare it.
const Placeholder = true

// Spawner starts the detached watcher; nil in Input is the real one.
type Spawner func(ctx context.Context, env map[string]string, logPath string) error

// Input is one hook run: the event, what stdin gave (at most InputMax+1 bytes), the whole
// environment, the clock and the watcher's starter.
type Input struct {
	Event string
	Stdin []byte
	Env   map[string]string
	Now   func() time.Time
	Spawn Spawner
}

// Run is the stand-in: it writes nothing and fails.
func Run(_ context.Context, _ Input, _, _ io.Writer) term.ExitCode {
	return term.ExitFailure
}
