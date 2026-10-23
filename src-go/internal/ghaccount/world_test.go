package ghaccount

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// ghAccount is one account the fake gh holds.
type ghAccount struct {
	state   string // "success", or anything else for a failed token
	missing string // the scopes the audit reports missing, "" for none
	// afterRefresh is what is missing after a refresh; "" is none.
	afterRefresh string
	unreadable   bool
}

// world is a fake machine: a checkout, a home, and gh, python3, ansible, ssh-keygen and ssh as
// one execx.Fake answers them. The Fake holds its lock while a rule runs, so the rules may
// change the world's state without one of their own.
type world struct {
	t      *testing.T
	root   string
	home   string
	yml    string
	env    map[string]string
	fake   *execx.Fake
	stdout bytes.Buffer
	stderr bytes.Buffer
	helper string

	accounts    map[string]*ghAccount
	order       []string
	active      string
	loginAs     string // the account a browser login authorises
	loginFails  bool
	refreshFail bool
	switchFails bool
	keysFail    bool
	addFails    bool
	keys        map[string][]string // login -> registered public key lines
	sshAs       map[string]string   // key path -> the login GitHub greets it as
	vaultOut    string
	vaultFails  bool
	ghVersion   string
	statusFails bool
}

func newWorld(t *testing.T) *world {
	t.Helper()
	base := t.TempDir()
	w := &world{
		t:         t,
		root:      filepath.Join(base, "checkout"),
		home:      filepath.Join(base, "home"),
		fake:      &execx.Fake{},
		accounts:  map[string]*ghAccount{},
		keys:      map[string][]string{},
		sshAs:     map[string]string{},
		vaultOut:  "localhost | SUCCESS => {\n    \"changed\": false,\n    \"msg\": \"vault pw\"\n}\n",
		ghVersion: "gh version 2.103.0 (2026-10-21)\n",
	}
	for _, d := range []string{w.root, w.home} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	w.yml = filepath.Join(w.root, "environment/localhost/host_vars/localhost.yml")
	w.helper = filepath.Join(base, "system-gh-print-auth-url")
	w.write(filepath.Join(w.root, "files/usr/local/bin/gh-print-auth-url"), "#!/bin/bash\n")
	w.env = map[string]string{"GH_ACCOUNT_SETUP_ROOT": w.root, "HOME": w.home}
	w.script()
	return w
}

func (w *world) write(path, text string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

// config writes localhost.yml with these alias:login pairs.
func (w *world) config(pairs ...string) {
	w.t.Helper()
	var text strings.Builder
	text.WriteString("user_login: someone\ngithub_accounts:\n")
	for _, p := range pairs {
		parts := strings.SplitN(p, ":", 2)
		if len(parts) != 2 {
			w.t.Fatalf("pair %q", p)
		}
		fmt.Fprintf(&text, "  %s: %q\n", parts[0], parts[1])
	}
	w.write(w.yml, text.String())
}

// logIn makes login an account gh holds, with its scopes complete.
func (w *world) logIn(login string) *ghAccount {
	a, ok := w.accounts[login]
	if !ok {
		a = &ghAccount{}
		w.accounts[login] = a
		w.order = append(w.order, login)
	}
	a.state = "success"
	return a
}

// key makes ~/.ssh/github_<alias> and its .pub, and says GitHub greets it as login.
func (w *world) key(alias, login string) string {
	path := filepath.Join(w.home, ".ssh", "github_"+alias)
	w.write(path, "PRIVATE\n")
	w.write(path+".pub", "ssh-ed25519 KEY"+alias+" "+login+"@github\n")
	w.sshAs[path] = login
	return path
}

func (w *world) register(login, alias string) {
	w.keys[login] = append(w.keys[login], "ssh-ed25519 KEY"+alias)
}

// ready is an account that needs nothing: logged in, its key made, uploaded and greeted.
func (w *world) ready(alias, login string) {
	w.logIn(login)
	w.key(alias, login)
	w.register(login, alias)
}

func lastArg(c execx.Cmd) string { return c.Args[len(c.Args)-1] }

func argAfter(c execx.Cmd, flag string) string {
	i := slices.Index(c.Args, flag)
	if i < 0 || i+1 >= len(c.Args) {
		return ""
	}
	return c.Args[i+1]
}

func (w *world) script() {
	f := w.fake
	f.OnFunc([]string{"gh", "--version"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte(w.ghVersion)}, nil
	})
	f.OnFunc([]string{"gh", "auth", "status"}, func(execx.Cmd) (execx.Result, error) {
		if w.statusFails {
			return execx.Result{Stderr: []byte("fatal"), ExitCode: 1}, nil
		}
		return execx.Result{Stdout: w.statusJSON()}, nil
	})
	f.OnFunc([]string{"gh", "auth", "switch"}, func(c execx.Cmd) (execx.Result, error) {
		if w.switchFails {
			return execx.Result{Stderr: []byte("switch refused"), ExitCode: 1}, nil
		}
		if _, ok := w.accounts[lastArg(c)]; !ok {
			return execx.Result{Stderr: []byte("no such account"), ExitCode: 1}, nil
		}
		w.active = lastArg(c)
		return execx.Result{}, nil
	})
	f.OnFunc([]string{"gh", "auth", "token"}, func(c execx.Cmd) (execx.Result, error) {
		if a, ok := w.accounts[lastArg(c)]; !ok || a.state != "success" {
			return execx.Result{Stderr: []byte("no token"), ExitCode: 1}, nil
		}
		return execx.Result{Stdout: []byte("tok-" + lastArg(c) + "\n")}, nil
	})
	f.OnFunc([]string{"gh", "api", "user/keys"}, func(c execx.Cmd) (execx.Result, error) {
		login, err := tokenLogin(c)
		if err != nil {
			return execx.Result{}, err
		}
		if w.keysFail {
			return execx.Result{Stderr: []byte("HTTP 502"), ExitCode: 1}, nil
		}
		return execx.Result{Stdout: []byte(strings.Join(w.keys[login], "\n") + "\n")}, nil
	})
	f.OnFunc([]string{"gh", "ssh-key", "add"}, func(c execx.Cmd) (execx.Result, error) {
		login, err := tokenLogin(c)
		if err != nil {
			return execx.Result{}, err
		}
		if w.addFails {
			return execx.Result{Stderr: []byte("HTTP 422"), ExitCode: 1}, nil
		}
		data, err := os.ReadFile(c.Args[2])
		if err != nil {
			return execx.Result{}, err
		}
		fields := strings.Fields(string(data))
		w.keys[login] = append(w.keys[login], fields[0]+" "+fields[1])
		return execx.Result{}, nil
	})
	w.scriptTools()
}

// fakeRequiredScopes is what the fake scope helper says gh must ask for; a test reads the
// command line gh was given against it, never against a list of its own.
const fakeRequiredScopes = "alpha,beta"

// scriptTools answers the scope helper, ansible, ssh-keygen, ssh and gh's browser flows.
func (w *world) scriptTools() {
	f := w.fake
	f.OnFunc([]string{"python3", "-m", "helpers.github_scopes.cli", "required"}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte(fakeRequiredScopes + "\n")}, nil
	})
	f.OnFunc([]string{"python3", "-m", "helpers.github_scopes.cli", "audit"}, func(c execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: []byte(w.audit(c))}, nil
	})
	f.OnFunc([]string{"ansible", "localhost"}, func(execx.Cmd) (execx.Result, error) {
		if w.vaultFails {
			return execx.Result{Stderr: []byte("Decryption failed"), ExitCode: 2}, nil
		}
		return execx.Result{Stdout: []byte(w.vaultOut)}, nil
	})
	f.OnFunc([]string{"ssh-keygen"}, func(c execx.Cmd) (execx.Result, error) {
		path, comment := argAfter(c, "-f"), argAfter(c, "-C")
		if err := os.WriteFile(path, []byte("PRIVATE\n"), 0o644); err != nil {
			return execx.Result{}, err
		}
		if err := os.WriteFile(path+".pub", []byte("ssh-ed25519 KEYNEW"+filepath.Base(path)+" "+comment+"\n"), 0o644); err != nil {
			return execx.Result{}, err
		}
		w.sshAs[path] = strings.TrimSuffix(comment, "@github")
		return execx.Result{}, nil
	})
	f.OnFunc([]string{"ssh"}, func(c execx.Cmd) (execx.Result, error) {
		if login := w.sshAs[argAfter(c, "-i")]; login != "" {
			return execx.Result{Stderr: []byte("Hi " + login + "! You've successfully authenticated, but GitHub does not provide shell access.\n"), ExitCode: 1}, nil
		}
		return execx.Result{Stderr: []byte("ssh: connect to host github.com port 22: Connection timed out\n"), ExitCode: 255}, nil
	})
	f.OnFunc([]string{"gh", "auth", "login"}, func(execx.Cmd) (execx.Result, error) {
		if w.loginFails {
			return execx.Result{ExitCode: 1}, nil
		}
		w.logIn(w.loginAs)
		w.active = w.loginAs
		return execx.Result{}, nil
	})
	f.OnFunc([]string{"gh", "auth", "refresh"}, func(execx.Cmd) (execx.Result, error) {
		if w.refreshFail {
			return execx.Result{ExitCode: 1}, nil
		}
		a := w.accounts[w.active]
		a.missing = a.afterRefresh
		return execx.Result{}, nil
	})
}

// audit answers the scope helper's audit as the real one would for the world's accounts.
func (w *world) audit(c execx.Cmd) string {
	var out strings.Builder
	for i, a := range c.Args {
		if a != "--user" {
			continue
		}
		login := c.Args[i+1]
		acc, ok := w.accounts[login]
		switch {
		case !ok || acc.state != "success":
			out.WriteString("SCOPES-NOT-AUTHENTICATED " + login + "\n")
		case acc.unreadable:
			out.WriteString("SCOPES-UNREADABLE " + login + " HTTP 401\n")
		case acc.missing != "":
			out.WriteString("SCOPES-MISSING " + login + " " + acc.missing + "\n")
		default:
			out.WriteString("SCOPES-OK " + login + "\n")
		}
	}
	return out.String()
}

// tokenLogin is the account a command acts as through its GH_TOKEN secret; a gh call that
// would act as the active account instead is an error in this world.
func tokenLogin(c execx.Cmd) (string, error) {
	for _, s := range c.Secrets {
		if s.Name() != "GH_TOKEN" {
			continue
		}
		for _, l := range []string{"alice", "bob", "carol", "dave", "jane"} {
			if s.Is("tok-" + l) {
				return l, nil
			}
		}
	}
	return "", errors.New(c.Line() + ": no GH_TOKEN of a known account")
}

func (w *world) statusJSON() []byte {
	type entry struct {
		Login  string `json:"login"`
		Active bool   `json:"active"`
		State  string `json:"state"`
	}
	entries := make([]entry, 0, len(w.order))
	for _, login := range w.order {
		entries = append(entries, entry{Login: login, Active: login == w.active, State: w.accounts[login].state})
	}
	data, err := json.Marshal(map[string]any{"hosts": map[string]any{"github.com": entries}})
	if err != nil {
		w.t.Fatal(err)
	}
	return data
}

func (w *world) newEnv() Env {
	return Env{
		Runner:     w.fake,
		Foreground: w.fake,
		Getenv:     func(k string) string { return w.env[k] },
		TakeSecret: func(name string) (execx.Secret, bool, error) {
			v, ok := w.env[name]
			delete(w.env, name)
			if !ok || v == "" {
				return execx.Secret{}, false, nil
			}
			s, err := execx.NewSecret(name, v)
			return s, err == nil, err
		},
		LookPath: func(name string) (string, error) {
			if name == "gh" && w.env["TEST_NO_GH"] == "" {
				return "/usr/bin/gh", nil
			}
			return "", errors.New("not found")
		},
		Hostname:            func() (string, error) { return "desk", nil },
		Executable:          func() (string, error) { return "/cache/gh-account-setup", nil },
		SystemBrowserHelper: w.helper,
		Stdout:              &w.stdout,
		Stderr:              &w.stderr,
	}
}

func (w *world) run(args ...string) term.ExitCode {
	return w.runCtx(context.Background(), args...)
}

func (w *world) runCtx(ctx context.Context, args ...string) term.ExitCode {
	w.stdout.Reset()
	w.stderr.Reset()
	return App{Env: w.newEnv()}.Run(ctx, args)
}

// lines is every recorded command line, the foreground ones marked, for a test that asserts
// what ran and what did not.
func (w *world) lines() []string {
	calls, fgs := w.fake.Calls(), w.fake.Foregrounds()
	out := make([]string, 0, len(calls)+len(fgs))
	for _, c := range calls {
		out = append(out, c.Line())
	}
	for _, c := range fgs {
		out = append(out, "FG "+c.Line())
	}
	return out
}

func (w *world) ran(prefix string) bool {
	return slices.ContainsFunc(w.lines(), func(l string) bool { return strings.HasPrefix(l, prefix) })
}

func (w *world) foreground(prefix string) (execx.Cmd, bool) {
	for _, c := range w.fake.Foregrounds() {
		if strings.HasPrefix(c.Line(), prefix) {
			return c, true
		}
	}
	return execx.Cmd{}, false
}

func (w *world) call(prefix string) (execx.Cmd, bool) {
	for _, c := range w.fake.Calls() {
		if strings.HasPrefix(c.Line(), prefix) {
			return c, true
		}
	}
	return execx.Cmd{}, false
}

// mustCall is the first recorded command starting with prefix; none fails the test.
func (w *world) mustCall(prefix string) execx.Cmd {
	w.t.Helper()
	c, ok := w.call(prefix)
	if !ok {
		w.t.Fatalf("no %q among %v", prefix, w.lines())
	}
	return c
}

// mustForeground is mustCall for the foreground commands.
func (w *world) mustForeground(prefix string) execx.Cmd {
	w.t.Helper()
	c, ok := w.foreground(prefix)
	if !ok {
		w.t.Fatalf("no foreground %q among %v", prefix, w.lines())
	}
	return c
}

func (w *world) mustContain(stream, text string) {
	w.t.Helper()
	got := w.stderr.String()
	if stream == "stdout" {
		got = w.stdout.String()
	}
	if !strings.Contains(got, text) {
		w.t.Errorf("%s lacks %q:\n%s", stream, text, got)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }
