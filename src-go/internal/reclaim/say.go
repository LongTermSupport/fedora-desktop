package reclaim

import (
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/ui"
)

// The terminal styles of E29, used only when colour is on.
const (
	styleBold   = "\x1b[1m"
	styleDim    = "\x1b[2m"
	styleCyan   = "\x1b[0;36m"
	styleYellow = "\x1b[1;33m"
	styleRed    = "\x1b[0;31m"
	styleGreen  = "\x1b[0;32m"
	styleBlue   = "\x1b[0;34m"
	styleReset  = "\x1b[0m"
)

// headingWidth is the width a heading is wrapped to when the terminal does not say.
const headingWidth = 80

// paint is text in style when colour is on (E29).
func (r *run) paint(style, text string) string {
	if !r.colour || text == "" {
		return text
	}
	return style + text + styleReset
}

// line is one line on stderr, in style.
func (r *run) line(style, text string) {
	r.errOut.Print(r.env.Stderr, r.paint(style, text)+"\n")
}

func (r *run) say(text string)     { r.line("", text) }
func (r *run) info(text string)    { r.line(styleCyan, text) }
func (r *run) warn(text string)    { r.line(styleYellow, text) }
func (r *run) errLine(text string) { r.line(styleRed, text) }
func (r *run) okLine(text string)  { r.line(styleGreen, text) }
func (r *run) dimLine(text string) { r.line(styleDim, text) }

// show is a program's output, each line indented under the action.
func (r *run) show(text string) {
	text = strings.TrimRight(text, "\n")
	if text == "" {
		return
	}
	for l := range strings.SplitSeq(text, "\n") {
		r.say("    " + l)
	}
}

// heading draws a screen's heading (E10, E14; InteractiveScripts.md rule 15).
func (r *run) heading(h ui.Heading) {
	width := headingWidth
	if r.env.Columns != nil {
		if n := r.env.Columns(); n > 0 {
			width = n
		}
	}
	drawn, err := h.Draw(r.colour, width)
	if err != nil {
		r.failf("%v", err)
		return
	}
	r.say("")
	r.say(drawn)
}
