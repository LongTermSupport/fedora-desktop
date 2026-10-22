package execx

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
)

const placeholder = "placeholder-not-a-real-token-value"

// mustSecret is the placeholder value as a secret under name.
func mustSecret(t *testing.T, name string) Secret {
	t.Helper()
	s, err := NewSecret(name, placeholder)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewSecretRefusesABadNameOrAnEmptyValue(t *testing.T) {
	for _, name := range []string{"", "1X", "A-B", "A B", "A=B"} {
		if _, err := NewSecret(name, placeholder); err == nil {
			t.Errorf("NewSecret(%q) accepted a name that is not a variable's", name)
		}
	}
	if _, err := NewSecret("TOKEN", ""); err == nil {
		t.Error("NewSecret accepted an empty value")
	}
}

func TestASecretNeverPrintsItsValue(t *testing.T) {
	s := mustSecret(t, "CLAUDE_CODE_OAUTH_TOKEN")
	c := Cmd{Name: "podman", Args: []string{"run", "-e", s.Name()}, Secrets: []Secret{s}}
	for _, text := range []string{
		s.String(), fmt.Sprint(s), fmt.Sprintf("%v %+v %#v %s %q", s, s, s, s, s),
		fmt.Sprintf("%v %+v %#v", c, c, c), c.Line(),
	} {
		if strings.Contains(text, placeholder) {
			t.Fatalf("a secret's value was printed: %s", text)
		}
	}
	if s.Name() != "CLAUDE_CODE_OAUTH_TOKEN" || !strings.Contains(s.String(), "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Fatalf("a secret's name is shown: %s", s)
	}
	if !s.Is(placeholder) || s.Is("other") {
		t.Fatal("Is compares the value")
	}
}

// A secret reaches the one child it is given to, by name, and only that child.
func TestRunPassesASecretByNameToItsChildOnly(t *testing.T) {
	t.Setenv("CCY_TEST_TOKEN", placeholder)
	s, ok, err := TakeSecret("CCY_TEST_TOKEN")
	if err != nil || !ok {
		t.Fatalf("TakeSecret = %v, %v", ok, err)
	}
	if _, still := os.LookupEnv("CCY_TEST_TOKEN"); still {
		t.Fatal("TakeSecret left the variable for every later child to inherit")
	}
	script := `printf '%s|' "${CCY_TEST_TOKEN-unset}"; tr '\0' ' ' </proc/$$/cmdline`
	res, err := OS{}.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", script}, Secrets: []Secret{s}})
	if err != nil {
		t.Fatal(err)
	}
	given, argv, found := strings.Cut(string(res.Stdout), "|")
	if !found || given != placeholder {
		t.Fatalf("the child got %q, want the secret", res.Stdout)
	}
	if strings.Contains(argv, placeholder) {
		t.Fatalf("the secret is in the child's argv: %s", argv)
	}
	other, err := OS{}.Run(context.Background(), Cmd{Name: "sh", Args: []string{"-c", `printf '%s' "${CCY_TEST_TOKEN-unset}"`}})
	if err != nil {
		t.Fatal(err)
	}
	if string(other.Stdout) != "unset" {
		t.Fatalf("an unrelated child got the secret: %q", other.Stdout)
	}
}

func TestTakeSecretOfAnUnsetOrEmptyVariable(t *testing.T) {
	t.Setenv("CCY_TEST_EMPTY", "")
	if err := os.Unsetenv("CCY_TEST_EMPTY"); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := TakeSecret("CCY_TEST_EMPTY"); ok || err != nil {
		t.Fatalf("TakeSecret of an unset variable = %v, %v", ok, err)
	}
	t.Setenv("CCY_TEST_EMPTY", "")
	if _, ok, err := TakeSecret("CCY_TEST_EMPTY"); ok || err != nil {
		t.Fatalf("TakeSecret of an empty variable = %v, %v", ok, err)
	}
	if _, still := os.LookupEnv("CCY_TEST_EMPTY"); still {
		t.Fatal("an empty secret variable was left set")
	}
	if _, _, err := TakeSecret("not a name"); err == nil {
		t.Fatal("TakeSecret accepted a name that is not a variable's")
	}
}

func TestASecretInTheArgvIsRefusedBeforeAnythingRuns(t *testing.T) {
	s := mustSecret(t, "GH_TOKEN")
	bad := []Cmd{
		{Name: "gh", Args: []string{"auth", "login", "--with-token", placeholder}, Secrets: []Secret{s}},
		{Name: "podman", Args: []string{"run", "-e", "GH_TOKEN=" + placeholder}, Secrets: []Secret{s}},
		{Name: placeholder, Secrets: []Secret{s}},
	}
	for _, c := range bad {
		if _, err := (OS{}).Run(context.Background(), c); !errors.Is(err, ErrSecretInArgv) {
			t.Errorf("OS.Run(%s) error = %v, want ErrSecretInArgv", c.Name, err)
		}
		f := &Fake{}
		f.On(nil, Result{})
		if _, err := f.Run(context.Background(), c); !errors.Is(err, ErrSecretInArgv) {
			t.Errorf("Fake.Run error = %v, want ErrSecretInArgv", err)
		}
		if err := (OS{}).Exec(c); !errors.Is(err, ErrSecretInArgv) {
			t.Errorf("OS.Exec error = %v, want ErrSecretInArgv", err)
		}
		if _, err := (OS{}).Foreground(c); !errors.Is(err, ErrSecretInArgv) {
			t.Errorf("OS.Foreground error = %v, want ErrSecretInArgv", err)
		}
		c.NewSession = true
		if _, err := (OS{}).Run(context.Background(), c); !errors.Is(err, ErrSecretInArgv) {
			t.Errorf("a session run error = %v, want ErrSecretInArgv", err)
		}
	}
}

func TestAnErrorNeverCarriesTheValue(t *testing.T) {
	s := mustSecret(t, "GH_TOKEN")
	_, err := (OS{}).Run(context.Background(), Cmd{Name: "gh", Args: []string{"x" + placeholder}, Secrets: []Secret{s}})
	if err == nil || strings.Contains(err.Error(), placeholder) || !strings.Contains(err.Error(), "GH_TOKEN") {
		t.Fatalf("the refusal = %v: it must name the variable and never the value", err)
	}
}

func TestASessionRunPassesTheSecret(t *testing.T) {
	s := mustSecret(t, "CCY_TEST_TOKEN")
	res, err := OS{}.Run(context.Background(), Cmd{
		Name: "sh", Args: []string{"-c", `printf '%s' "$CCY_TEST_TOKEN"`}, Secrets: []Secret{s}, NewSession: true,
	})
	if err != nil || string(res.Stdout) != placeholder {
		t.Fatalf("a session child got %q, %v", res.Stdout, err)
	}
}

// A fed secret reaches the child on its stdin, inside its text, and neither its environment
// nor its argv; a session run feeds it too.
func TestRunFeedsASecretOnStdin(t *testing.T) {
	s := mustSecret(t, "CCY_TEST_TOKEN")
	script := `cat; printf '|%s|' "${CCY_TEST_TOKEN-unset}"; tr '\0' ' ' </proc/$$/cmdline`
	for _, session := range []bool{false, true} {
		c := Cmd{Name: "sh", Args: []string{"-c", script}, Feed: &Feed{Secret: s, Before: "header = \"", After: "\"\n"}, NewSession: session}
		res, err := OS{}.Run(context.Background(), c)
		if err != nil {
			t.Fatal(err)
		}
		parts := strings.SplitN(string(res.Stdout), "|", 3)
		if len(parts) != 3 || parts[0] != "header = \""+placeholder+"\"\n" || parts[1] != "unset" || strings.Contains(parts[2], placeholder) {
			t.Fatalf("session %v: the child saw %q", session, parts)
		}
	}
}

// A fed secret is held to the argv check, a Feed and a Stdin together are refused, and the
// runners that cannot write a child's stdin refuse a Feed.
func TestAFeedIsCheckedAndRefusedWhereItCannotBeFed(t *testing.T) {
	s := mustSecret(t, "CCY_TEST_TOKEN")
	feed := &Feed{Secret: s}
	leak := Cmd{Name: "curl", Args: []string{placeholder}, Feed: feed}
	if _, err := (OS{}).Run(context.Background(), leak); !errors.Is(err, ErrSecretInArgv) {
		t.Errorf("OS.Run of a fed secret in argv: %v, want ErrSecretInArgv", err)
	}
	f := &Fake{}
	f.On(nil, Result{})
	if _, err := f.Run(context.Background(), leak); !errors.Is(err, ErrSecretInArgv) {
		t.Errorf("Fake.Run of a fed secret in argv: %v, want ErrSecretInArgv", err)
	}
	both := Cmd{Name: "true", Feed: feed, Stdin: strings.NewReader("x")}
	if _, err := (OS{}).Run(context.Background(), both); err == nil {
		t.Error("OS.Run accepted a Feed and a Stdin together")
	}
	fed := Cmd{Name: "true", Feed: feed}
	if err := (OS{}).Exec(fed); !errors.Is(err, ErrFeedUnsupported) {
		t.Errorf("OS.Exec of a Feed: %v, want ErrFeedUnsupported", err)
	}
	if _, err := (OS{}).Foreground(fed); !errors.Is(err, ErrFeedUnsupported) {
		t.Errorf("OS.Foreground of a Feed: %v, want ErrFeedUnsupported", err)
	}
	if _, err := f.Foreground(fed); !errors.Is(err, ErrFeedUnsupported) {
		t.Errorf("Fake.Foreground of a Feed: %v, want ErrFeedUnsupported", err)
	}
	if strings.Contains(fmt.Sprintf("%v %+v %#v", fed, fed, fed), placeholder) {
		t.Error("a Cmd with a Feed printed the secret")
	}
}
