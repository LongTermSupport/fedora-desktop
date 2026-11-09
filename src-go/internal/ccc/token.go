package ccc

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tokens"
)

const (
	// tokenName is the pool name ccc signs in under and launches with by default.
	tokenName = "ccc"
	// tokenLifeDays is the expiry stamped on a new token: the estimate ccy's create_token uses,
	// since setup-token does not say when its token expires.
	tokenLifeDays = 90
	// renewSoon is how many days before its expiry a token is mentioned at launch, the same
	// window the pool listing shows yellow in.
	renewSoon = 14
	// maxSignIns is how many sign-ins one launch tries before giving up.
	maxSignIns = 3
	// oatPrefix starts every Claude Code long-lived OAuth token.
	oatPrefix = "sk-ant-oat01-"
)

var (
	tokenPattern  = regexp.MustCompile(oatPrefix + `[A-Za-z0-9_-]{20,}`)
	escapePattern = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)
)

// poolDir is the token pool ccy, cc and speech-to-text already share.
func poolDir(home string) string {
	return filepath.Join(home, ".claude-tokens", "ccy", "tokens")
}

// pickToken is the usable file of the name with the latest expiry; false when there is none.
func pickToken(files []string, name string, today time.Time) (tokens.File, bool) {
	var best tokens.File
	found := false
	for _, file := range files {
		f, ok := tokens.ParseFileName(file)
		if !ok || f.Name != name || f.Status(today) != tokens.StatusValid {
			continue
		}
		if !found || f.Expiry.After(best.Expiry) {
			best, found = f, true
		}
	}
	return best, found
}

// readToken is a token file's value, refused when it is not a Claude Code OAuth token.
func readToken(path string) (string, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return "", fmt.Errorf("read the token file: %w", err)
	}
	value := strings.TrimSpace(string(data))
	if !tokenPattern.MatchString(value) || tokenPattern.FindString(value) != value {
		return "", fmt.Errorf("%s is not a Claude Code sign-in token", path)
	}
	return value, nil
}

// extractToken finds the token a sign-in printed, through the terminal's escapes.
func extractToken(output string) (string, bool) {
	value := tokenPattern.FindString(escapePattern.ReplaceAllString(output, ""))
	return value, value != ""
}

// saveToken stores a new token in the pool as <name>.<today + tokenLifeDays>.token, mode 0600.
func saveToken(dir, name, value string, today time.Time) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("make the token directory: %w", err)
	}
	fileName, err := tokens.FileName(name, today.AddDate(0, 0, tokenLifeDays))
	if err != nil {
		return "", err
	}
	path := filepath.Join(dir, fileName)
	if err := fsx.WriteAtomic(path, "the token", []byte(value+"\n"), 0o600); err != nil {
		return "", err
	}
	return path, nil
}
