package ftpcamera

import (
	"context"
	_ "embed" // words.txt is embedded verbatim
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// words.txt is the word list the hotspot password is drawn from: short common lowercase words,
// shipped with the tool so no dictionary package is needed.
//
//go:embed words.txt
var hotspotWordsText string

// The hotspot password's shape (E67): three words joined by a hyphen.
const (
	hotspotPasswordWords = 3
	hotspotSeparator     = "-"
	// minHotspotWords is the fewest words the list may hold.
	minHotspotWords = 800
	// pskField is the NetworkManager setting that holds a Wi-Fi password.
	pskField = "802-11-wireless-security.psk"
)

// hotspotWordShape is 3 to 9 lowercase letters, so any three joined are 11 to 29 characters:
// always a WPA2 passphrase (8 to 63).
var hotspotWordShape = regexp.MustCompile(`^[a-z]{3,9}$`)

// hotspotWords is the shipped word list, checked (E67).
func hotspotWords() ([]string, error) {
	return parseHotspotWords(hotspotWordsText, minHotspotWords)
}

// parseHotspotWords reads a whitespace-separated list, refusing a word of the wrong shape, a
// word listed twice, and fewer than least words.
func parseHotspotWords(text string, least int) ([]string, error) {
	words := strings.Fields(text)
	seen := make(map[string]bool, len(words))
	for _, w := range words {
		if !hotspotWordShape.MatchString(w) {
			return nil, fmt.Errorf("word list: %q is not 3 to 9 lowercase letters", w)
		}
		if seen[w] {
			return nil, fmt.Errorf("word list: %q is listed twice", w)
		}
		seen[w] = true
	}
	if len(words) < least {
		return nil, fmt.Errorf("word list: %d words, want at least %d", len(words), least)
	}
	return words, nil
}

// errNoRandom is a run with no random source.
var errNoRandom = errors.New("no random source")

// newHotspotPassword draws hotspotPasswordWords words uniformly from random and joins them.
func newHotspotPassword(random io.Reader, words []string) (string, error) {
	if random == nil {
		return "", errNoRandom
	}
	picked := make([]string, 0, hotspotPasswordWords)
	for range hotspotPasswordWords {
		i, err := uniformIndex(random, len(words))
		if err != nil {
			return "", err
		}
		picked = append(picked, words[i])
	}
	return strings.Join(picked, hotspotSeparator), nil
}

// drawSpace is how many values one two-byte draw has.
const drawSpace = 1 << 16

// uniformIndex is a uniform index below n (at most drawSpace): a two-byte big-endian draw,
// drawn again at or above the largest multiple of n, so no index is likelier than another.
func uniformIndex(random io.Reader, n int) (int, error) {
	limit := drawSpace - drawSpace%n
	var buf [2]byte
	for {
		if _, err := io.ReadFull(random, buf[:]); err != nil {
			return 0, fmt.Errorf("read random bytes: %w", err)
		}
		if v := int(binary.BigEndian.Uint16(buf[:])); v < limit {
			return v % n, nil
		}
	}
}

// newHotspotPasswordMode prints a new hotspot password on stdout and nothing else (E67).
// play-ftp-camera.yml runs it when no password is saved, and vaults what it prints.
func (c *cli) newHotspotPasswordMode() term.ExitCode {
	words, err := hotspotWords()
	if err != nil {
		return c.fail("could not make a hotspot password: %v", err)
	}
	pass, err := newHotspotPassword(c.env.Random, words)
	if err != nil {
		return c.fail("could not make a hotspot password: %v", err)
	}
	c.out.f("%s\n", pass)
	return term.ExitOK
}

// hotspotPassword is the Wi-Fi password of the NetworkManager profile conn (E68), "" when the
// profile has none. NetworkManager is the one place it is kept that the camera must match, so
// it is asked first as the user, which polkit allows an active local session; only when that
// gives nothing is it asked as root.
func (c *cli) hotspotPassword(ctx context.Context, conn string) (string, error) {
	args := []string{"-s", "-g", pskField, nmConn, nmShow, conn}
	userRes, userErr := c.env.Runner.Run(ctx, execx.Cmd{Name: nmcliBin, Args: args})
	if pass := strings.TrimSpace(string(userRes.Stdout)); userErr == nil && pass != "" {
		return pass, nil
	}
	res, err := c.rootCapture(ctx, append([]string{nmcliBin}, args...)...)
	if err != nil {
		asUser := ""
		if userErr != nil {
			asUser = fmt.Sprintf(" (as the user: %v)", userErr)
		}
		return "", c.failErr("could not read the Wi-Fi password of hotspot '%s': %v %s%s",
			conn, err, strings.TrimSpace(string(res.Stderr)), asUser)
	}
	return strings.TrimSpace(string(res.Stdout)), nil
}
