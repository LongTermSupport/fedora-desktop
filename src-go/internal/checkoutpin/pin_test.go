package checkoutpin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var (
	digestA = strings.Repeat("a", 64)
	digestB = strings.Repeat("b", 64)
	digestC = strings.Repeat("c", 64)
)

var testAt = time.Date(2026, 11, 26, 20, 0, 0, 0, time.UTC)

// checkout is a fresh checkout directory, its real path.
func checkout(t *testing.T) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func write(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func store(t *testing.T) Store {
	t.Helper()
	s, err := Locate(func(k string) string { return map[string]string{"CCY_STATE_DIR": t.TempDir()}[k] })
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// P1: the store sits beside the session registry, never in a checkout, and a relative base is
// refused as the registry refuses it.
func TestLocateIsBesideTheRegistry(t *testing.T) {
	s, err := Locate(func(k string) string { return map[string]string{"HOME": "/home/u"}[k] })
	if err != nil || s.Dir != "/home/u/.local/state/ccy/approved" {
		t.Fatalf("Locate = %q, %v", s.Dir, err)
	}
	if _, err := Locate(func(k string) string { return map[string]string{"CCY_STATE_DIR": "rel"}[k] }); err == nil {
		t.Fatal("a relative base was accepted")
	}
}

// P2: nothing recorded is a refusal that names the file and the exact approve verb; a recorded
// digest passes; another digest is a refusal saying it changed.
func TestVerifyRefusesWhatNoPersonApproved(t *testing.T) {
	s, dir := store(t), checkout(t)
	err := s.Verify(dir, KindMounts, digestA)
	var refused *RefusalError
	if !errors.As(err, &refused) {
		t.Fatalf("Verify with nothing recorded = %v", err)
	}
	for _, want := range []string{dir + "/" + Mounts, "ccy-teams approve mounts " + dir, "no approval"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q lacks %q", err, want)
		}
	}
	if err := s.Record(dir, KindMounts, digestA, testAt); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(dir, KindMounts, digestA); err != nil {
		t.Fatalf("Verify of the recorded digest = %v", err)
	}
	err = s.Verify(dir, KindMounts, digestB)
	if !errors.As(err, &refused) || !strings.Contains(err.Error(), "changed since") {
		t.Fatalf("Verify of another digest = %v", err)
	}
	if err := s.Verify(dir, KindCompose, digestA); !errors.As(err, &refused) {
		t.Fatalf("a mounts approval passed compose: %v", err)
	}
	if err := s.Verify(checkout(t), KindMounts, digestA); !errors.As(err, &refused) {
		t.Fatalf("one checkout's approval passed another: %v", err)
	}
}

// P3: the record is the user's alone (0700 directories, 0600 file), and a record that is not
// one this package wrote, or names another checkout, is an error, never a pass.
func TestRecordIsPrivateAndReadStrictly(t *testing.T) {
	s, dir := store(t), checkout(t)
	if err := s.Record(dir, KindCompose, digestC, testAt); err != nil {
		t.Fatal(err)
	}
	path := s.path(dir, KindCompose)
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("record mode = %v, %v", info, err)
	}
	if d, err := os.Stat(s.Dir); err != nil || d.Mode().Perm() != 0o700 {
		t.Fatalf("store mode = %v, %v", d, err)
	}
	if err := os.WriteFile(path, []byte("ccy-checkout-pin 1\ncheckout=/elsewhere\nsha256="+digestC+"\napproved=x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	var refused *RefusalError
	if err := s.Verify(dir, KindCompose, digestC); err == nil || errors.As(err, &refused) {
		t.Fatalf("a record naming another checkout = %v", err)
	}
	if err := os.WriteFile(path, []byte("garbage\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.Verify(dir, KindCompose, digestC); err == nil || errors.As(err, &refused) {
		t.Fatalf("a malformed record = %v", err)
	}
}

// P4: the mounts digest is the file's bytes; a link in its place is refused, an absent file is
// not present.
func TestMountsDigest(t *testing.T) {
	dir := checkout(t)
	if _, _, present, err := MountsDigest(dir); present || err != nil {
		t.Fatalf("absent: present=%v err=%v", present, err)
	}
	write(t, dir, Mounts, "/srv:/x\n")
	content, one, present, err := MountsDigest(dir)
	if err != nil || !present || string(content) != "/srv:/x\n" || len(one) != 64 {
		t.Fatalf("MountsDigest = %q %q %v %v", content, one, present, err)
	}
	write(t, dir, Mounts, "/srv:/y\n")
	if _, two, _, err := MountsDigest(dir); err != nil || two == one {
		t.Fatalf("a changed file kept its digest: %v", err)
	}
	other := checkout(t)
	write(t, other, "m", "/srv:/x\n")
	if err := os.MkdirAll(filepath.Join(other, ".claude/ccy"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(other, "m"), filepath.Join(other, Mounts)); err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := MountsDigest(other); err == nil {
		t.Fatal("a link in the mounts file's place was read")
	}
}

// P5: the compose digest covers every compose file and override present, the .env the files
// are interpolated from, and the files they include or extend; any of them changing, or one
// appearing, changes it. A file outside the checkout, a link, or a .env naming COMPOSE_FILE is
// refused: no pin could cover what it reads.
func TestComposeDigest(t *testing.T) {
	dir := checkout(t)
	if files, _, present, err := ComposeDigest(dir); present || err != nil || len(files) != 0 {
		t.Fatalf("no compose: %v %v %v", files, present, err)
	}
	write(t, dir, "compose.yml", "include:\n  - other/base.yml\nservices:\n  a:\n    extends:\n      file: ext.yml\n      service: b\n")
	write(t, dir, "other/base.yml", "services: {}\n")
	write(t, dir, "ext.yml", "services:\n  b:\n    image: x\n")
	files, base, present, err := ComposeDigest(dir)
	if err != nil || !present {
		t.Fatalf("ComposeDigest = %v %v", present, err)
	}
	if strings.Join(files, ",") != "compose.yml,other/base.yml,ext.yml" {
		t.Fatalf("files = %v", files)
	}
	for _, change := range []struct{ name, content string }{
		{"other/base.yml", "services:\n  evil:\n    image: x\n"},
		{"ext.yml", "services:\n  b:\n    volumes: [/:/host]\n"},
		{"compose.override.yml", "services: {}\n"},
		{".env", "X=1\n"},
	} {
		write(t, dir, change.name, change.content)
		_, now, _, err := ComposeDigest(dir)
		if err != nil || now == base {
			t.Errorf("%s changed and the digest did not (%v)", change.name, err)
		}
		base = now
	}
	for name, content := range map[string]string{
		"outside": "include:\n  - ../outside.yml\n",
		"abs":     "services:\n  a:\n    extends:\n      file: /etc/x.yml\n      service: b\n",
	} {
		bad := checkout(t)
		write(t, bad, "docker-compose.yml", content)
		if _, _, _, err := ComposeDigest(bad); err == nil {
			t.Errorf("%s: an include outside the checkout was accepted", name)
		}
	}
	env := checkout(t)
	write(t, env, "compose.yaml", "services: {}\n")
	write(t, env, ".env", "export COMPOSE_FILE=/tmp/x.yml\n")
	if _, _, _, err := ComposeDigest(env); err == nil {
		t.Error("a .env naming COMPOSE_FILE was accepted")
	}
}
