// Package tokens is the ccy launcher's token pool, shared with cc (Plan 00191 Task 4.5, phase
// 1.3 of ASSESS-ccy-launcher.md): a token file's name and expiry stamp, whether it may be used,
// how its expiry is shown, the other files that shadow a name, the pool listing, and the
// launcher's answer to a pool with nothing usable in it. It reads names and directories only;
// a token's value is never read here.
//
// Intended behaviour, each held by a test (bash, where it differs, is noted):
//
//   - T1. A token file is <name>.<YYYY-MM-DD>.token, the date a calendar day; any other
//     *.token file is undated and never valid. Bash matched the date anywhere before .token
//     (work2030-06-01.token was dated) and compared it as text (2030-13-45 was valid).
//   - T2. A file is valid only while its date is after today: a file stamped today is
//     expired. Days are counted between calendar dates, so the time of day and the zone do
//     not move the boundary.
//   - T3. The expiry is red when the file cannot be used, yellow within 14 days, green beyond,
//     and plain when the output has no colour. Bash coloured whatever it wrote to.
//   - T4. A name is letters, digits, _ and - only.
//   - T5. A name's other files are its other <name>.<date>.token files, never an undated file,
//     another name or a name that merely starts with this one; a name launches with its
//     earliest-dated file, and the shadow warning names the rest. Bash's --token NAME globbed
//     NAME.*.token, so an undated NAME.x.token could be picked; here it never is.
//   - T6. The listing names each token file with its expiry and status, or says there are none.
//   - T7. A pool with no usable token: the container launcher refuses (a missing directory is
//     a failure, exit 1; an empty pool or an expired-only pool exit 3); the host launcher falls
//     through to the Desktop account only for a pool that is missing or empty, and refuses an
//     expired-only pool, which would otherwise sign in as another account without a word.
//   - T8. The import's CCY_TOKEN_IMPORT_REMOVE_OTHERS is 1 (remove), 0 (keep) or unset (ask);
//     any other value refuses with exit 64 before anything is written.
package tokens

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"
)

// Suffix ends every token file's name.
const Suffix = ".token"

// dateLayout is the expiry stamp's form.
const dateLayout = "2006-01-02"

// ErrBadName is a token name outside letters, digits, _ and -.
var ErrBadName = errors.New("a token name is letters, digits, _ and - only")

var namePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidName refuses a name outside letters, digits, _ and -: a name is written into file names
// and into the import script.
func ValidName(name string) error {
	if !namePattern.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrBadName, name)
	}
	return nil
}

// File is a token file's name read: the token's name, and its expiry when the file is dated.
type File struct {
	FileName string
	Name     string
	Expiry   time.Time // midnight UTC of the stamp's day; zero when not Dated
	Dated    bool
}

// ParseFileName reads a token file's name; false when it is not a *.token name with something
// before the suffix.
func ParseFileName(fileName string) (File, bool) {
	stem, ok := strings.CutSuffix(fileName, Suffix)
	if !ok || stem == "" || strings.HasPrefix(stem, ".") {
		return File{}, false
	}
	f := File{FileName: fileName, Name: stem}
	dot := strings.LastIndexByte(stem, '.')
	if dot <= 0 {
		return f, true
	}
	expiry, err := time.Parse(dateLayout, stem[dot+1:])
	if err != nil || expiry.Format(dateLayout) != stem[dot+1:] {
		return f, true
	}
	f.Name, f.Expiry, f.Dated = stem[:dot], expiry, true
	return f, true
}

// FileName is the file a token of this name expiring on that day is stored in.
func FileName(name string, expiry time.Time) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	return name + "." + expiry.Format(dateLayout) + Suffix, nil
}

// calendarDay is t's date in its own zone, as midnight UTC, so two days subtract whole.
func calendarDay(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// DaysLeft is how many days after today the file expires: 0 today, negative once past.
func (f File) DaysLeft(today time.Time) int {
	return int(f.Expiry.Sub(calendarDay(today)).Hours() / 24)
}

// Status is whether a token file may be used today.
type Status int

// A file's states. Only StatusValid may be used.
const (
	StatusUndated Status = iota
	StatusValid
	StatusExpiresToday
	StatusExpired
)

func (s Status) String() string {
	switch s {
	case StatusValid:
		return "✓ Valid"
	case StatusExpiresToday:
		return "⚠ Expires TODAY"
	case StatusExpired:
		return "✗ EXPIRED"
	case StatusUndated:
	}
	return "✗ INVALID FORMAT (missing expiry date)"
}

// Status is the file's state today.
func (f File) Status(today time.Time) Status {
	switch days := f.DaysLeft(today); {
	case !f.Dated:
		return StatusUndated
	case days > 0:
		return StatusValid
	case days == 0:
		return StatusExpiresToday
	default:
		return StatusExpired
	}
}

// IsValid reports whether the token file named may be used today.
func IsValid(fileName string, today time.Time) bool {
	f, ok := ParseFileName(fileName)
	return ok && f.Status(today) == StatusValid
}

// The expiry colours.
const (
	red    = "\x1b[31m"
	yellow = "\x1b[33m"
	green  = "\x1b[32m"
	reset  = "\x1b[0m"
)

// renewSoon is how many days before its expiry a token shows yellow.
const renewSoon = 14

// ColourExpiry is a YYYY-MM-DD expiry as the listing shows it: red when it cannot be used,
// yellow within renewSoon days, green beyond; unchanged when colour is off or it is not a date.
func ColourExpiry(date string, today time.Time, colour bool) string {
	expiry, err := time.Parse(dateLayout, date)
	if !colour || err != nil {
		return date
	}
	switch days := (File{Expiry: expiry, Dated: true}).DaysLeft(today); {
	case days <= 0:
		return red + date + reset
	case days <= renewSoon:
		return yellow + date + reset
	default:
		return green + date + reset
	}
}

// ExpiryLabel is " (expires: <date>)" for a dated token file, nothing for any other: every line
// that names the token a launch will use carries it.
func ExpiryLabel(fileName string, today time.Time, colour bool) string {
	f, ok := ParseFileName(fileName)
	if !ok || !f.Dated {
		return ""
	}
	return " (expires: " + ColourExpiry(f.Expiry.Format(dateLayout), today, colour) + ")"
}

// datedFiles is the entries that are dated files of the name, sorted.
func datedFiles(entries []string, name string) []string {
	var got []string
	for _, e := range entries {
		if f, ok := ParseFileName(e); ok && f.Dated && f.Name == name {
			got = append(got, e)
		}
	}
	slices.Sort(got)
	return got
}

// Others is the other dated files of the chosen file's name among entries, sorted; nil when
// chosen is not a token file.
func Others(entries []string, chosen string) []string {
	f, ok := ParseFileName(chosen)
	if !ok {
		return nil
	}
	return slices.DeleteFunc(datedFiles(entries, f.Name), func(e string) bool { return e == chosen })
}

// Earliest is the file a name launches with: its earliest-dated file.
func Earliest(entries []string, name string) (string, bool) {
	files := datedFiles(entries, name)
	if len(files) == 0 {
		return "", false
	}
	return files[0], true
}

// ShadowWarning is the warning, for stderr, that other files share the chosen file's name;
// empty when there are none.
func ShadowWarning(dir, chosen string, others []string) string {
	f, ok := ParseFileName(chosen)
	if !ok || len(others) == 0 {
		return ""
	}
	return fmt.Sprintf("⚠  Token %s has %d other file(s): %s\n", f.Name, len(others), strings.Join(others, " ")) +
		fmt.Sprintf("   ccy uses the earliest-dated, %s. Remove the file you do not want from %s\n", chosen, dir)
}
