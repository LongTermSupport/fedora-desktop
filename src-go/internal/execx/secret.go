package execx

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
)

// ErrSecretInArgv is a command whose argv holds a secret's value: argv is world-readable
// (/proc/<pid>/cmdline), so the command is refused before it starts.
var ErrSecretInArgv = errors.New("a secret's value is in the argv")

var envName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// Secret is a value handed to one child in its environment under a name, never in argv: the
// child reads it by name (`podman run -e NAME`, gh's GH_TOKEN). It prints as its name only, so
// a log, an error or a %v of a Cmd cannot show it. It is held in memory and in the
// environment of the child it is given to, and of no other, except that the child's own
// children inherit it, and a value TakeSecret moved stays readable in this process's
// /proc/self/environ (the kernel's copy of the starting environment, which Unsetenv does not
// change).
type Secret struct {
	name, value string
}

// NewSecret is a secret named name; the name must be a variable's and the value not empty.
func NewSecret(name, value string) (Secret, error) {
	if !envName.MatchString(name) {
		return Secret{}, fmt.Errorf("a secret's name must be a variable name, not %q", name)
	}
	if value == "" {
		return Secret{}, fmt.Errorf("secret %s is empty", name)
	}
	return Secret{name: name, value: value}, nil
}

// TakeSecret moves the named variable out of this process's environment into a Secret, so no
// later child inherits it; false when it is unset or empty (an empty one is unset as well).
func TakeSecret(name string) (Secret, bool, error) {
	if !envName.MatchString(name) {
		return Secret{}, false, fmt.Errorf("a secret's name must be a variable name, not %q", name)
	}
	value, set := os.LookupEnv(name)
	if !set {
		return Secret{}, false, nil
	}
	if err := os.Unsetenv(name); err != nil {
		return Secret{}, false, fmt.Errorf("unset %s: %w", name, err)
	}
	if value == "" {
		return Secret{}, false, nil
	}
	return Secret{name: name, value: value}, true, nil
}

// Name is the variable the child reads the secret from.
func (s Secret) Name() string { return s.name }

// Is reports whether the secret's value is v, for a test or a comparison that must not copy it.
func (s Secret) Is(v string) bool { return s.value == v }

// String is the secret as it may be shown: its name, never its value.
func (s Secret) String() string { return s.name + "=<redacted>" }

// GoString is String, so %#v cannot show the value either.
func (s Secret) GoString() string { return s.String() }

// Feed is a secret given to a child on its standard input, inside a line of text (curl's
// `--config -`), so it is in neither the child's argv nor its environment. Only Run feeds a
// child; Foreground and Exec refuse a command with a Feed (ErrFeedUnsupported).
type Feed struct {
	Secret        Secret
	Before, After string // the text around the secret's value
}

// ErrFeedUnsupported is a Feed given to a runner that cannot write a child's standard input.
var ErrFeedUnsupported = errors.New("a secret fed on stdin needs Run")

// fed is the command with its Feed made its Stdin; an error when it has a Stdin as well.
func (c Cmd) fed() (Cmd, error) {
	if c.Feed == nil {
		return c, nil
	}
	if c.Stdin != nil {
		return c, fmt.Errorf("run %s: a command is given both a Feed and a Stdin", c.Name)
	}
	c.Stdin = strings.NewReader(c.Feed.Before + c.Feed.Secret.value + c.Feed.After)
	c.Feed = nil
	return c, nil
}

// refuseFeed is the error of a runner that cannot feed a child.
func (c Cmd) refuseFeed() error {
	if c.Feed != nil {
		return fmt.Errorf("run %s: %w", c.Name, ErrFeedUnsupported)
	}
	return nil
}

// checkSecrets refuses a command whose name or arguments hold a secret's value, a fed one too.
func (c Cmd) checkSecrets() error {
	secrets := c.Secrets
	if c.Feed != nil {
		secrets = append(slices.Clone(secrets), c.Feed.Secret)
	}
	for _, s := range secrets {
		for _, word := range c.argv() {
			if s.value != "" && strings.Contains(word, s.value) {
				return fmt.Errorf("run %s: %w: the value of %s", c.Name, ErrSecretInArgv, s.name)
			}
		}
	}
	return nil
}

// childEnv is the child's environment when the command changes it (Env, Unset, Secrets); false
// when the child simply inherits this process's. A secret is added last, and ChildEnv drops
// every other copy of its name, so the child sees the secret's value and only that.
func (c Cmd) childEnv() ([]string, bool) {
	if len(c.Env) == 0 && len(c.Unset) == 0 && len(c.Secrets) == 0 {
		return nil, false
	}
	add := append([]string(nil), c.Env...)
	for _, s := range c.Secrets {
		add = append(add, s.name+"="+s.value)
	}
	return ChildEnv(os.Environ(), add, c.Unset), true
}
