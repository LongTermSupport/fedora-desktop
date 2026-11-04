package release

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/releasever"
)

// changelogTitle heads a changelog the first release makes.
const changelogTitle = "# Changelog"

// isoDate is a calendar date, as `date -u +%F` writes it.
var isoDate = regexp.MustCompile(`^[0-9]{4}-[0-9]{2}-[0-9]{2}$`)

// Prepend is the changelog with the entry for version first, under the title: a heading line
// (version and date), then one list line per item, as scripts/release.bash writes it. The rest
// of the changelog is kept as it was. A version that already has an entry, an entry with
// nothing in it, and a version or date that is not one are errors, so the entry is never
// listed twice or empty.
func Prepend(existing, version, date string, items []string) (string, error) {
	if _, err := releasever.ParseVersion(version); err != nil {
		return "", err
	}
	if !isoDate.MatchString(date) {
		return "", fmt.Errorf("date %q is not YYYY-MM-DD", date)
	}
	if entryStart(existing, version) >= 0 {
		return "", fmt.Errorf("the changelog already has an entry for %s", version)
	}
	var list strings.Builder
	for _, item := range items {
		if line := oneLine(item); line != "" {
			list.WriteString("- " + line + "\n")
		}
	}
	if list.Len() == 0 {
		return "", fmt.Errorf("an entry for %s needs at least one item", version)
	}
	entry := fmt.Sprintf("## %s - %s\n\n%s\n", version, date, list.String())
	if strings.TrimSpace(existing) == "" {
		return changelogTitle + "\n\n" + entry, nil
	}
	title, rest, hasBody := strings.Cut(existing, "\n")
	if !hasBody {
		return title + "\n\n" + entry, nil
	}
	return title + "\n\n" + entry + strings.TrimPrefix(rest, "\n"), nil
}

// Notes is the body of the entry for version: the lines after its heading up to the next
// heading, trimmed. An entry that is missing, or has nothing in it, is an error: a release is
// never published with empty notes.
func Notes(changelog, version string) (string, error) {
	if _, err := releasever.ParseVersion(version); err != nil {
		return "", err
	}
	start := entryStart(changelog, version)
	if start < 0 {
		return "", fmt.Errorf("the changelog has no entry for %s", version)
	}
	body := ""
	if i := strings.IndexByte(changelog[start:], '\n'); i >= 0 {
		body = changelog[start+i+1:]
	}
	if next := strings.Index(body, "\n## "); next >= 0 {
		body = body[:next]
	} else if strings.HasPrefix(body, "## ") {
		body = ""
	}
	if body = strings.TrimSpace(body); body == "" {
		return "", fmt.Errorf("the entry for %s in the changelog is empty", version)
	}
	return body, nil
}

// entryStart is the offset of the heading line of version's entry, or -1: a line that starts
// "## <version> " (the space keeps 44.1.0 from matching 44.1.0-rc1).
func entryStart(changelog, version string) int {
	heading := "## " + version + " "
	if strings.HasPrefix(changelog, heading) {
		return 0
	}
	if i := strings.Index(changelog, "\n"+heading); i >= 0 {
		return i + 1
	}
	return -1
}

// oneLine is text on a single line: line breaks become spaces, so an item cannot start a
// heading or a list of its own.
func oneLine(text string) string {
	return strings.Join(strings.Fields(text), " ")
}
