package ghaccount

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
)

const accountsKey = "github_accounts"

// account is one configured GitHub account: the alias the play builds its SSH host and helpers
// from, and the GitHub login.
type account struct {
	alias, username string
}

// configMissingError is a configuration file that is not there (E7).
type configMissingError struct{ path string }

func (e configMissingError) Error() string { return "Config not found: " + e.path }

// readAccounts is the accounts of the configuration file at path (E7).
func readAccounts(path string) ([]account, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, configMissingError{path}
	}
	if err != nil {
		return nil, fmt.Errorf("cannot read %s: %w", path, err)
	}
	accounts, err := parseAccounts(data)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return accounts, nil
}

// parseAccounts reads the top-level github_accounts mapping, in file order. Tags such as
// !vault are data here: only the accounts are read, and nothing is decrypted.
func parseAccounts(data []byte) ([]account, error) {
	value, err := accountsNode(data)
	if err != nil {
		return nil, err
	}
	switch {
	case value.Kind == yaml.AliasNode:
		return nil, errors.New(accountsKey + " must be written out, not a YAML alias")
	case value.Kind == yaml.ScalarNode && value.Tag == "!!null":
		return nil, nil
	case value.Kind != yaml.MappingNode:
		return nil, errors.New(accountsKey + " is not a mapping of alias: username")
	}
	var out []account
	for i := 0; i+1 < len(value.Content); i += 2 {
		key, val := value.Content[i], value.Content[i+1]
		alias := key.Value
		if key.Kind != yaml.ScalarNode || !validAlias(alias) {
			return nil, fmt.Errorf("%s: '%s' is not an alias: letters, digits, '_' and '-', starting with a letter or digit", accountsKey, alias)
		}
		if slices.ContainsFunc(out, func(a account) bool { return strings.EqualFold(a.alias, alias) }) {
			return nil, fmt.Errorf("%s: the alias '%s' is given twice", accountsKey, alias)
		}
		if val.Kind != yaml.ScalarNode || (val.Tag != "!!str" && val.Tag != "!!int") {
			return nil, fmt.Errorf("%s: the username of '%s' is not a plain value (a null or a vaulted value cannot be used)", accountsKey, alias)
		}
		if !validLogin(val.Value) {
			return nil, fmt.Errorf("%s: the username of '%s', '%s', is not a GitHub username", accountsKey, alias, val.Value)
		}
		out = append(out, account{alias: alias, username: val.Value})
	}
	return out, nil
}

// accountsNode is the value of the top-level github_accounts key, a null when there is none.
func accountsNode(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("not YAML: %w", err)
	}
	absent := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null"}
	if doc.Kind == 0 {
		return absent, nil
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, errors.New("not a YAML mapping")
	}
	top := doc.Content[0]
	var found *yaml.Node
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value != accountsKey {
			continue
		}
		if found != nil {
			return nil, errors.New(accountsKey + " is given twice")
		}
		found = top.Content[i+1]
	}
	if found == nil {
		return absent, nil
	}
	return found, nil
}

// addToConfig adds the account to the configuration file at path (E23): the file a symlink
// names is the one changed, replaced whole, its mode kept; nothing is written unless the
// result reads back as the old accounts plus the new one.
func addToConfig(path string, a account) error {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		return fmt.Errorf("cannot resolve %s: %w", path, err)
	}
	info, err := os.Stat(target)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return fmt.Errorf("cannot read %s: %w", path, err)
	}
	out, err := addAccountText(data, a)
	if err != nil {
		return fmt.Errorf("%s: %w", path, err)
	}
	if err := fsx.WriteAtomic(target, "configuration", out, info.Mode().Perm()); err != nil {
		return fmt.Errorf("cannot write %s: %w", path, err)
	}
	// The umask narrowed the new file's mode; the old file's is kept.
	if err := os.Chmod(target, info.Mode().Perm()); err != nil {
		return fmt.Errorf("cannot keep the mode of %s: %w", path, err)
	}
	return nil
}

// addAccountText is the file's text with the entry added, proven by reading it back.
func addAccountText(data []byte, a account) ([]byte, error) {
	before, err := parseAccounts(data)
	if err != nil {
		return nil, err
	}
	if slices.ContainsFunc(before, func(b account) bool { return strings.EqualFold(b.alias, a.alias) }) {
		return nil, fmt.Errorf("the alias '%s' is already there", a.alias)
	}
	out := insertEntry(string(data), a)
	after, err := parseAccounts([]byte(out))
	if err != nil {
		return nil, fmt.Errorf("adding %s would break the file (%w); add '%s: \"%s\"' under %s by hand", a.alias, err, a.alias, a.username, accountsKey)
	}
	if !slices.Equal(after, append(slices.Clone(before), a)) {
		return nil, fmt.Errorf("cannot add %s to this %s; add '%s: \"%s\"' under it by hand", a.alias, accountsKey, a.alias, a.username)
	}
	return []byte(out), nil
}

// insertEntry places the entry: after the last entry line of a top-level block, as the block
// when the key has an empty value, or appended with a new key. What it writes is judged by
// addAccountText, not trusted.
func insertEntry(text string, a account) string {
	lines := strings.SplitAfter(text, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	if n := len(lines); n > 0 && !strings.HasSuffix(lines[n-1], "\n") {
		lines[n-1] += "\n"
	}
	keyAt := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, accountsKey+":") })
	if keyAt < 0 {
		sep := ""
		if len(lines) > 0 {
			sep = "\n"
		}
		return strings.Join(lines, "") + sep + accountsKey + ":\n  " + entryText(a) + "\n"
	}
	if value := strings.TrimSpace(stripComment(strings.TrimPrefix(lines[keyAt], accountsKey+":"))); slices.Contains([]string{"{}", "null", "~"}, value) {
		lines[keyAt] = accountsKey + ":\n"
	}
	indent, last := "  ", keyAt
	for i := keyAt + 1; i < len(lines); i++ {
		l := lines[i]
		trimmed := strings.TrimSpace(l)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(l, " ") {
			break
		}
		if last == keyAt {
			indent = l[:len(l)-len(strings.TrimLeft(l, " "))]
		}
		last = i
	}
	entry := indent + entryText(a) + "\n"
	return strings.Join(slices.Insert(lines, last+1, entry), "")
}

func entryText(a account) string { return a.alias + `: "` + a.username + `"` }

// stripComment drops a trailing # comment from a YAML value that holds no quote.
func stripComment(s string) string {
	if strings.ContainsAny(s, `"'`) {
		return s
	}
	return strings.SplitN(s, " #", 2)[0]
}
