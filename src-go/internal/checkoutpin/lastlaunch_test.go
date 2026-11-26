package checkoutpin

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const savedChoices = "# CCY Launch Configuration\nSAVED_CONFIG_VERSION=2\nLAST_TOKEN=\"work\"\n" +
	"LAST_SSH_KEYS=\"/keys/id_a\"\nLAST_NETWORK=\"proj_net\"\nLAST_TEAMS=\"\"\n"

// L1: the launcher's own write is pinned as it is written, so a launch nobody attends takes
// it; a container's change to it afterwards is a refusal naming the file and the verb.
func TestWriteLastLaunchPinsWhatItWrites(t *testing.T) {
	s, dir := store(t), checkout(t)
	if err := os.MkdirAll(filepath.Join(dir, ".claude/ccy"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteLastLaunch(s, dir, []byte(savedChoices), testAt); err != nil {
		t.Fatalf("WriteLastLaunch: %v", err)
	}
	content, digest, present, err := LastLaunchDigest(dir)
	if err != nil || !present || string(content) != savedChoices {
		t.Fatalf("LastLaunchDigest = %q %v %v", content, present, err)
	}
	if info, err := os.Stat(filepath.Join(dir, LastLaunch)); err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("the file is not private: %v %v", info, err)
	}
	if err := s.Verify(dir, KindLastLaunch, digest); err != nil {
		t.Fatalf("the launcher's own write is not pinned: %v", err)
	}
	write(t, dir, LastLaunch, strings.Replace(savedChoices, "/keys/id_a", "/keys/other", 1))
	_, changed, _, err := LastLaunchDigest(dir)
	if err != nil {
		t.Fatal(err)
	}
	err = s.Verify(dir, KindLastLaunch, changed)
	var refused *RefusalError
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), dir+"/"+LastLaunch) ||
		!strings.Contains(err.Error(), "ccy-teams approve last-launch "+dir) {
		t.Fatalf("a changed file: %v", err)
	}
}

// L2: the write refuses text the reader would refuse, and never follows a link a container put
// in the file's place or the directory's.
func TestWriteLastLaunchRefusesBadTextAndLinks(t *testing.T) {
	s, dir := store(t), checkout(t)
	if err := os.MkdirAll(filepath.Join(dir, ".claude/ccy"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := WriteLastLaunch(s, dir, []byte("LAST_TOKEN=\"$(id)\"\n"), testAt); err == nil {
		t.Fatal("a value bash would expand was written")
	}
	outside := checkout(t)
	if err := os.Symlink(filepath.Join(outside, "f"), filepath.Join(dir, LastLaunch)); err != nil {
		t.Fatal(err)
	}
	if err := WriteLastLaunch(s, dir, []byte(savedChoices), testAt); err != nil {
		t.Fatalf("a link in the file's place is replaced, not followed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "f")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the write followed the link: %v", err)
	}
	other := checkout(t)
	if err := os.MkdirAll(filepath.Join(other, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(other, ".claude/ccy")); err != nil {
		t.Fatal(err)
	}
	if err := WriteLastLaunch(s, other, []byte(savedChoices), testAt); err == nil {
		t.Fatal("a write through a linked .claude/ccy left the checkout")
	}
	if entries, err := os.ReadDir(outside); err != nil || len(entries) != 0 {
		t.Fatalf("the write reached %s: %v %v", outside, entries, err)
	}
}

// L3: the parser is the reader's: KEY="value" lines of known keys, nothing bash would expand.
func TestParseLastLaunch(t *testing.T) {
	got, err := ParseLastLaunch([]byte(savedChoices))
	if err != nil || len(got) != 5 || got[0] != (Setting{"SAVED_CONFIG_VERSION", "2"}) || got[2] != (Setting{"LAST_SSH_KEYS", "/keys/id_a"}) {
		t.Fatalf("ParseLastLaunch = %v, %v", got, err)
	}
	for _, bad := range []string{"LAST_SHELL=\"zsh\"\n", "LAST_TOKEN=\"`id`\"\n", "LAST_TOKEN=\"a\"", "LAST_TOKEN=\"a\"\nLAST_TOKEN=\"b\"\n"} {
		if _, err := ParseLastLaunch([]byte(bad)); err == nil {
			t.Errorf("ParseLastLaunch(%q) passed", bad)
		}
	}
}

// L4: a person approves a changed file at a terminal, shown every choice it holds.
func TestApproveLastLaunch(t *testing.T) {
	r := newRig(t)
	write(t, r.dir, LastLaunch, savedChoices)
	r.answer = yes()
	if code := r.cmd().Approve(context.Background(), []string{"last-launch", r.dir}); code != term.ExitOK {
		t.Fatalf("approve: %v %s", code, r.err.String())
	}
	for _, want := range []string{"LAST_SSH_KEYS", "/keys/id_a", "proj_net"} {
		if !strings.Contains(r.err.String(), want) {
			t.Errorf("approval showed %q, lacking %q", r.err.String(), want)
		}
	}
	s, err := Locate(r.cmd().Getenv)
	if err != nil {
		t.Fatal(err)
	}
	_, digest, _, err := LastLaunchDigest(r.dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(r.dir, KindLastLaunch, digest); err != nil {
		t.Fatalf("approved but not recorded: %v", err)
	}
	write(t, r.dir, LastLaunch, "LAST_TOKEN=\"$(id)\"\n")
	if code := r.cmd().Approve(context.Background(), []string{"last-launch", r.dir}); code != term.ExitFailure {
		t.Fatalf("a malformed file was approved: %v", code)
	}
}
