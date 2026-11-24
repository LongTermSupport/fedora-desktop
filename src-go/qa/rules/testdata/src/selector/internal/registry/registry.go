// Package registry stands in for internal/registry: the one parser of claude's conversation
// selectors in a recorded launch (selectorOf), and ResumeID as it was (fedora-desktop#411),
// a second reader that took the word after a bare --resume even when it was a flag.
package registry

import "strings"

// Record is a recorded session.
type Record struct {
	Args []string
}

type selector int

const (
	selectorNone selector = iota
	selectorAlone
	selectorMaybeValue
	selectorWithValue
)

// selectorOf is the one parser, not judged.
func selectorOf(w string) selector {
	switch {
	case w == "-c", w == "--continue", strings.HasPrefix(w, "--resume="), strings.HasPrefix(w, "--session-id="):
		return selectorAlone
	case w == "-r", w == "--resume":
		return selectorMaybeValue
	case w == "--session-id":
		return selectorWithValue
	}
	return selectorNone
}

// ResumeID is the defect's shape: a second reader of --resume that takes the next word
// whatever it is.
func ResumeID(args []string) string {
	id := ""
	for i := 0; i < len(args); i++ {
		switch word := args[i]; {
		case strings.HasPrefix(word, "--resume="): // want `claude's conversation selectors are read in registry's selectorOf alone`
			id = strings.TrimPrefix(word, "--resume=") // want `claude's conversation selectors are read in registry's selectorOf alone`
		case word == "--resume", word == "-r": // want `claude's conversation selectors are read in registry's selectorOf alone` `claude's conversation selectors are read in registry's selectorOf alone`
			if i+1 < len(args) {
				i++
				id = args[i]
			}
		}
	}
	return id
}

// asked reads the selectors through the parser.
func asked(args []string) bool {
	for _, w := range args {
		if selectorOf(w) != selectorNone {
			return true
		}
	}
	return false
}

// ResumeArgs builds a resume: spelling a selector to write it is not reading one.
func ResumeArgs(args []string, id string) []string {
	if asked(args) {
		args = nil
	}
	return append(args, "--resume", id)
}
