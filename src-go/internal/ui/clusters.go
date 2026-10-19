package ui

import "github.com/charmbracelet/x/ansi"

// Cluster is one grapheme cluster, what a terminal draws as one character (a ZWJ emoji or a
// flag is one cluster of several runes), and the cells it takes, counted as Width counts.
type Cluster struct {
	Text  string
	Cells int
}

// Clusters is text split into grapheme clusters. Text laid out for a terminal is cut and
// edited by cluster, never inside one.
func Clusters(text string) []Cluster {
	var out []Cluster
	for text != "" {
		cluster, cells := ansi.FirstGraphemeCluster(text, ansi.GraphemeWidth)
		out = append(out, Cluster{Text: cluster, Cells: cells})
		text = text[len(cluster):]
	}
	return out
}
