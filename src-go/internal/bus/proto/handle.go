package proto

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Handle-building errors (SPEC P17). The message text is not part of the contract.
var (
	ErrHandle = errors.New("a handle cannot be built from this input")
	ErrSeat   = fmt.Errorf("%w: the seat must be a number from 1 to %d or match %s", ErrHandle, seatMax, handleSeatPattern)
)

const (
	repoMax = 48
	seatMax = 999999
)

var (
	repoUnsafeRE = regexp.MustCompile(`[^a-z0-9_-]`)
	hostUnsafeRE = regexp.MustCompile(`[^a-z0-9-]`)
	remoteSplit  = regexp.MustCompile(`[/:]`)
)

// replaceUnsafe turns every code point the pattern matches into "-", a lone surrogate included.
func replaceUnsafe(re *regexp.Regexp, s string) string {
	var b strings.Builder
	for _, r := range points(s) {
		if r < 0x80 && !re.MatchString(string(r)) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

// NormaliseRepo is SPEC P17: Python lower, outside [a-z0-9_-] to "-", leading "-" and "_"
// stripped, the first 48 kept; empty is refused.
func NormaliseRepo(raw string) (string, error) {
	repo := strings.TrimLeft(replaceUnsafe(repoUnsafeRE, pyLower(raw)), "-_")
	if len(repo) > repoMax {
		repo = repo[:repoMax]
	}
	if repo == "" {
		return "", fmt.Errorf("%w: repository name %q is empty once normalised", ErrHandle, raw)
	}
	return repo, nil
}

// RepoFromRemote is SPEC P17: the last component of the remote ("" for none), else of the
// directory; ".git" is removed only from a remote's.
func RepoFromRemote(remote, dir string) (string, error) {
	source := dir
	if remote != "" {
		source = remote
	}
	parts := remoteSplit.Split(strings.TrimRight(source, "/"), -1)
	last := parts[len(parts)-1]
	if remote != "" {
		last = strings.TrimSuffix(last, ".git")
	}
	return NormaliseRepo(last)
}

// ResolveHost is SPEC P17: the explicit host first, else the install's role ("" for none).
func ResolveHost(explicit, role string) (string, error) {
	raw := explicit
	if raw == "" {
		raw = role
	}
	if raw == "" {
		return "", fmt.Errorf("%w: no role is set for this install: pass --host", ErrHandle)
	}
	host := replaceUnsafe(hostUnsafeRE, pyLower(raw))
	if !hostRE.MatchString(host) {
		return "", fmt.Errorf("%w: host %q does not fit %s once normalised", ErrHandle, raw, handleHostPattern)
	}
	return host, nil
}

func formatHandle(repo, seat, host, typ string) (string, error) {
	handle := repo + "." + seat + "+" + host + "." + typ
	if _, ok := ParseHandle(handle); !ok {
		return "", fmt.Errorf("%w: %q does not fit the handle grammar", ErrHandle, handle)
	}
	return handle, nil
}

// BuildHandle is SPEC P17 for a named seat (or one of digits given as text).
func BuildHandle(repo, seat, host, typ string) (string, error) {
	if !IsSeat(seat) {
		return "", ErrSeat
	}
	return formatHandle(repo, seat, host, typ)
}

// BuildNumberedHandle is SPEC P17 for a counter-issued number, 1 to 999999.
func BuildNumberedHandle(repo string, n int64, host, typ string) (string, error) {
	if n < 1 || n > seatMax {
		return "", ErrSeat
	}
	return formatHandle(repo, strconv.FormatInt(n, 10), host, typ)
}

// PrefixOf is SPEC P17: the counter key <repo>+<host>.<type>.
func PrefixOf(handle string) (string, error) {
	h, ok := ParseHandle(handle)
	if !ok {
		return "", fmt.Errorf("%w: %q is not an agent handle", ErrHandle, handle)
	}
	return h.Repo + "+" + h.Host + "." + h.Type, nil
}
