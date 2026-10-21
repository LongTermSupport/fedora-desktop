package seat

// This is member.json's decoder, config.read_member's rules (SPEC-view V32). It lives here
// only until internal/bus/state is built: that package will own the member.json decoder,
// and this file moves there.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
)

const (
	memberFile         = "member.json"
	memberFileMaxBytes = 65536
	protocolVersion    = 1
	adminLocalpart     = "admin"
	tokenFile          = "token"
	dnsNameMax         = 253
	unknownKeysShown   = 5
)

// requiredKeys are member.json's required keys in v1's order: a missing one is named first
// by this order.
var requiredKeys = []string{
	"protocol", "team", "user_id", "server_name", "base_url", "plain_http_hosts", "token_file", "admin", "room",
}

var optionalKeys = []string{"human_text", "limits"}

// limitBounds are limits.BOUNDS, inclusive, in v1's order (its refusal lists them so).
var limitBounds = []struct {
	key       string
	low, high int64
}{
	{"send_per_minute", 1, 60},
	{"send_burst", 1, 30},
	{"recv_per_sender_minute", 10, 600},
	{"ack_timeout_s", 60, 86400},
	{"halt_ack_timeout_s", 30, 3600},
	{"human_max_age_s", 3600, 604800},
	{"wait_timeout_s", 1, 1790},
	{"report_per_hour", 1, 60},
}

const dnsLabel = `[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`

// The patterns of config.py and protocol.py, anchored as python's fullmatch anchors them.
var (
	dnsNameRE   = regexp.MustCompile(`^` + dnsLabel + `(?:\.` + dnsLabel + `)*$`)
	keyNameRE   = regexp.MustCompile(`^[a-z_]{1,32}$`)
	printableRE = regexp.MustCompile(`^[\x21-\x7e]+$`)
	roomIDRE    = regexp.MustCompile(`^![A-Za-z0-9_-]{43}$`)
	handleRE    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}\.(?:[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11})\+` +
		dnsLabel + `\.(?:podman|lxc|docker|vm|host)$`)
)

// readMember is bundleDir/member.json validated for team, as config.read_member reads it,
// and its handle (the user ID's localpart). A refusal is a *RefusalError with v1's words; what v1
// did not catch (a RecursionError, urlsplit's ValueError) is a plain error.
func readMember(team, bundleDir string) (string, error) {
	path := filepath.Join(bundleDir, memberFile)
	where := "team " + team + ": " + pathText(path)
	data, err := readMemberFile(where, path)
	if err != nil {
		return "", err
	}
	return parseMember(team, where, member{data})
}

// readMemberFile opens without waiting on a FIFO, following a symlink, and reads at most one
// byte past the cap (config._read_member_file).
func readMemberFile(where, path string) (*pycompat.Dict, error) {
	file, err := os.OpenFile(filepath.Clean(path), os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		return nil, refusalf("%s: no bundle (member.json is missing)", where)
	}
	if err != nil {
		return nil, cannotRead(where, err)
	}
	raw, err := readCapped(file)
	if closeErr := file.Close(); err == nil && closeErr != nil {
		err = closeErr
	}
	if err != nil {
		if errors.Is(err, errNotRegular) {
			return nil, refusalf("%s: member.json is not a regular file", where)
		}
		return nil, cannotRead(where, err)
	}
	if len(raw) > memberFileMaxBytes {
		return nil, refusalf("%s: member.json is larger than %d bytes", where, memberFileMaxBytes)
	}
	value, err := pycompat.Loads(string(raw), pycompat.LoadOptions{NoDuplicateKeys: true})
	if err != nil {
		return nil, notJSON(where, err)
	}
	data, ok := value.(*pycompat.Dict)
	if !ok {
		return nil, refusalf("%s: member.json is not a JSON object", where)
	}
	return data, nil
}

var errNotRegular = errors.New("not a regular file")

// readCapped reads the open file as v1 did: python's fdopen refuses a directory (EISDIR, so
// "cannot be read (Is a directory)") before the regular-file check sees it.
func readCapped(file *os.File) ([]byte, error) {
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.IsDir() {
		return nil, syscall.EISDIR
	}
	if !info.Mode().IsRegular() {
		return nil, errNotRegular
	}
	return io.ReadAll(io.LimitReader(file, memberFileMaxBytes+1))
}

// cannotRead is v1's refusal of an OSError reading the file, with its strerror; an error
// that carries no errno was not an OSError, so it is a crash.
func cannotRead(where string, err error) error {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return fmt.Errorf("%s: read member.json: %w", where, err)
	}
	return refusalf("%s: member.json cannot be read (%s)", where, strerror(errno))
}

// notJSON is v1's refusal of what json.loads raised: UnicodeDecodeError and ValueError (with
// its JSONDecodeError) are caught and named; a RecursionError is not, so it is a crash.
func notJSON(where string, err error) error {
	if pe, ok := errors.AsType[*pycompat.Error](err); ok {
		switch pe.Class {
		case pycompat.ClassJSONDecodeError, pycompat.ClassValueError, pycompat.ClassUnicodeDecodeError:
			return refusalf("%s: member.json is not valid JSON (%s)", where, pe.Class)
		case pycompat.ClassRecursionError, pycompat.ClassUnicodeEncodeError, pycompat.ClassTypeError:
		}
	}
	return fmt.Errorf("%s: member.json: %w", where, err)
}

// member is a decoded member.json object.
type member struct{ *pycompat.Dict }

// value is the key's value, None when absent (only asked of keys checkKeys found present,
// or of optional ones whose absence the caller has asked first).
func (m member) value(key string) pycompat.Value {
	if v, ok := m.Get(key); ok {
		return v
	}
	return pycompat.None{}
}

func (m member) has(key string) bool {
	_, ok := m.Get(key)
	return ok
}

// parseMember is config._parse_member, in its order; it returns the handle.
func parseMember(team, where string, data member) (string, error) {
	refuse := func(key, why string) error { return refusalf("%s: %s %s", where, key, why) }
	if err := checkKeys(data, refuse); err != nil {
		return "", err
	}
	if v, ok := data.value("protocol").(pycompat.Int); !ok || !v.V.IsInt64() || v.V.Int64() != protocolVersion {
		return "", refuse("protocol", fmt.Sprintf("must be the integer %d", protocolVersion))
	}
	if !pycompat.Equal(data.value("team"), pycompat.Str(team)) {
		return "", refuse("team", fmt.Sprintf("does not equal the bundle directory's name (%s)", team))
	}
	server, ok := data.value("server_name").(pycompat.Str)
	if !ok || !isDNSName(string(server)) {
		return "", refuse("server_name", "is not a lowercase DNS name")
	}
	handle, ok := agentHandle(data.value("user_id"), string(server))
	if !ok {
		return "", refuse("user_id", "is not @<handle>:<server_name> for this server_name")
	}
	if !pycompat.Equal(data.value("admin"), pycompat.Str("@"+adminLocalpart+":"+string(server))) {
		return "", refuse("admin", "is not @"+adminLocalpart+":<server_name>")
	}
	if room, ok := data.value("room").(pycompat.Str); !ok || !roomIDRE.MatchString(string(room)) {
		return "", refuse("room", "is not a room ID")
	}
	if !pycompat.Equal(data.value("token_file"), pycompat.Str(tokenFile)) {
		return "", refuse("token_file", `must be "`+tokenFile+`", the file inside the bundle`)
	}
	return handle, checkTail(data, where, refuse)
}

// checkTail is the rest of _parse_member after token_file: the hosts, the URL, human_text and
// the limits.
func checkTail(data member, where string, refuse func(key, why string) error) error {
	hosts, err := plainHTTPHosts(data.value("plain_http_hosts"), refuse)
	if err != nil {
		return err
	}
	if err := checkBaseURL(data.value("base_url"), hosts, refuse); err != nil {
		return err
	}
	if data.has("human_text") {
		if _, ok := data.value("human_text").(pycompat.Bool); !ok {
			return refuse("human_text", "must be true or false")
		}
	}
	return checkLimits(data, where)
}

func checkKeys(data member, refuse func(key, why string) error) error {
	var unknown []string
	for _, key := range data.Keys() {
		if !slices.Contains(requiredKeys, key) && !slices.Contains(optionalKeys, key) {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		// WTF-8 byte order is code point order, which is python's sorted().
		slices.Sort(unknown)
		labels := make([]string, 0, unknownKeysShown)
		for _, key := range unknown[:min(len(unknown), unknownKeysShown)] {
			labels = append(labels, keyLabel(key))
		}
		return refuse("member.json", "has unknown keys: "+strings.Join(labels, ", "))
	}
	for _, key := range requiredKeys {
		if !data.has(key) {
			return refuse(key, "is missing")
		}
	}
	return nil
}

// keyLabel echoes a key only when it looks like a key name, never like a pasted value.
func keyLabel(key string) string {
	if keyNameRE.MatchString(key) {
		return pycompat.PyRepr(pycompat.Str(key))
	}
	return "a key that is not a name"
}

func isDNSName(s string) bool {
	return pycompat.Len(s) <= dnsNameMax && dnsNameRE.MatchString(s)
}

// agentHandle is protocol.parse_user_id followed by parse_handle: the localpart of
// @<localpart>:<server> on exactly this server, when it is an agent handle.
func agentHandle(v pycompat.Value, server string) (string, bool) {
	s, ok := v.(pycompat.Str)
	if !ok || !strings.HasPrefix(string(s), "@") {
		return "", false
	}
	localpart, rest, found := strings.Cut(string(s)[1:], ":")
	if !found || rest != server || !handleRE.MatchString(localpart) {
		return "", false
	}
	return localpart, true
}

func plainHTTPHosts(v pycompat.Value, refuse func(key, why string) error) ([]string, error) {
	list, ok := v.(pycompat.List)
	if !ok {
		return nil, refuse("plain_http_hosts", "must be an array of IP literals")
	}
	hosts := make([]string, 0, len(list))
	for _, item := range list {
		entry, ok := item.(pycompat.Str)
		if !ok || strings.Contains(string(entry), "%") {
			return nil, refuse("plain_http_hosts", "must hold only IP literals")
		}
		canonical, ok := pycompat.IPAddress(string(entry))
		if !ok {
			return nil, refuse("plain_http_hosts", "must hold only IP literals")
		}
		if canonical != string(entry) {
			return nil, refuse("plain_http_hosts", "must hold IP literals in canonical form")
		}
		if slices.Contains(hosts, canonical) {
			return nil, refuse("plain_http_hosts", "lists an address twice")
		}
		hosts = append(hosts, canonical)
	}
	return hosts, nil
}

// checkLimits is config._limits over limits.parse_limits: absent is the defaults; present it
// is an object of known keys, each an integer (not a bool) in bounds, judged in file order.
func checkLimits(data member, where string) error {
	if !data.has("limits") {
		return nil
	}
	limits, ok := data.value("limits").(*pycompat.Dict)
	if !ok {
		return refusalf("%s: limits must be an object", where)
	}
	keys := make([]string, 0, len(limitBounds))
	for _, b := range limitBounds {
		keys = append(keys, b.key)
	}
	for _, key := range limits.Keys() {
		if !slices.Contains(keys, key) {
			return refusalf("%s: limits may hold only these keys: %s", where, strings.Join(keys, ", "))
		}
	}
	for _, item := range limits.Items() {
		b := limitBounds[slices.Index(keys, item.Key)]
		n, ok := item.Value.(pycompat.Int)
		if !ok || !n.V.IsInt64() || n.V.Int64() < b.low || n.V.Int64() > b.high {
			return refusalf("%s: limits.%s must be an integer from %d to %d", where, b.key, b.low, b.high)
		}
	}
	return nil
}
