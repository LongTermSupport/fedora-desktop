package ccylaunch

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tokens"
)

// How the bash launcher reads the token pool, where it can differ from the tokens package: the
// front answers a token flag only when every file in the pool reads the same both ways, and
// hands the run to the bash otherwise (F4).

const tokenSuffix = ".token"

// bashDate is the date the bash finds in a token file's name (token-management.bash).
var bashDate = regexp.MustCompile(`([0-9]{4}-[0-9]{2}-[0-9]{2})\.token$`)

// bashName is ${fn%.*.token}: fn less its shortest suffix of the form .<anything>.token.
func bashName(fn string) string {
	if !strings.HasSuffix(fn, tokenSuffix) {
		return fn
	}
	for i := len(fn) - len("."+tokenSuffix); i >= 0; i-- {
		if fn[i] == '.' {
			return fn[:i]
		}
	}
	return fn
}

// readsAlike reports whether the bash and the tokens package read the file name the same: both
// dated, on the same day and with the same name, or both undated.
func readsAlike(fn string) bool {
	f, ok := tokens.ParseFileName(fn)
	m := bashDate.FindStringSubmatch(fn)
	if !ok || !f.Dated {
		return m == nil
	}
	return m != nil && m[1] == f.Expiry.Format(time.DateOnly) && bashName(fn) == f.Name
}

// isTokenFile is a name the bash's "$dir"/*.token matches: no leading dot, ending .token.
func isTokenFile(name string) bool {
	return strings.HasSuffix(name, tokenSuffix) && !strings.HasPrefix(name, ".")
}

// tokenGlob is the names in dir that match, sorted by bytes, as a glob lists them; exists is
// false for a directory that is not there. why is set when the bash could read the directory
// differently: it is not a readable directory, or a matching entry is not a regular file (the
// bash's tests and its ls treat those each their own way).
func tokenGlob(dir string, match func(string) bool) ([]string, bool, string) {
	var names []string
	entries, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, ""
	}
	if err != nil {
		return nil, false, "the token directory cannot be listed: " + err.Error()
	}
	for _, e := range entries {
		if !match(e.Name()) {
			continue
		}
		info, statErr := os.Stat(filepath.Join(dir, e.Name()))
		if statErr != nil || !info.Mode().IsRegular() {
			return nil, true, "a token file is not a regular file: " + e.Name()
		}
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names, true, ""
}

// collationIsBytes reports whether a glob sorts by bytes: the collation (LC_ALL, else
// LC_COLLATE, else LANG) is C, POSIX or C.<charset>, or none is set.
func collationIsBytes(getenv func(string) string) bool {
	locale := ""
	for _, name := range []string{"LC_ALL", "LC_COLLATE", "LANG"} {
		if v := getenv(name); v != "" {
			locale = v
			break
		}
	}
	return locale == "" || locale == "C" || locale == "POSIX" || strings.HasPrefix(locale, "C.")
}

// plainTokenName is a name every collation sorts as bytes do: lower-case letters and a date.
var plainTokenName = regexp.MustCompile(`^[a-z]+\.[0-9]{4}-[0-9]{2}-[0-9]{2}\.token$`)

// orderAlike reports whether the bash's glob lists names in byte order: always under a C
// collation, and under another only when every name is plain.
func orderAlike(names []string, bytesOrder bool) bool {
	return bytesOrder || !slices.ContainsFunc(names, func(n string) bool { return !plainTokenName.MatchString(n) })
}

// zoneReadsAlike reports whether the bash's date and this program read the same local zone:
// TZ unset or empty (both read the same default), or a zone Go loads; neither TZDIR nor
// ZONEINFO set, which only one of the two reads.
func zoneReadsAlike(getenv func(string) string) bool {
	if getenv("TZDIR") != "" || getenv("ZONEINFO") != "" {
		return false
	}
	tz := strings.TrimPrefix(getenv("TZ"), ":")
	if tz == "" {
		return true
	}
	if filepath.IsAbs(tz) {
		info, err := os.Stat(tz)
		return err == nil && info.Mode().IsRegular()
	}
	_, err := time.LoadLocation(tz)
	return err == nil
}

const (
	renewSoonDays = 14
	secondsPerDay = 86400
)

// expiryClass is the colour the bash gives an expiry that many days away: 0 red, 1 yellow, 2
// green (token-management.bash's colorize_expiry, the bounds of tokens.ColourExpiry).
func expiryClass(days int64) int {
	switch {
	case days <= 0:
		return 0
	case days <= renewSoonDays:
		return 1
	default:
		return 2
	}
}

// colourAlike reports whether the bash colours a YYYY-MM-DD expiry as the tokens package does
// today: the bash counts the seconds between the two local midnights and truncates to days,
// which a day the clocks change moves, and a midnight the zone skips it cannot count at all.
func colourAlike(date string, now time.Time) bool {
	expiry, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return false
	}
	loc := now.Location()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	to := time.Date(expiry.Year(), expiry.Month(), expiry.Day(), 0, 0, 0, 0, loc)
	if from.Hour() != 0 || to.Hour() != 0 {
		return false
	}
	bashDays := (to.Unix() - from.Unix()) / secondsPerDay
	goDays := int64(tokens.File{Expiry: expiry, Dated: true}.DaysLeft(now))
	return expiryClass(bashDays) == expiryClass(goDays)
}
