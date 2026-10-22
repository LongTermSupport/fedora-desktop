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

// How the bash launcher reads the token pool, where it can differ from the tokens package in
// what a file means: the front answers a token flag only when every file in the pool means the
// same both ways, and hands the run to the bash otherwise (F4). A difference in display only
// is the front's own, declared in the package doc (D1 to D9).

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

// tokenGlob is the names in dir that match, sorted by bytes; exists is false for a directory
// that is not there. why is set when the bash could read the directory differently: it is not a
// readable directory, or a matching entry is not a regular file (the bash's tests and its ls
// treat those each their own way).
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
