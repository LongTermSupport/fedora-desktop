package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunAnswersHelpAndUsage(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(t.Context(), []string{"--help"}, &out, &errOut); code != term.ExitOK || !strings.Contains(out.String(), "teamchat-loadgen setup") {
		t.Fatalf("--help: exit %d stdout %q stderr %q", code, out.String(), errOut.String())
	}
	out.Reset()
	if code := run(t.Context(), nil, &out, &errOut); code != term.ExitUsage || out.Len() != 0 {
		t.Fatalf("no verb: exit %d stdout %q", code, out.String())
	}
}
