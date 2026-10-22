package declare

import (
	"math"
	"regexp"
	"strings"
	"unicode"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
)

// The grammars of helpers/pingbus/protocol.py and helpers/agent_bus/registry.py that the
// reader judges values by, and the line forms of declare.py's reader. A pattern is matched
// whole, as python's fullmatch matches it; the patterns are ASCII, as python's are without
// re.IGNORECASE.
const (
	teamNamePattern = `[a-z][a-z0-9-]{0,23}`
	seatPattern     = `[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11}`
	hostPattern     = `[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`
	ownerPattern    = `[a-z0-9](?:[a-z0-9-]{0,37}[a-z0-9])?`
	repoPattern     = `[a-z0-9._-]{1,100}`
	repoMax         = 48
	descriptionMax  = 120
	maxIntDigits    = 4300
)

func whole(pattern string) *regexp.Regexp { return regexp.MustCompile(`^(?:` + pattern + `)$`) }

var (
	teamNameRE  = whole(teamNamePattern)
	seatRE      = whole(seatPattern)
	hostRE      = whole(hostPattern)
	ownerRepoRE = whole(`(` + ownerPattern + `)/(` + repoPattern + `)`)
	containerRE = whole(`[A-Za-z0-9][A-Za-z0-9_.-]{0,63}`)
	userRE      = whole(`[a-z_][a-z0-9_-]{0,31}`)
	intRE       = whole(`-?[0-9]+`)
	remoteRE    = whole(`(?:git@github\.com[^:/]*:|ssh://git@github\.com[^/]*/|https://github\.com/)([^/]+)/([^/]+?)(?:\.git)?/?`)
)

// isOwnerRepo is protocol.is_owner_repo: a lower-case OWNER/REPO whose repo is not "." or
// ".." and does not end ".git".
func isOwnerRepo(s string) bool {
	m := ownerRepoRE.FindStringSubmatch(s)
	return m != nil && m[2] != "." && m[2] != ".." && !strings.HasSuffix(m[2], ".git")
}

// GithubRemote is declare.py github-remote (SPEC-view.md V22): the lower-case OWNER/REPO a
// GitHub remote URL names (an alias host such as github.com-work included), or "".
func GithubRemote(url string) string {
	m := remoteRE.FindStringSubmatch(pycompat.Strip(url))
	if m == nil {
		return ""
	}
	if name := pycompat.Lower(m[1] + "/" + m[2]); isOwnerRepo(name) {
		return name
	}
	return ""
}

// replaceOutside is re.sub("[^<keep>]", "-", s) per code point.
func replaceOutside(s string, keep func(rune) bool) string {
	var b strings.Builder
	for _, r := range pycompat.CodePoints(s) {
		if keep(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
	}
	return b.String()
}

func lowerDigit(r rune) bool { return r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' }

// normaliseRepo is registry.normalise_repo: lower-cased, outside [a-z0-9_-] a dash, leading
// dashes and underscores dropped, at most 48.
func normaliseRepo(raw string) (string, error) {
	repo := replaceOutside(pycompat.Lower(raw), func(r rune) bool { return lowerDigit(r) || r == '_' })
	repo = strings.TrimLeft(repo, "-_")
	if len(repo) > repoMax {
		repo = repo[:repoMax]
	}
	if repo == "" {
		return "", refusedf("repository name %s is empty once normalised", pycompat.PyRepr(pycompat.Str(raw)))
	}
	return repo, nil
}

// resolveHost is registry.resolve_host for an explicit host and no role.
func resolveHost(raw string) (string, error) {
	if raw == "" {
		return "", refusedf("no role is set for this install: pass --host")
	}
	host := replaceOutside(pycompat.Lower(raw), lowerDigit)
	if !hostRE.MatchString(host) {
		return "", refusedf("host %s does not fit %s once normalised", pycompat.PyRepr(pycompat.Str(raw)), hostPattern)
	}
	return host, nil
}

// isDescription is protocol.is_description: 1 to 120 characters, stripped, and no control or
// format character.
func isDescription(s string) bool {
	n := pycompat.Len(s)
	if n < 1 || n > descriptionMax || s != pycompat.Strip(s) {
		return false
	}
	for _, r := range pycompat.CodePoints(s) {
		if unicode.In(r, unicode.Cc, unicode.Cf) {
			return false
		}
	}
	return true
}

// otherDigits are the code points python's str.isdigit accepts that are not decimal digits
// (Numeric_Type=Digit: superscripts, circled digits): int() refuses them. Proven against
// python over every code point by TestOtherDigitsAreIsdigitLessIsdecimal.
var otherDigits = &unicode.RangeTable{
	R16: []unicode.Range16{
		{Lo: 0xB2, Hi: 0xB3, Stride: 1},
		{Lo: 0xB9, Hi: 0xB9, Stride: 1},
		{Lo: 0x1369, Hi: 0x1371, Stride: 1},
		{Lo: 0x19DA, Hi: 0x19DA, Stride: 1},
		{Lo: 0x2070, Hi: 0x2070, Stride: 1},
		{Lo: 0x2074, Hi: 0x2079, Stride: 1},
		{Lo: 0x2080, Hi: 0x2089, Stride: 1},
		{Lo: 0x2460, Hi: 0x2468, Stride: 1},
		{Lo: 0x2474, Hi: 0x247C, Stride: 1},
		{Lo: 0x2488, Hi: 0x2490, Stride: 1},
		{Lo: 0x24EA, Hi: 0x24EA, Stride: 1},
		{Lo: 0x24F5, Hi: 0x24FD, Stride: 1},
		{Lo: 0x24FF, Hi: 0x24FF, Stride: 1},
		{Lo: 0x2776, Hi: 0x277E, Stride: 1},
		{Lo: 0x2780, Hi: 0x2788, Stride: 1},
		{Lo: 0x278A, Hi: 0x2792, Stride: 1},
	},
	R32: []unicode.Range32{
		{Lo: 0x10A40, Hi: 0x10A43, Stride: 1},
		{Lo: 0x10E60, Hi: 0x10E68, Stride: 1},
		{Lo: 0x11052, Hi: 0x1105A, Stride: 1},
		{Lo: 0x1F100, Hi: 0x1F10A, Stride: 1},
	},
	LatinOffset: 2,
}

func isDecimal(r rune) bool   { return pycompat.IsDigitNd(r) }
func isDigitChar(r rune) bool { return isDecimal(r) || unicode.Is(otherDigits, r) }

// portNumber is declare.py's `value.isdigit()` then `int(value)` for the port on line `line`:
// not a number unless every character is a digit; a digit int() refuses, or more than
// python's 4300 digits, is the ValueError v1 crashed on; beyond an int, refused (B-S6).
func portNumber(value string, line int) (int, error) {
	const notNumber = "line %d: the port is not a number"
	rs := pycompat.CodePoints(value)
	if len(rs) == 0 {
		return 0, refusedf(notNumber, line)
	}
	decimal := true
	for _, r := range rs {
		if !isDigitChar(r) {
			return 0, refusedf(notNumber, line)
		}
		decimal = decimal && isDecimal(r)
	}
	n, ok := pycompat.ParseDigits(value)
	if !decimal || !ok {
		return 0, crashf(notNumber, line)
	}
	if !n.IsInt64() || n.Int64() > math.MaxInt {
		return 0, refusedf(notNumber, line)
	}
	return int(n.Int64()), nil
}

// cutComment is re.sub(r"(^|\s)#.*$", "", rest) up to whitespace its callers strip: the text
// before the first '#' that starts it or follows a whitespace character (SPEC-view.md V6).
func cutComment(rest string) string {
	afterSpace := true
	for i, r := range rest {
		if r == '#' && afterSpace {
			return rest[:i]
		}
		afterSpace = pycompat.IsSpace(r)
	}
	return rest
}

// scalar is declare.py's _scalar (SPEC-view.md V7): the comment removed, stripped; a tag
// refused as vaulted; matching outer quotes removed, nothing unescaped.
func scalar(rest, where string) (string, error) {
	value, ok := plainScalar(rest)
	if !ok {
		return "", vaultedf("%s is tagged (vault-encrypted?); ccy-teams reads only plain values", where)
	}
	return value, nil
}

// plainScalar is scalar's value, and false for a tagged one: where v1 caught the refusal and
// read the value as none (the bus address, the first listen address).
func plainScalar(rest string) (string, bool) {
	value := pycompat.Strip(cutComment(rest))
	if strings.HasPrefix(value, "!") {
		return "", false
	}
	if len(value) >= 2 && value[0] == value[len(value)-1] && (value[0] == '"' || value[0] == '\'') {
		value = value[1 : len(value)-1]
	}
	return value, true
}

// keyLine is declare.py's _KEY_LINE_RE, `(?P<key>[a-z_]+):(?P<rest>(?:\s.*)?)`, matched whole.
func keyLine(body string) (string, string, bool) {
	n := 0
	for n < len(body) && (body[n] >= 'a' && body[n] <= 'z' || body[n] == '_') {
		n++
	}
	if n == 0 || n == len(body) || body[n] != ':' {
		return "", "", false
	}
	rest := body[n+1:]
	if rest != "" && !startsWithSpace(rest) {
		return "", "", false
	}
	return body[:n], rest, true
}

func startsWithSpace(s string) bool {
	for _, r := range s {
		return pycompat.IsSpace(r)
	}
	return false
}

// keyRest is `<key>\s*:(?P<rest>.*)` matched whole: the rest of a top-level key's line.
func keyRest(bare, key string) (string, bool) {
	after, ok := strings.CutPrefix(bare, key)
	if !ok {
		return "", false
	}
	after = strings.TrimLeftFunc(after, pycompat.IsSpace)
	return strings.CutPrefix(after, ":")
}

// bare is line.rstrip("\r\n").
func bare(line string) string { return strings.TrimRight(line, "\r\n") }

// spaces is len(s) - len(s.lstrip(" ")).
func spaces(s string) int { return len(s) - len(strings.TrimLeft(s, " ")) }

// printable refuses text python could not write to its UTF-8 stdout: a lone surrogate, which
// raised UnicodeEncodeError in v1 when the line was printed.
func printable(texts ...string) error {
	for _, text := range texts {
		for _, r := range pycompat.CodePoints(text) {
			if r >= 0xD800 && r <= 0xDFFF {
				return crashf("%s holds a lone surrogate, which cannot be printed", pycompat.PyRepr(pycompat.Str(text)))
			}
		}
	}
	return nil
}
