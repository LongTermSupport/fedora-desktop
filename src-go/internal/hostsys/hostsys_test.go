package hostsys

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

func newSys() (*Sys, *execx.Fake) {
	f := &execx.Fake{}
	return &Sys{Runner: f, Chatter: &strings.Builder{}}, f
}

func TestMissingListsThePackagesRpmDoesNotKnowInOrder(t *testing.T) {
	s, f := newSys()
	f.On([]string{"rpm", "-q", "--quiet"}, execx.Result{ExitCode: 1})
	f.On([]string{"rpm", "-q", "--quiet", "python3"}, execx.Result{})
	f.On([]string{"rpm", "-q", "--quiet", "jq"}, execx.Result{})
	got, err := s.Missing(context.Background(), []string{"python3", "firewalld", "jq", "zstd"})
	if err != nil || !slices.Equal(got, []string{"firewalld", "zstd"}) {
		t.Fatalf("%v, %v", got, err)
	}
}

func TestMissingFailsWhenRpmCannotRun(t *testing.T) {
	s, f := newSys()
	f.OnError([]string{"rpm"}, &execx.NotInstalledError{Name: "rpm"})
	if _, err := s.Missing(context.Background(), []string{"jq"}); !execx.IsNotInstalled(err) {
		t.Fatalf("got %v", err)
	}
}

func TestInstallRunsOneDnfForAllAndSendsItsOutputAside(t *testing.T) {
	s, f := newSys()
	f.On([]string{"dnf"}, execx.Result{Stdout: []byte("Complete!\n")})
	if err := s.Install(context.Background(), []string{"firewalld", "zstd"}); err != nil {
		t.Fatal(err)
	}
	if got := f.Calls()[0].Line(); got != "dnf install -y firewalld zstd" {
		t.Fatalf("ran %q", got)
	}
	if !strings.Contains(fmt.Sprint(s.Chatter), "Complete!") {
		t.Fatal("dnf output not sent to the chatter writer")
	}
	f.On([]string{"dnf"}, execx.Result{ExitCode: 1})
	if err := s.Install(context.Background(), []string{"zstd"}); err == nil || !strings.Contains(err.Error(), "dnf install -y zstd") {
		t.Fatalf("got %v", err)
	}
}

func TestUserExistsReadsGetentsExitStatus(t *testing.T) {
	s, f := newSys()
	f.On([]string{"getent", "passwd", "here"}, execx.Result{Stdout: []byte("here:x:990:990::/:/sbin/nologin\n")})
	f.On([]string{"getent", "passwd", "gone"}, execx.Result{ExitCode: 2})
	f.On([]string{"getent", "passwd", "broken"}, execx.Result{ExitCode: 1})
	for name, want := range map[string]bool{"here": true, "gone": false} {
		got, err := s.UserExists(context.Background(), name)
		if err != nil || got != want {
			t.Errorf("UserExists(%s) = %v, %v", name, got, err)
		}
	}
	if _, err := s.UserExists(context.Background(), "broken"); err == nil {
		t.Fatal("an unexpected getent status was taken for an answer")
	}
}

func TestAddServiceUserRunsUseraddWithTheServiceAccountFlags(t *testing.T) {
	s, f := newSys()
	f.On([]string{"useradd"}, execx.Result{})
	if err := s.AddServiceUser(context.Background(), "svc", "/var/lib/svc", "A service"); err != nil {
		t.Fatal(err)
	}
	call := f.Calls()[0]
	want := []string{
		"--system", "--user-group", "--no-create-home", "--home-dir", "/var/lib/svc",
		"--shell", "/usr/sbin/nologin", "--comment", "A service", "svc",
	}
	if call.Name != "useradd" || !slices.Equal(call.Args, want) {
		t.Fatalf("ran %v", call.Args)
	}
	f.On([]string{"useradd"}, execx.Result{ExitCode: 9})
	if err := s.AddServiceUser(context.Background(), "svc", "/var/lib/svc", "A service"); err == nil {
		t.Fatal("a failed useradd returned no error")
	}
}

func TestMachineIsUnameMTrimmed(t *testing.T) {
	s, f := newSys()
	f.On([]string{"uname", "-m"}, execx.Result{Stdout: []byte("x86_64\n")})
	if got, err := s.Machine(context.Background()); err != nil || got != "x86_64" {
		t.Fatalf("%q, %v", got, err)
	}
	f.On([]string{"uname"}, execx.Result{ExitCode: 1})
	if _, err := s.Machine(context.Background()); err == nil {
		t.Fatal("a failed uname returned no error")
	}
}

func TestDecompressRunsZstdOverwritingTheOutput(t *testing.T) {
	s, f := newSys()
	f.On([]string{"zstd"}, execx.Result{})
	if err := s.Decompress(context.Background(), "/w/a.zst", "/w/a"); err != nil {
		t.Fatal(err)
	}
	if got := f.Calls()[0].Line(); got != "zstd -q -d -f -o /w/a -- /w/a.zst" {
		t.Fatalf("ran %q", got)
	}
	f.On([]string{"zstd"}, execx.Result{ExitCode: 1, Stderr: []byte("not zstd")})
	if err := s.Decompress(context.Background(), "/w/a.zst", "/w/a"); err == nil || !strings.Contains(err.Error(), "zstd") {
		t.Fatalf("got %v", err)
	}
}
