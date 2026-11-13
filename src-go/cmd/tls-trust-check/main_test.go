package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestRunReadsTheArgumentsAndTheEnvironment(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(nil, &out, &errOut); code != term.ExitUsage || out.Len() != 0 {
		t.Fatalf("no arguments: exit %d stdout %q", code, out.String())
	}
	errOut.Reset()
	t.Setenv("SSL_CERT_FILE", "")
	if code := run([]string{"--url", "https://127.0.0.1:1"}, &out, &errOut); code != term.ExitFailure ||
		!strings.Contains(errOut.String(), "SSL_CERT_FILE is not set") {
		t.Fatalf("no SSL_CERT_FILE: exit %d stderr %q", code, errOut.String())
	}
}
