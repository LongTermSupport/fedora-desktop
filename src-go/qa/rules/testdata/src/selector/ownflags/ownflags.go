// Package ownflags parses its own command line, which has a --continue of its own: it reads
// no recorded launch (it does not import the registry), so it is not judged.
package ownflags

func Parse(args []string) bool {
	for _, a := range args {
		if a == "--continue" || a == "-c" {
			return true
		}
	}
	return false
}
