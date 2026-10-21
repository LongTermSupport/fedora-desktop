package proto

import "sort"

// unicode15New holds every code point Unicode 15.0 assigned that 14.0 left unassigned (SPEC
// P84): Go's tables are 15.0, the container's Python 3.11 is 14.0, and a code point classed
// differently would render or match differently on the two sides. Go's 15.0 categories agree
// with Python 14.0's on every other code point. Built by comparing the two tables; held equal
// to the corpus's unicode suite.
var unicode15New = [][2]rune{
	{0xCF3, 0xCF3},
	{0xECE, 0xECE},
	{0x10EFD, 0x10EFF},
	{0x1123F, 0x11241},
	{0x11B00, 0x11B09},
	{0x11F00, 0x11F10},
	{0x11F12, 0x11F3A},
	{0x11F3E, 0x11F59},
	{0x1342F, 0x1342F},
	{0x13439, 0x13455},
	{0x1B132, 0x1B132},
	{0x1B155, 0x1B155},
	{0x1D2C0, 0x1D2D3},
	{0x1DF25, 0x1DF2A},
	{0x1E030, 0x1E06D},
	{0x1E08F, 0x1E08F},
	{0x1E4D0, 0x1E4F9},
	{0x1F6DC, 0x1F6DC},
	{0x1F774, 0x1F776},
	{0x1F77B, 0x1F77F},
	{0x1F7D9, 0x1F7D9},
	{0x1FA75, 0x1FA77},
	{0x1FA87, 0x1FA88},
	{0x1FAAD, 0x1FAAF},
	{0x1FABB, 0x1FABD},
	{0x1FABF, 0x1FABF},
	{0x1FACE, 0x1FACF},
	{0x1FADA, 0x1FADB},
	{0x1FAE8, 0x1FAE8},
	{0x1FAF7, 0x1FAF8},
	{0x2B739, 0x2B739},
	{0x31350, 0x323AF},
}

// NewInUnicode15 reports whether Unicode 15.0 assigned the code point and 14.0 did not.
func NewInUnicode15(r rune) bool {
	i := sort.Search(len(unicode15New), func(i int) bool { return unicode15New[i][1] >= r })
	return i < len(unicode15New) && unicode15New[i][0] <= r
}
