// Package shellword quotes words for a POSIX shell: the one quoter the tools share, for a
// command shown to a person (a dry run) or a value written where a shell reads it.
package shellword

import "strings"

// plain are the characters a word may hold and still need no quotes in any POSIX shell.
// Glob characters (*, ?, [) are not among them.
const plain = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_@%+=:,./-"

// Quote is s as one shell word: unchanged when every character is plain, otherwise in single
// quotes, each single quote inside closed, escaped and reopened. The empty word is a pair of
// single quotes.
func Quote(s string) string {
	if s != "" && strings.Trim(s, plain) == "" {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Join is argv as one shell command line: each word quoted, one space between them.
func Join(argv []string) string {
	words := make([]string, len(argv))
	for i, a := range argv {
		words[i] = Quote(a)
	}
	return strings.Join(words, " ")
}
