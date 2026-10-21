package teamchatseparate

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Name is one entry of the v1 name list. With Next empty it is found in any letter case; with
// Next set it is found exactly as written wherever Next does not follow it.
type Name struct {
	Text string
	Next string
}

// Hit is where a name was found in a line, as a byte range.
type Hit struct {
	Start int
	End   int
}

// errNoNames is a list with no entry: a scan against nothing proves nothing.
var errNoNames = errors.New("no v1 name is listed")

// ParseNames reads the v1 name list (the format is in the file's header). Every line is checked
// and the first problem is returned with its line number.
func ParseNames(data string) ([]Name, error) {
	var names []Name
	for i, raw := range strings.Split(data, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		n, err := parseName(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if slices.Contains(names, n) {
			return nil, fmt.Errorf("line %d: %s is listed twice", i+1, n.Text)
		}
		names = append(names, n)
	}
	if len(names) == 0 {
		return nil, errNoNames
	}
	return names, nil
}

func parseName(line string) (Name, error) {
	for _, r := range line {
		if r > 0x7e {
			return Name{}, errors.New("not ASCII: the case-free match is defined on ASCII")
		}
	}
	fields := strings.Fields(line)
	switch {
	case len(fields) == 1 && !strings.HasPrefix(fields[0], "!"):
		return Name{Text: fields[0]}, nil
	case len(fields) == 2 && !strings.HasPrefix(fields[0], "!") && len(fields[1]) > 1 && strings.HasPrefix(fields[1], "!"):
		return Name{Text: fields[0], Next: fields[1][1:]}, nil
	}
	return Name{}, fmt.Errorf("%q is neither NAME nor NAME !NEXT", line)
}

// Find is every place the name is found in the line.
func (n Name) Find(line string) []Hit {
	var hits []Hit
	for i := 0; i+len(n.Text) <= len(line); i++ {
		end := i + len(n.Text)
		if n.Next == "" && strings.EqualFold(line[i:end], n.Text) ||
			n.Next != "" && line[i:end] == n.Text && !strings.HasPrefix(line[end:], n.Next) {
			hits = append(hits, Hit{Start: i, End: end})
		}
	}
	return hits
}
