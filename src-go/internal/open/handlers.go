package open

import (
	"slices"
	"strings"
)

// targetArg stands for the target in a handler's arguments; it is replaced by the target as one
// argument of its own, never spliced into a string (D4).
const targetArg = "\x00target"

// handler is one terminal handler: its program, label and arguments, and the pager its stdout
// feeds when it has one.
type handler struct {
	label string
	argv  []string
	pager []string
}

func h(label string, argv ...string) handler { return handler{label: label, argv: argv} }

// paged is h with its stdout fed to the pager.
func (x handler) paged(pager ...string) handler {
	x.pager = pager
	return x
}

// T is the target placeholder, short so the table reads like the commands it holds.
const T = targetArg

var (
	lessPager    = []string{"less"}
	rawLessPager = []string{"less", "-R"}
)

// typeRule is one row of E11: the types it matches, its handlers, and whether the text viewers
// follow them.
type typeRule struct {
	matches  func(mime string) bool
	handlers []handler
	text     bool
}

func exactly(types ...string) func(string) bool {
	return func(m string) bool { return slices.Contains(types, m) }
}

func under(prefixes ...string) func(string) bool {
	return func(m string) bool {
		return slices.ContainsFunc(prefixes, func(p string) bool { return strings.HasPrefix(m, p) })
	}
}

// terminalTable is E11, in order.
var terminalTable = []typeRule{
	{matches: exactly("inode/directory"), handlers: []handler{
		h("yazi (file manager)", "yazi", T),
		h("ranger (file manager)", "ranger", T),
		h("nnn (file manager)", "nnn", T),
		h("lf (file manager)", "lf", T),
		h("ls (listing)", "ls", "-lAh", "--color=always", "--", T).paged(rawLessPager...),
		h("tree (recursive listing)", "tree", "-C", "--", T).paged(rawLessPager...),
	}},
	{matches: under("x-scheme-handler/"), handlers: []handler{
		h("w3m (browser)", "w3m", "--", T),
		h("lynx (browser)", "lynx", "--", T),
		h("links (browser)", "links", "--", T),
		h("curl (dump)", "curl", "-fsSL", "--", T).paged(lessPager...),
	}},
	{matches: exactly("text/html", "application/xhtml+xml"), text: true, handlers: []handler{
		h("w3m (render)", "w3m", "--", T),
		h("lynx (render)", "lynx", "--", T),
	}},
	{matches: exactly("application/pdf"), handlers: []handler{
		h("pdftotext (text)", "pdftotext", "-layout", "--", T, "-").paged(lessPager...),
		h("pdfinfo (metadata)", "pdfinfo", "--", T),
	}},
	{matches: under("image/"), handlers: []handler{
		h("chafa (terminal image)", "chafa", "--", T),
		h("viu (terminal image)", "viu", "--", T),
		h("identify (metadata)", "identify", "--", T),
		h("exiftool (metadata)", "exiftool", "--", T).paged(lessPager...),
	}},
	{matches: under("audio/", "video/"), handlers: []handler{
		h("mpv (play)", "mpv", "--", T),
		h("ffplay (play)", "ffplay", "-autoexit", "--", T),
		h("ffprobe (metadata)", "ffprobe", "-hide_banner", "--", T),
	}},
	{matches: exactly("application/json"), text: true, handlers: []handler{
		h("jq (pretty print)", "jq", ".", T).paged(rawLessPager...),
	}},
	{matches: exactly(
		"application/zip", "application/x-tar", "application/gzip", "application/x-gzip",
		"application/x-bzip2", "application/x-xz", "application/zstd", "application/x-7z-compressed",
		"application/vnd.rar", "application/x-rar", "application/x-compressed-tar", "application/x-archive",
	), handlers: []handler{
		h("ouch (list contents)", "ouch", "list", "--", T).paged(lessPager...),
		h("atool (list contents)", "atool", "--list", "--", T).paged(lessPager...),
		h("bsdtar (list contents)", "bsdtar", "-tvf", T).paged(lessPager...),
		h("7z (list contents)", "7z", "l", "--", T).paged(lessPager...),
	}},
	{matches: exactly("application/vnd.sqlite3", "application/x-sqlite3"), handlers: []handler{
		h("sqlite3 (shell)", "sqlite3", T),
	}},
	{matches: exactly("application/x-executable", "application/x-sharedlib", "application/x-pie-executable"), handlers: []handler{
		h("readelf (headers)", "readelf", "-h", "--", T).paged(lessPager...),
		h("nm (symbols)", "nm", "--", T).paged(lessPager...),
	}},
	{matches: func(m string) bool {
		return strings.HasPrefix(m, "text/") || slices.Contains([]string{
			"application/xml", "application/javascript", "application/x-shellscript", "application/x-perl",
			"application/x-python", "application/toml", "application/yaml", "application/x-yaml",
			"application/x-desktop", "message/rfc822",
		}, m)
	}, text: true},
}

// genericHandlers are E13's offers for a type nobody is sure of, before the text viewers.
var genericHandlers = []handler{
	h("xxd (hex dump)", "xxd", "--", T).paged(lessPager...),
	h("hexdump (hex dump)", "hexdump", "-C", "--", T).paged(lessPager...),
}

// identify is offered for every type, last (E13).
var identify = h("file (identify only)", "file", "--", T)

// editors are E12's fall-backs after $VISUAL and $EDITOR.
var editors = []string{"nvim", "vim", "nano", "vi"}

// terminalCandidates is E11-E13: the installed handlers for mime, with target as their
// argument, and whether the type is known with something installed for it.
func (r *run) terminalCandidates(mime, target string) ([]candidate, bool) {
	var offered []handler
	for _, rule := range terminalTable {
		if !rule.matches(mime) {
			continue
		}
		offered = r.installed(rule.handlers)
		if rule.text {
			offered = append(offered, r.textViewers()...)
		}
		break
	}
	known := len(offered) > 0
	if !known {
		offered = append(r.installed(genericHandlers), r.textViewers()...)
	}
	offered = append(offered, r.installed([]handler{identify})...)
	cands := make([]candidate, len(offered))
	for i, x := range offered {
		cands[i] = candidate{label: x.label, argv: withTarget(x.argv, target), pager: x.pager}
	}
	return cands, known
}

// textViewers is E12, the installed ones.
func (r *run) textViewers() []handler {
	var out []handler
	if words := r.editor(); words != nil {
		out = append(out, h(strings.Join(words, " ")+" (edit)", append(words, T)...))
	}
	return append(out, r.installed([]handler{
		h("bat (view, highlighted)", "bat", "--paging=always", "--", T),
		h("less (page)", "less", "--", T),
	})...)
}

// editor is the first of $VISUAL, $EDITOR and the fall-backs whose first word is on PATH, as its
// words; nil when there is none.
func (r *run) editor() []string {
	for _, e := range append([]string{r.env.Getenv("VISUAL"), r.env.Getenv("EDITOR")}, editors...) {
		words := strings.Fields(e)
		if len(words) > 0 && r.onPath(words[0]) {
			return words
		}
	}
	return nil
}

// installed is the handlers whose program, and pager when they have one, are on PATH (B9).
func (r *run) installed(hs []handler) []handler {
	var out []handler
	for _, x := range hs {
		if r.onPath(x.argv[0]) && (x.pager == nil || r.onPath(x.pager[0])) {
			out = append(out, x)
		}
	}
	return out
}

func (r *run) onPath(prog string) bool {
	_, err := r.env.LookPath(prog)
	return err == nil
}

func withTarget(argv []string, target string) []string {
	out := slices.Clone(argv)
	for i, a := range out {
		if a == targetArg {
			out[i] = target
		}
	}
	return out
}
