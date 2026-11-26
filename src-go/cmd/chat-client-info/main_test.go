package main

import (
	"bytes"
	"errors"
	"os/user"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/chatclientinfo"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/matrixclients"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func someone() (*user.User, error) { return &user.User{Uid: "1000", HomeDir: "/home/someone"}, nil }

func TestAnAccountThatCannotBeReadFails(t *testing.T) {
	for name, account := range map[string]func() (*user.User, error){
		"no entry": func() (*user.User, error) { return nil, errors.New("no passwd entry") },
		"bad uid":  func() (*user.User, error) { return &user.User{Uid: "x", HomeDir: "/h"}, nil },
	} {
		fake := &execx.Fake{}
		var out, errOut bytes.Buffer
		if code := run(fake, account, nil, &out, &errOut); code != term.ExitFailure {
			t.Errorf("%s: exit %d", name, code)
		}
		if len(fake.Calls()) != 0 || !strings.HasPrefix(errOut.String(), "chat-client-info: ") {
			t.Errorf("%s: calls %v, stderr %q", name, fake.Calls(), errOut.String())
		}
	}
}

func TestAnArgumentIsRefusedBeforeAnythingRuns(t *testing.T) {
	fake := &execx.Fake{}
	var out, errOut bytes.Buffer
	if code := run(fake, someone, []string{"--help"}, &out, &errOut); code != term.ExitUsage {
		t.Fatalf("exit %d", code)
	}
	if len(fake.Calls()) != 0 || out.Len() != 0 {
		t.Fatalf("calls %v, stdout %q", fake.Calls(), out.String())
	}
}

func TestRunReadsTheEmbeddedClients(t *testing.T) {
	fake := &execx.Fake{}
	fake.On([]string{"/usr/bin/flatpak"}, execx.Result{})
	var out, errOut bytes.Buffer
	if code := run(fake, someone, nil, &out, &errOut); code != term.ExitOK {
		t.Fatalf("exit %d, stderr %q", code, errOut.String())
	}
	list, err := matrixclients.Load()
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, argv := range chatclientinfo.Table(list.Clients) {
		want = append(want, strings.Join(argv, " "))
	}
	var got []string
	for _, c := range fake.Calls() {
		got = append(got, c.Line())
	}
	if !slices.Equal(got, want) {
		t.Fatalf("calls %q, want the table %q", got, want)
	}
}
