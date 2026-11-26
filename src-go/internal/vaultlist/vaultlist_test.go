package vaultlist

import (
	"bytes"
	"context"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/hostvars"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// Made-up names; none is anyone's.
const (
	nameA = "Zorblax Widgets"
	nameB = "quillfeather-labs"
	nameC = "Marrowind"
)

// fakeVault is ansible-vault's ciphertext for plain: the hex of plain under a vault header, so
// the fake decrypt can read it back.
func fakeVault(plain string) string {
	return "$ANSIBLE_VAULT;1.2;AES256;localhost\n" + hex.EncodeToString([]byte(plain)) + "\n"
}

func vaultItem(plain string) string {
	return "  - !vault |\n      $ANSIBLE_VAULT;1.2;AES256;localhost\n      " + hex.EncodeToString([]byte(plain)) + "\n"
}

const head = `---
user_login: "u"
# a comment that stays
`

const tail = `
other_key: [1, 2]
`

type world struct {
	t        *testing.T
	root     string
	fake     *execx.Fake
	out, err bytes.Buffer
	echoOffs int
	restored int
	// onEncrypt runs at each encryption (a test changes the file under the run with it).
	onEncrypt func()
	// decryptAs, when set, is what every decrypt answers (a check that reads back wrong).
	decryptAs *string
}

func newWorld(t *testing.T, vars string) *world {
	t.Helper()
	w := &world{t: t, root: t.TempDir(), fake: &execx.Fake{}}
	if err := os.MkdirAll(filepath.Join(w.root, filepath.Dir(hostVarsPath)), 0o700); err != nil {
		t.Fatal(err)
	}
	w.write(vars)
	w.fake.OnFunc([]string{filepath.Join(w.root, "vault.bash"), "encrypt"}, func(c execx.Cmd) (execx.Result, error) {
		plain, err := io.ReadAll(c.Stdin)
		if err != nil {
			return execx.Result{}, err
		}
		if w.onEncrypt != nil {
			w.onEncrypt()
		}
		out := c.Args[1] + ": !vault |\n          " + strings.ReplaceAll(strings.TrimSuffix(fakeVault(string(plain)), "\n"), "\n", "\n          ") + "\n"
		return execx.Result{Stdout: []byte(out), Stderr: []byte("Encryption successful\n")}, nil
	})
	w.fake.OnFunc([]string{"ansible-vault", "decrypt"}, func(c execx.Cmd) (execx.Result, error) {
		data, err := io.ReadAll(c.Stdin)
		if err != nil {
			return execx.Result{}, err
		}
		lines := strings.Split(strings.TrimSpace(string(data)), "\n")
		if len(lines) != 2 || !strings.HasPrefix(lines[0], "$ANSIBLE_VAULT;") {
			return execx.Result{ExitCode: 1, Stderr: []byte("ERROR! input is not vault encrypted data")}, nil
		}
		plain, err := hex.DecodeString(strings.TrimSpace(lines[1]))
		if err != nil {
			return execx.Result{}, err
		}
		if w.decryptAs != nil {
			plain = []byte(*w.decryptAs)
		}
		return execx.Result{Stdout: plain, Stderr: []byte("Decryption successful\n")}, nil
	})
	return w
}

func (w *world) path() string { return filepath.Join(w.root, hostVarsPath) }

func (w *world) write(text string) {
	w.t.Helper()
	if err := os.WriteFile(w.path(), []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) read() string {
	w.t.Helper()
	data, err := os.ReadFile(w.path())
	if err != nil {
		w.t.Fatal(err)
	}
	return string(data)
}

func (w *world) env(stdin string) Env {
	return Env{
		Runner:      w.fake,
		Getenv:      func(k string) string { return map[string]string{rootEnv: w.root}[k] },
		InContainer: func() bool { return false },
		ReadFile:    os.ReadFile,
		WriteFile:   hostvars.WriteKeepingMode,
		Stdin:       strings.NewReader(stdin),
		Stdout:      &w.out,
		Stderr:      &w.err,
		EchoOff: func() (func() error, error) {
			w.echoOffs++
			return func() error { w.restored++; return nil }, nil
		},
		IsTerminal: func(any) bool { return true },
	}
}

func (w *world) run(stdin string, args ...string) term.ExitCode {
	w.out.Reset()
	w.err.Reset()
	return App{Env: w.env(stdin)}.Run(context.Background(), args)
}

// items is what private_name_terms decrypts to, as the fake reads it.
func (w *world) items() []string {
	w.t.Helper()
	top, err := hostvars.TopMapping([]byte(w.read()))
	if err != nil {
		w.t.Fatal(err)
	}
	var out []string
	v := hostvars.TopValue(top, "private_name_terms")
	if v == nil {
		return nil
	}
	for _, n := range v.Content {
		if n.Tag != hostvars.VaultTag {
			w.t.Fatalf("item not vaulted: %+v", n)
		}
		lines := strings.Split(strings.TrimSpace(n.Value), "\n")
		plain, err := hex.DecodeString(lines[1])
		if err != nil {
			w.t.Fatal(err)
		}
		out = append(out, string(plain))
	}
	return out
}

func (w *world) noNameShown() {
	w.t.Helper()
	for _, n := range []string{nameA, nameB, nameC} {
		for _, s := range []string{w.out.String(), w.err.String()} {
			if strings.Contains(strings.ToLower(s), strings.ToLower(n)) {
				w.t.Errorf("a name was shown:\n%s", s)
			}
		}
	}
}

func TestAddsToAnAbsentListAndKeepsTheRest(t *testing.T) {
	w := newWorld(t, head+tail)
	if code := w.run(nameA+"\n"+nameB+"\n\n", "add", "private_name_terms"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
	if got := w.items(); strings.Join(got, "|") != nameA+"|"+nameB {
		t.Errorf("items = %q", got)
	}
	if !strings.HasPrefix(w.read(), head+tail) {
		t.Errorf("the rest of the file changed:\n%s", w.read())
	}
	if w.out.String() != "private_name_terms: 2 items, 2 added\n" {
		t.Errorf("stdout = %q", w.out.String())
	}
	if w.echoOffs != 3 || w.restored != 3 {
		t.Errorf("echo off %d, restored %d; want 3 each", w.echoOffs, w.restored)
	}
	w.noNameShown()
}

// V1: no value reaches an argv or the environment; vault.bash and ansible-vault get it on stdin,
// in the checkout, under its ansible.cfg (the vault id and password file it names).
func TestValuesTravelOnStdinOnlyUnderTheCheckoutsConfig(t *testing.T) {
	w := newWorld(t, head+"private_name_terms:\n"+vaultItem(nameA)+tail)
	if code := w.run(nameB+"\n\n", "add", "private_name_terms"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
	decrypts := 0
	for _, c := range w.fake.Calls() {
		seen := strings.ToLower(c.Line() + " " + strings.Join(c.Env, " "))
		for _, n := range []string{nameA, nameB} {
			if strings.Contains(seen, strings.ToLower(n)) {
				t.Errorf("a value is in the argv or environment of %s", c.Name)
			}
		}
		if c.Dir != w.root {
			t.Errorf("%s ran in %q, not the checkout", c.Name, c.Dir)
		}
		if c.Name == "ansible-vault" {
			decrypts++
			if !slices.Contains(c.Env, "ANSIBLE_CONFIG="+filepath.Join(w.root, "ansible.cfg")) {
				t.Errorf("decrypt without the checkout's ansible.cfg: %q", c.Env)
			}
		}
	}
	if decrypts != 3 { // the item read, then both items after the write
		t.Errorf("%d decrypts, want 3", decrypts)
	}
}

func TestKeepsExistingItemsAndSkipsAnyAlreadyListed(t *testing.T) {
	vars := head + "private_name_terms:\n" + vaultItem(nameA) + tail
	w := newWorld(t, vars)
	// The same name in another case, then a new one twice.
	stdin := strings.ToUpper(nameA) + "\n" + nameC + "\n" + nameC + "\n\n"
	if code := w.run(stdin, "add", "private_name_terms"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
	if got := w.items(); strings.Join(got, "|") != nameA+"|"+nameC {
		t.Errorf("items = %q", got)
	}
	if !strings.Contains(w.err.String(), "already listed") {
		t.Errorf("no word of the duplicate:\n%s", w.err.String())
	}
	if w.out.String() != "private_name_terms: 2 items, 1 added\n" {
		t.Errorf("stdout = %q", w.out.String())
	}
	if !strings.HasSuffix(w.read(), tail) || !strings.HasPrefix(w.read(), head) {
		t.Errorf("the rest of the file changed:\n%s", w.read())
	}
	w.noNameShown()
}

func TestNothingToAddLeavesTheFileAlone(t *testing.T) {
	vars := head + "private_name_terms:\n" + vaultItem(nameA) + tail
	w := newWorld(t, vars)
	if code := w.run(nameA+"\n\n", "add", "private_name_terms"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
	if w.read() != vars {
		t.Errorf("the file changed:\n%s", w.read())
	}
	if w.out.String() != "private_name_terms: 1 items, 0 added\n" {
		t.Errorf("stdout = %q", w.out.String())
	}
	if w.fake.Called(filepath.Join(w.root, "vault.bash")) {
		t.Error("something was encrypted")
	}
}

func TestImportsAFileOfNames(t *testing.T) {
	w := newWorld(t, head+tail)
	file := filepath.Join(t.TempDir(), "names")
	if err := os.WriteFile(file, []byte("# a header\n\n  "+nameB+"  \n"+nameC+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.run(nameA+"\n\n", "add", "private_name_terms", "--import", file); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
	if got := w.items(); strings.Join(got, "|") != nameB+"|"+nameC+"|"+nameA {
		t.Errorf("items = %q", got)
	}
	if w.out.String() != "private_name_terms: 3 items, 3 added\n" {
		t.Errorf("stdout = %q", w.out.String())
	}
	w.noNameShown()
}

func TestABadImportLineIsRefusedByItsNumber(t *testing.T) {
	w := newWorld(t, head+tail)
	file := filepath.Join(t.TempDir(), "names")
	if err := os.WriteFile(file, []byte(nameA+"\nbad\x01name\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := w.run("\n", "add", "private_name_terms", "--import", file); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.err.String(), "line 2") || w.read() != head+tail {
		t.Errorf("stderr:\n%s\nfile:\n%s", w.err.String(), w.read())
	}
	w.noNameShown()
	if code := w.run("\n", "add", "private_name_terms", "--import", filepath.Join(t.TempDir(), "none")); code != term.ExitFailure {
		t.Errorf("a missing import file: exit %d", code)
	}
}

func TestABadNameIsAskedAgainAndThreeEndTheRun(t *testing.T) {
	w := newWorld(t, head+tail)
	if code := w.run("#"+nameA+"\n"+nameB+"\n\n", "add", "private_name_terms"); code != term.ExitOK {
		t.Fatalf("exit %d\n%s", code, w.err.String())
	}
	if !strings.Contains(w.err.String(), "cannot start with #") {
		t.Errorf("no complaint:\n%s", w.err.String())
	}
	if got := w.items(); strings.Join(got, "|") != nameB {
		t.Errorf("items = %q", got)
	}
	w.noNameShown()

	w = newWorld(t, head+tail)
	if code := w.run("#a\n#b\n#c\n", "add", "private_name_terms"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if w.read() != head+tail || !strings.Contains(w.err.String(), "nothing was changed") {
		t.Errorf("stderr:\n%s", w.err.String())
	}
}

func TestTheInputEndingChangesNothing(t *testing.T) {
	w := newWorld(t, head+tail)
	if code := w.run(nameA+"\n", "add", "private_name_terms"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if w.read() != head+tail || !strings.Contains(w.err.String(), "nothing was changed") {
		t.Errorf("stderr:\n%s", w.err.String())
	}
}

func TestAListThatIsNotAllVaultedIsRefused(t *testing.T) {
	for name, value := range map[string]string{
		"a plain item":  "private_name_terms:\n  - plain\n",
		"a mapping":     "private_name_terms:\n  a: 1\n",
		"a vault value": "private_name_terms: !vault |\n  $ANSIBLE_VAULT;1.2;AES256;localhost\n  00\n",
	} {
		w := newWorld(t, head+value+tail)
		if code := w.run("\n", "add", "private_name_terms"); code != term.ExitFailure {
			t.Errorf("%s: exit %d", name, code)
		}
		if !strings.Contains(w.err.String(), "private_name_terms") {
			t.Errorf("%s: stderr does not name the variable:\n%s", name, w.err.String())
		}
	}
	w := newWorld(t, head+"private_name_terms:\n  - !vault |\n      not-a-vault\n"+tail)
	if code := w.run("\n", "add", "private_name_terms"); code != term.ExitFailure || !strings.Contains(w.err.String(), "item 1") {
		t.Errorf("an undecryptable item: exit %d\n%s", code, w.err.String())
	}
}

func TestAnEmptyOrNullListIsEmpty(t *testing.T) {
	for _, value := range []string{"private_name_terms:\n", "private_name_terms: []\n"} {
		w := newWorld(t, head+value+tail)
		if code := w.run(nameA+"\n\n", "add", "private_name_terms"); code != term.ExitOK {
			t.Fatalf("%q: exit %d\n%s", value, code, w.err.String())
		}
		if got := w.items(); len(got) != 1 {
			t.Errorf("%q: items = %q", value, got)
		}
		if !strings.Contains(w.read(), "other_key: [1, 2]") {
			t.Errorf("%q: lost the rest:\n%s", value, w.read())
		}
	}
}

func TestAFileChangedDuringTheRunIsNotOverwritten(t *testing.T) {
	w := newWorld(t, head+tail)
	w.onEncrypt = func() { w.write(head + "late: 1\n" + tail) }
	if code := w.run(nameA+"\n\n", "add", "private_name_terms"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.err.String(), "changed while") || strings.Contains(w.read(), "private_name_terms") {
		t.Errorf("stderr:\n%s\nfile:\n%s", w.err.String(), w.read())
	}
}

func TestAWrittenListThatDoesNotDecryptBackFails(t *testing.T) {
	w := newWorld(t, head+tail)
	wrong := nameC
	w.onEncrypt = func() { w.decryptAs = &wrong }
	if code := w.run(nameA+"\n\n", "add", "private_name_terms"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(w.err.String(), "does not decrypt back") {
		t.Errorf("stderr:\n%s", w.err.String())
	}
	w.noNameShown()
}

func TestEncryptionFailureChangesNothing(t *testing.T) {
	w := newWorld(t, head+tail)
	w.fake.On([]string{filepath.Join(w.root, "vault.bash"), "encrypt"}, execx.Result{ExitCode: 1, Stderr: []byte("ERROR: vault-pass.secret not found")})
	if code := w.run(nameA+"\n\n", "add", "private_name_terms"); code != term.ExitFailure {
		t.Fatalf("exit %d", code)
	}
	if w.read() != head+tail || !strings.Contains(w.err.String(), "vault.bash encrypt") {
		t.Errorf("stderr:\n%s", w.err.String())
	}
}

func TestUsageAndPlaceRefusals(t *testing.T) {
	w := newWorld(t, head+tail)
	for _, args := range [][]string{nil, {"add"}, {"add", "Bad-Name"}, {"remove", "x"}, {"add", "x", "--import"}, {"add", "x", "--other", "f"}, {"add", "x", "y"}} {
		if code := w.run("", args...); code != term.ExitUsage {
			t.Errorf("%q: exit %d", args, code)
		}
	}
	if code := w.run("", "--help"); code != term.ExitOK || !strings.Contains(w.out.String(), "Usage: vault-list") {
		t.Errorf("--help: exit %d\n%s", code, w.out.String())
	}

	env := w.env("")
	env.InContainer = func() bool { return true }
	if code := (App{Env: env}).Run(context.Background(), []string{"add", "x"}); code != term.ExitFailure {
		t.Errorf("in a container: exit %d", code)
	}
	env = w.env("")
	env.Getenv = func(string) string { return "relative" }
	if code := (App{Env: env}).Run(context.Background(), []string{"add", "x"}); code != term.ExitFailure {
		t.Errorf("a bad root: exit %d", code)
	}
	env = w.env("")
	env.ReadFile = func(string) ([]byte, error) { return nil, errors.New("unreadable") }
	if code := (App{Env: env}).Run(context.Background(), []string{"add", "x"}); code != term.ExitFailure {
		t.Errorf("an unreadable host_vars: exit %d", code)
	}
	w.write("a: [\n")
	if code := w.run("", "add", "x"); code != term.ExitFailure {
		t.Errorf("a host_vars that is not YAML: exit %d", code)
	}
}

func TestNoEchoControlRefusesToAsk(t *testing.T) {
	w := newWorld(t, head+tail)
	env := w.env(nameA + "\n\n")
	env.EchoOff = nil
	if code := (App{Env: env}).Run(context.Background(), []string{"add", "private_name_terms"}); code != term.ExitFailure {
		t.Errorf("exit %d", code)
	}
	if w.read() != head+tail {
		t.Error("the file changed")
	}
}

func TestCheckName(t *testing.T) {
	for _, good := range []string{"a", nameA, "x#y", "émile"} {
		if err := checkName(good); err != nil {
			t.Errorf("%q refused: %v", good, err)
		}
	}
	for _, bad := range []string{"#x", "a\tb", "a\nb", "a\x7fb"} {
		if err := checkName(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

func TestNewEnvIsWired(t *testing.T) {
	env := NewEnv()
	if env.Runner == nil || env.Getenv == nil || env.InContainer == nil || env.ReadFile == nil ||
		env.WriteFile == nil || env.Stdin == nil || env.Stdout == nil || env.Stderr == nil || env.EchoOff == nil {
		t.Errorf("NewEnv left a field unset: %+v", env)
	}
}
