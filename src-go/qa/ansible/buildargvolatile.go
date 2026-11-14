package ansible

import (
	"regexp"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
)

// The image-build-arg-volatile rule (CLAUDE/QA.md, "image-build-arg-volatile"). A container
// image build given a --build-arg or --label whose value is new on every commit or every run,
// whatever the image is built from, rebuilds every stage that reads it and gives the image a new
// id every time: the build pays for work it did not need, sessions on the image look out of date
// when nothing they use changed, and a play that reports the rebuild as its change reports one
// on every run (issue 277: the ccy image's builder stage keyed on the checkout's HEAD).
//
// Volatile values are the output of a command that reads the checkout's current commit (git
// rev-parse of HEAD or @, git log or git show with no path limit, git describe), the clock
// (date) or a fresh id (uuidgen), and the facts ansible_date_time and now(). Volatile names are
// the register of a command or shell task that runs such a command, then, to a fixed point over
// every file judged together, each vars entry and set_fact key whose value names one or holds a
// volatile value. The rule reports a command or shell task that runs an image build (a build
// word in its command) with a --build-arg or --label whose value names a volatile name or holds
// a volatile value, and a copy task that writes such a value (its content) into the build context
// of a build in the files (the build's last word, an absolute path), where a COPY or a launcher
// that passes it as a build argument carries it into the image. The commit of the last change to given paths (git log -- <paths>), a tree
// id (HEAD:<path>) and a content hash are not volatile: they change when what they name does.

// ruleBuildArgVolatile is the rule ID, printed with every finding and the anchor of its section.
const ruleBuildArgVolatile = "image-build-arg-volatile"

var (
	// headRevision is an argument naming the current commit: HEAD or @, peeled or not.
	headRevision = regexp.MustCompile(`^["']?(?:HEAD|@)(?:\^\{commit\}|\^0|\^\{\})?["']?$`)
	// gitCommand is a git command line up to a shell separator: its words after git.
	gitCommand = regexp.MustCompile(`\bgit\b([^;&|\n)]*)`)
	// clockCommand is a command that reads the clock or makes a fresh id.
	clockCommand = regexp.MustCompile(`(?:^|[\s;&|(\x60'"])(?:date|uuidgen)(?:$|[\s;&|)\x60'"])`)
	// volatileFact is a Jinja fact or function that is new on every run.
	volatileFact = regexp.MustCompile(`\bansible_date_time\b|\bnow\s*\(`)
	// buildWord is the build subcommand of an image build in a command line.
	buildWord = regexp.MustCompile(`(?:^|\s)build(?:\s|$)`)
	// keyedOption is a --build-arg or --label in a command line, and its value: up to the next
	// option or the end of the line.
	keyedOption = regexp.MustCompile(`--(?:build-arg|label)(?:=|\s+)(.*?)(?:\s--|$)`)
)

// buildKeyOptions are the build options whose value is part of the image's cache key or id.
var buildKeyOptions = []string{"--build-arg", "--label"}

// volatileText is whether a command line or value reads the current commit, the clock or a
// fresh id, or is a fact that is new on every run.
func volatileText(s string) bool {
	if volatileFact.MatchString(s) || clockCommand.MatchString(s) {
		return true
	}
	for _, m := range gitCommand.FindAllStringSubmatch(s, -1) {
		if volatileGit(strings.Fields(m[1])) {
			return true
		}
	}
	return false
}

// gitSubcommands are the git subcommands that can name the current commit.
var gitSubcommands = []string{"rev-parse", "log", "show", "describe"}

// volatileGit is whether git's words name the current commit with nothing narrowing it to paths.
// The subcommand is the first of gitSubcommands among the words: a -C or -c value before it may
// be a template of several words, so it cannot be skipped by position.
func volatileGit(words []string) bool {
	at := slices.IndexFunc(words, func(w string) bool { return slices.Contains(gitSubcommands, w) })
	if at < 0 {
		return false
	}
	sub := words[at]
	words = words[at+1:]
	switch sub {
	case "rev-parse":
		return slices.ContainsFunc(words, headRevision.MatchString)
	case "log", "show":
		return !slices.Contains(words, "--")
	case "describe":
		return true
	}
	return false
}

// imageBuildArgVolatile returns the rule's findings over every file, judged together, each
// "<rel>:<line>: <rule>: <why>", in file then document order. A file the YAML library cannot
// parse is skipped: command-always-changed reports it.
func imageBuildArgVolatile(sources []yamlSource) []string {
	files := make([]scratchFile, 0, len(sources))
	volatile := map[string]bool{}
	for _, src := range sources {
		f := scratchFile{rel: src.rel, docs: parsedDocs(src.data)}
		for _, t := range f.tasks() {
			reg := scalarValue(t.node, "register")
			if reg != "" && (t.module == modCommand || t.module == modShell) && volatileText(commandText(t.moduleValue())) {
				volatile[reg] = true
			}
		}
		files = append(files, f)
	}
	for spreadVolatileAll(files, volatile) {
	}
	contexts := buildContexts(files)
	var findings []string
	for _, f := range files {
		for _, t := range f.tasks() {
			switch {
			case volatileBuild(t, volatile):
				findings = append(findings, f.rel+":"+t.line()+": "+ruleBuildArgVolatile+": this image build is given a "+
					"build argument or label that is new on every commit or run, so it is rebuilt every time whatever "+
					"changed; key it on what the image is built from (the last commit of its source paths, a tree id, a hash)")
			case volatileContextWrite(t, volatile, contexts):
				findings = append(findings, f.rel+":"+t.line()+": "+ruleBuildArgVolatile+": this task writes a value that "+
					"is new on every commit or run into an image build context, where a COPY or a later build argument "+
					"makes the image new every time; write what the image is built from (the last commit of its source "+
					"paths, a tree id, a hash)")
			}
		}
	}
	return findings
}

// buildContexts is the build context of every image build in the files: the last argv word of a
// build, or the last word of its command line, when it is an absolute path.
func buildContexts(files []scratchFile) []string {
	var contexts []string
	for _, f := range files {
		for _, t := range f.tasks() {
			if t.module != modCommand && t.module != modShell {
				continue
			}
			words := buildWords(t)
			if len(words) == 0 || !slices.Contains(words, "build") {
				continue
			}
			if last := strings.Trim(words[len(words)-1], `"'`); strings.HasPrefix(last, "/") {
				contexts = append(contexts, strings.TrimRight(last, "/")+"/")
			}
		}
	}
	return contexts
}

// buildWords is a command task's argv words, or its command line's fields.
func buildWords(t uxTask) []string {
	for _, holder := range []*yaml.Node{t.moduleValue(), mappingValue(t.node, "args")} {
		if holder == nil || holder.Kind != yaml.MappingNode {
			continue
		}
		if argv := mappingValue(holder, "argv"); argv != nil && argv.Kind == yaml.SequenceNode {
			words := make([]string, 0, len(argv.Content))
			for _, w := range argv.Content {
				words = append(words, w.Value)
			}
			return words
		}
	}
	return strings.Fields(commandText(t.moduleValue()))
}

// volatileContextWrite is whether a copy task writes a volatile content into a build context.
func volatileContextWrite(t uxTask, volatile map[string]bool, contexts []string) bool {
	args := t.moduleValue()
	if t.module != modCopy || args == nil || args.Kind != yaml.MappingNode {
		return false
	}
	dest := scalarValue(args, "dest")
	inContext := slices.ContainsFunc(contexts, func(c string) bool { return strings.HasPrefix(dest, c) })
	return inContext && volatileValue(mappingValue(args, "content"), volatile)
}

// spreadVolatileAll is one pass over every file's vars and set_fact: whether a name was added.
func spreadVolatileAll(files []scratchFile, volatile map[string]bool) bool {
	grew := false
	for _, f := range files {
		for _, doc := range f.docs {
			grew = spreadVolatile(doc, volatile) || grew
		}
	}
	return grew
}

// spreadVolatile adds every vars entry and set_fact key under n whose value is volatile.
func spreadVolatile(n *yaml.Node, volatile map[string]bool) bool {
	grew := false
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, m := n.Content[i].Value, n.Content[i+1]
			if (key != keyVars && shortName(key) != modSetFact) || m.Kind != yaml.MappingNode {
				continue
			}
			for j := 0; j+1 < len(m.Content); j += 2 {
				name := m.Content[j].Value
				if !volatile[name] && volatileValue(m.Content[j+1], volatile) {
					volatile[name], grew = true, true
				}
			}
		}
	}
	for _, child := range n.Content {
		grew = spreadVolatile(child, volatile) || grew
	}
	return grew
}

// volatileValue is whether a scalar names a volatile name or holds a volatile value.
func volatileValue(v *yaml.Node, volatile map[string]bool) bool {
	if v == nil || v.Kind != yaml.ScalarNode {
		return false
	}
	return namesScratch(v, volatile) || volatileText(v.Value)
}

// volatileBuild is whether a task runs an image build keyed on a volatile value.
func volatileBuild(t uxTask, volatile map[string]bool) bool {
	if t.module != modCommand && t.module != modShell {
		return false
	}
	args := t.moduleValue()
	for _, value := range buildKeyValues(args, mappingValue(t.node, "args")) {
		if volatileValue(&yaml.Node{Kind: yaml.ScalarNode, Value: value}, volatile) {
			return true
		}
	}
	return false
}

// buildKeyValues is the values of a build's --build-arg and --label options: from its argv
// words when it has an argv, else from its command line. None when it runs no build.
func buildKeyValues(args, taskArgs *yaml.Node) []string {
	for _, holder := range []*yaml.Node{args, taskArgs} {
		if holder == nil || holder.Kind != yaml.MappingNode {
			continue
		}
		if argv := mappingValue(holder, "argv"); argv != nil && argv.Kind == yaml.SequenceNode {
			return argvKeyValues(argv)
		}
	}
	text := commandText(args)
	if text == "" && taskArgs != nil {
		text = commandText(taskArgs)
	}
	var values []string
	for line := range strings.SplitSeq(text, "\n") {
		if !buildWord.MatchString(line) {
			continue
		}
		for _, m := range keyedOption.FindAllStringSubmatch(line, -1) {
			values = append(values, m[1])
		}
	}
	return values
}

// argvKeyValues is the --build-arg and --label values of an argv that has a build word.
func argvKeyValues(argv *yaml.Node) []string {
	words := make([]string, 0, len(argv.Content))
	for _, w := range argv.Content {
		words = append(words, w.Value)
	}
	if !slices.Contains(words, "build") {
		return nil
	}
	var values []string
	for i, w := range words {
		for _, opt := range buildKeyOptions {
			if w == opt && i+1 < len(words) {
				values = append(values, words[i+1])
			} else if value, ok := strings.CutPrefix(w, opt+"="); ok {
				values = append(values, value)
			}
		}
	}
	return values
}
