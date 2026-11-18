package pushgate

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// pathFilter is a workflow's push trigger path filter, read the way GitHub reads it.
type pathFilter struct {
	// all is a push trigger with no path filter: every file starts the workflow.
	all bool
	// ignore is a paths-ignore filter; otherwise the patterns are a paths filter.
	ignore   bool
	patterns []globPattern
}

type globPattern struct {
	negated bool
	re      *regexp.Regexp
}

// runsFor is whether a push changing file alone would start the workflow. paths: the patterns
// in order, a ! pattern taking back what earlier ones matched, the last match decides.
// paths-ignore: the file is left out when any pattern matches.
func (f pathFilter) runsFor(file string) bool {
	if f.all {
		return true
	}
	if f.ignore {
		for _, p := range f.patterns {
			if p.re.MatchString(file) {
				return false
			}
		}
		return true
	}
	runs := false
	for _, p := range f.patterns {
		if p.re.MatchString(file) {
			runs = !p.negated
		}
	}
	return runs
}

// parsePushFilter reads the push trigger's path filter of a workflow file. A workflow with no
// push trigger, a filter in a form GitHub would refuse, or a pattern compileGlob cannot read
// is an error.
func parsePushFilter(workflow []byte) (pathFilter, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(workflow, &doc); err != nil {
		return pathFilter{}, fmt.Errorf("not YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return pathFilter{}, errors.New("not a workflow: the document is not a mapping")
	}
	on, err := field(doc.Content[0], "on")
	if err != nil {
		return pathFilter{}, err
	}
	switch on.Kind {
	case yaml.ScalarNode:
		if on.Value == "push" {
			return pathFilter{all: true}, nil
		}
	case yaml.SequenceNode:
		for _, ev := range on.Content {
			if ev.Kind == yaml.ScalarNode && ev.Value == "push" {
				return pathFilter{all: true}, nil
			}
		}
	case yaml.MappingNode:
		if push, err := field(on, "push"); err == nil {
			return pushTrigger(push)
		} else if !errors.Is(err, errNoField) {
			return pathFilter{}, err
		}
	case yaml.DocumentNode, yaml.AliasNode:
	}
	return pathFilter{}, errors.New("the workflow has no push trigger")
}

var errNoField = errors.New("no such key")

// field is the value of key in a mapping; a key given twice is an error.
func field(m *yaml.Node, key string) (*yaml.Node, error) {
	var found *yaml.Node
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value != key {
			continue
		}
		if found != nil {
			return nil, fmt.Errorf("the key %q is given twice", key)
		}
		found = m.Content[i+1]
	}
	if found == nil {
		return nil, fmt.Errorf("%w: %q", errNoField, key)
	}
	return found, nil
}

func pushTrigger(push *yaml.Node) (pathFilter, error) {
	if push.Kind == yaml.ScalarNode && push.Tag == "!!null" {
		return pathFilter{all: true}, nil
	}
	if push.Kind != yaml.MappingNode {
		return pathFilter{}, errors.New("the push trigger is not a mapping")
	}
	paths, pathsErr := field(push, "paths")
	ignore, ignoreErr := field(push, "paths-ignore")
	for _, err := range []error{pathsErr, ignoreErr} {
		if err != nil && !errors.Is(err, errNoField) {
			return pathFilter{}, err
		}
	}
	switch {
	case paths != nil && ignore != nil:
		return pathFilter{}, errors.New("the push trigger has both paths and paths-ignore, which GitHub refuses")
	case paths != nil:
		return patternList(paths, "paths", false)
	case ignore != nil:
		return patternList(ignore, "paths-ignore", true)
	default:
		return pathFilter{all: true}, nil
	}
}

func patternList(list *yaml.Node, name string, ignore bool) (pathFilter, error) {
	if list.Kind != yaml.SequenceNode || len(list.Content) == 0 {
		return pathFilter{}, fmt.Errorf("push %s is not a list of patterns", name)
	}
	f := pathFilter{ignore: ignore}
	positive := false
	for _, item := range list.Content {
		if item.Kind != yaml.ScalarNode || item.Tag != "!!str" {
			return pathFilter{}, fmt.Errorf("push %s holds an item that is not a string (line %d)", name, item.Line)
		}
		text, negated := strings.CutPrefix(item.Value, "!")
		if negated && ignore {
			return pathFilter{}, fmt.Errorf("push %s: the ! pattern %q is outside what this gate reads", name, item.Value)
		}
		re, err := compileGlob(text)
		if err != nil {
			return pathFilter{}, fmt.Errorf("push %s: %w", name, err)
		}
		positive = positive || !negated
		f.patterns = append(f.patterns, globPattern{negated: negated, re: re})
	}
	if !positive {
		return pathFilter{}, fmt.Errorf("push %s has only ! patterns, which GitHub refuses", name)
	}
	return f, nil
}

// compileGlob turns one GitHub filter pattern (without its leading !) into an anchored regexp:
// * any run of characters but /, ** any run at all (**/ also none, so docs/**/*.md matches
// docs/README.md), ? and + zero-or-one and one-or-more of the character or class before them,
// [...] one character of the listed alphanumerics and ranges a-z, A-Z, 0-9, and \ the next
// character as itself. Anything else this gate cannot be sure GitHub reads the same way (a
// leading / or ./, ***, a quantifier with nothing to repeat, a mid-pattern !, braces) is an error.
func compileGlob(pattern string) (*regexp.Regexp, error) {
	refuse := func(why string) (*regexp.Regexp, error) {
		return nil, fmt.Errorf("the pattern %q is outside what this gate reads: %s", pattern, why)
	}
	switch {
	case pattern == "":
		return refuse("empty")
	case strings.HasPrefix(pattern, "/"), strings.HasPrefix(pattern, "./"):
		return refuse("a leading / or ./")
	case strings.Contains(pattern, "***"):
		return refuse("***")
	}
	g := &globCompiler{runes: []rune(pattern)}
	g.b.WriteString("^")
	for g.i < len(g.runes) {
		if why := g.step(); why != "" {
			return refuse(why)
		}
	}
	g.b.WriteString("$")
	return regexp.Compile(g.b.String())
}

// globCompiler is compileGlob's position in the pattern and the regexp written so far.
type globCompiler struct {
	runes []rune
	i     int
	b     strings.Builder
	// repeatable is whether the last thing written is one character or class, which ? and +
	// may follow.
	repeatable bool
}

// peek is whether the rune n places ahead is r.
func (g *globCompiler) peek(n int, r rune) bool {
	return g.i+n < len(g.runes) && g.runes[g.i+n] == r
}

// step compiles the rune at the position and moves past what it consumed; a pattern it cannot
// read answers why.
func (g *globCompiler) step() string {
	c := g.runes[g.i]
	g.i++
	switch c {
	case '*':
		g.star()
	case '?', '+':
		if !g.repeatable {
			return string(c) + " with nothing before it to repeat"
		}
		g.repeatable = false
		g.b.WriteRune(c)
	case '[':
		return g.class()
	case '\\':
		if g.i == len(g.runes) {
			return "a trailing \\"
		}
		g.literal(g.runes[g.i])
		g.i++
	case ']', '!', '{', '}':
		return "a " + string(c) + " outside its place"
	default:
		g.literal(c)
	}
	return ""
}

func (g *globCompiler) literal(c rune) {
	g.b.WriteString(regexp.QuoteMeta(string(c)))
	g.repeatable = true
}

// star writes *, ** or **/ (the opening * already consumed).
func (g *globCompiler) star() {
	g.repeatable = false
	switch {
	case g.peek(0, '*') && g.peek(1, '/'):
		g.i += 2
		g.b.WriteString("(?:.*/)?")
	case g.peek(0, '*'):
		g.i++
		g.b.WriteString(".*")
	default:
		g.b.WriteString("[^/]*")
	}
}

// class writes a [...] class (the [ already consumed).
func (g *globCompiler) class() string {
	end := indexRune(g.runes[g.i:], ']')
	if end < 0 {
		return "an unclosed ["
	}
	class := g.runes[g.i : g.i+end]
	if !validClass(class) {
		return "a class other than alphanumerics and the ranges a-z, A-Z, 0-9"
	}
	g.b.WriteString("[" + string(class) + "]")
	g.i += end + 1
	g.repeatable = true
	return ""
}

func indexRune(rs []rune, r rune) int {
	for i, x := range rs {
		if x == r {
			return i
		}
	}
	return -1
}

// validClass is whether a bracket class holds only alphanumerics and ranges within one of a-z,
// A-Z or 0-9, low end first.
func validClass(class []rune) bool {
	if len(class) == 0 {
		return false
	}
	for i := 0; i < len(class); i++ {
		lo := class[i]
		if !alnum(lo) {
			return false
		}
		if i+2 < len(class) && class[i+1] == '-' {
			hi := class[i+2]
			if !alnum(hi) || kind(lo) != kind(hi) || hi < lo {
				return false
			}
			i += 2
		}
	}
	return true
}

func alnum(r rune) bool { return kind(r) != 0 }

// kind is 1 for a-z, 2 for A-Z, 3 for 0-9, 0 for anything else.
func kind(r rune) int {
	switch {
	case r >= 'a' && r <= 'z':
		return 1
	case r >= 'A' && r <= 'Z':
		return 2
	case r >= '0' && r <= '9':
		return 3
	default:
		return 0
	}
}
