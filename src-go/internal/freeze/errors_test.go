package freeze

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestDieIsAFormattedFatalError(t *testing.T) {
	err := Die("no %s", "thing")
	mustEq(t, "text", err.Error(), "no thing")
	mustEq(t, "msg", dieMsg(fmt.Errorf("wrapped: %w", err)), "no thing")
}

func TestExitCodeMapsErrorsToStatusAndPrintsTheToolLine(t *testing.T) {
	cases := []struct {
		what     string
		err      error
		wantCode term.ExitCode
		wantErr  string
	}{
		{"success", nil, term.ExitOK, ""},
		{"a fatal error is one line with the tool name", Die("this is a container"), term.ExitFailure, "testfreeze: this is a container\n"},
		{"a multi-line fatal keeps its lines", Die("a\n  b"), term.ExitFailure, "testfreeze: a\n  b\n"},
		{"failed targets were already reported", fmt.Errorf("batch: %w", ErrSomeFailed), term.ExitFailure, ""},
		{"an interrupt is a cancellation", fmt.Errorf("read: %w", context.Canceled), term.ExitCancelled, ""},
		{"any other error is a failure with the tool name", errors.New("boom"), term.ExitFailure, "testfreeze: boom\n"},
	}
	for _, c := range cases {
		t.Run(c.what, func(t *testing.T) {
			var buf bytes.Buffer
			mustEq(t, "code", ExitCode(&buf, "testfreeze", c.err), c.wantCode)
			mustEq(t, "stderr", buf.String(), c.wantErr)
		})
	}
}
