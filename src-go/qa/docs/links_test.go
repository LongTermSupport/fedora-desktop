package docs

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	absent  = "target does not exist"
	noTrack = "target is not tracked by this repository"
)

// hit formats the stdout line for a finding of docs/t.md.
func hit(line int, target, problem string) string {
	return "docs/t.md:" + strconv.Itoa(line) + "  " + target + "  — " + problem
}

// D8: what is a link, and what is not.
func TestD8WhatIsAndIsNotALink(t *testing.T) {
	cases := []struct {
		name string
		body []string
		want []string
	}{
		{"inline", []string{"[x](./a.md)"}, []string{hit(1, "./a.md", absent)}},
		{"inline with a title", []string{`[x](./a.md "the title")`}, []string{hit(1, "./a.md", absent)}},
		{"angle bracket destination", []string{"[x](<./a b.md>)"}, []string{hit(1, "./a b.md", absent)}},
		{"reference link", []string{"see [x][r]", "", "[r]: ./a.md"}, []string{hit(1, "./a.md", absent)}},
		{"collapsed reference", []string{"see [r][]", "", "[r]: ./a.md"}, []string{hit(1, "./a.md", absent)}},
		{"shortcut reference", []string{"see [r]", "", "[r]: ./a.md"}, []string{hit(1, "./a.md", absent)}},
		{"an unused reference definition is no link", []string{"[r]: ./a.md"}, nil},
		{"an undefined reference is no link", []string{"see [x][nowhere]"}, nil},
		{"in a list", []string{"- one", "- [x](./a.md)"}, []string{hit(2, "./a.md", absent)}},
		{"in a quotation", []string{"> [x](./a.md)"}, []string{hit(1, "./a.md", absent)}},
		{"in a table cell", []string{"| h |", "| --- |", "| [x](./a.md) |"}, []string{hit(3, "./a.md", absent)}},
		{"in a heading", []string{"## [x](./a.md)"}, []string{hit(1, "./a.md", absent)}},
		{"two on a line, in order", []string{"[a](./a.md) and [b](./b.md)"}, []string{hit(1, "./a.md", absent), hit(1, "./b.md", absent)}},
		{"emphasis around the text", []string{"[**x**](./a.md)"}, []string{hit(1, "./a.md", absent)}},
		{"a code span as the text", []string{"[`x`](./a.md)"}, []string{hit(1, "./a.md", absent)}},
		{"nested brackets in the text", []string{"[a [b] c](./a.md)"}, []string{hit(1, "./a.md", absent)}},
		{"the line the text starts on", []string{"para", "[multi", "line](./a.md)"}, []string{hit(2, "./a.md", absent)}},
		{"an image is no link", []string{"![x](./a.png)"}, nil},
		{"a link around an image is a link", []string{"[![x](./a.png)](./b.md)"}, []string{hit(1, "./b.md", absent)}},
		{"a code span is no link", []string{"`[x](./a.md)`"}, nil},
		{"a regular expression in a code span", []string{"match `[a-z](foo)` then [x](./a.md)"}, []string{hit(1, "./a.md", absent)}},
		{"a fenced block is no link", []string{"```", "[x](./a.md)", "```"}, nil},
		{"a tilde fence is no link", []string{"~~~", "[x](./a.md)", "~~~"}, nil},
		{"an indented block is no link", []string{"para", "", "    [x](./a.md)"}, nil},
		{"an escaped bracket is no link", []string{`\[x](./a.md)`}, nil},
		{"an autolink is not checked", []string{"<https://example.com/a.md> <./a.md>"}, nil},
		{"raw HTML is not checked", []string{`<a href="./a.md">x</a>`}, nil},
		{"https is external", []string{"[x](https://example.com/a.md)"}, nil},
		{"mailto is external", []string{"[x](mailto:a@example.com)"}, nil},
		{"a network path is external", []string{"[x](//example.com/a.md)"}, nil},
		{"a scheme with digits and dots", []string{"[x](git+ssh.v2://example.com/a)"}, nil},
		{"CRLF line ends", []string{"[x](./a.md)\r", "[y](./b.md)\r"}, []string{hit(1, "./a.md", absent), hit(2, "./b.md", absent)}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t, nil)
			w.doc("t.md", c.body...)
			w.wantFindings(c.want...)
		})
	}
}

// D9
func TestD9AnAbsolutePathIsAFinding(t *testing.T) {
	w := newWorld(t, nil)
	w.doc("t.md", "[x](/docs/README.md)", "[y](/etc/hostname#a)")
	w.wantFindings(
		hit(1, "/docs/README.md", "link is an absolute path: use a path relative to this document"),
		hit(2, "/etc/hostname#a", "link is an absolute path: use a path relative to this document"),
	)
}

// D10
func TestD10TheDestinationIsPercentDecoded(t *testing.T) {
	w := newWorld(t, nil)
	w.add("docs/a b.md", "# Héllo Wörld\n")
	w.doc("t.md", "[x](a%20b.md)", "[y](<a b.md>)", "[z](a%20b.md#h%C3%A9llo-w%C3%B6rld)", "[w](a%20b.md#nope)", "[bad](a%zzb.md)")
	w.wantFindings(
		hit(4, "a%20b.md#nope", "no heading matches the anchor"),
		hit(5, "a%zzb.md", "link target is not valid percent-encoding"),
	)
}

// D11
func TestD11ResolutionIsRelativeToTheDocumentAndLexical(t *testing.T) {
	w := newWorld(t, nil)
	w.add("helpers/Topic.md", "# T\n")
	w.add("docs/sub/deep.md", "# D\n")
	w.doc("t.md", "[a](./sub/deep.md)", "[b](sub/../sub/deep.md)", "[c](../helpers/Topic.md)", "[d](sub/nope.md)", "[e](../README.md)")
	w.wantFindings(
		hit(4, "sub/nope.md", absent),
		hit(5, "../README.md", absent),
	)
}

func TestD11APathLeavingTheRepositoryIsJudgedLikeAnyOther(t *testing.T) {
	w := newWorld(t, nil)
	outside := filepath.Join(filepath.Dir(w.root), "outside.md")
	if err := os.WriteFile(outside, []byte("# o\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.doc("t.md", "[a](../../"+filepath.Base(w.root)+"/../outside.md)", "[b](../../../../../../../nowhere-at-all.md)")
	w.wantFindings(
		hit(1, "../../"+filepath.Base(w.root)+"/../outside.md", noTrack),
		hit(2, "../../../../../../../nowhere-at-all.md", absent),
	)
}

// D12
func TestD12ATargetOnDiskThatNobodyTrackedIsAFinding(t *testing.T) {
	w := newWorld(t, nil)
	w.fresh("docs/new.md", "# N\n")
	w.ignored("docs/ignored.md", "# I\n")
	w.add("docs/tracked.md", "# T\n")
	w.doc("t.md", "[a](new.md)", "[b](ignored.md)", "[c](tracked.md)", "[d](missing.md)")
	w.wantFindings(
		hit(1, "new.md", noTrack),
		hit(2, "ignored.md", noTrack),
		hit(4, "missing.md", absent),
	)
}

func TestD12ADirectoryThatHoldsATrackedFileAndTheRootAreOurs(t *testing.T) {
	w := newWorld(t, nil)
	if err := os.MkdirAll(filepath.Join(w.root, "emptydir"), 0o750); err != nil {
		t.Fatal(err)
	}
	w.doc("t.md", "[a](../playbooks/)", "[b](../playbooks/imports)", "[c](../)", "[d](.)", "[e](../emptydir)")
	w.wantFindings(hit(5, "../emptydir", noTrack))
}

func TestD12ANestedRepositoryIsNamedInTheHint(t *testing.T) {
	w := newWorld(t, nil)
	w.put("scratch/clone/.git/HEAD", "ref\n")
	w.put("scratch/clone/README.md", "# c\n")
	w.put("scratch/linked/.git", "gitdir: elsewhere\n")
	w.put("scratch/linked/x.md", "# x\n")
	w.doc("t.md", "[a](../scratch/clone/README.md)", "[b](../scratch/linked/x.md)", "[c](../scratch/clone)")
	hint := " — a repository is nested at scratch/"
	got := w.check()
	wantExit(t, got, ExitFailed)
	for _, want := range []string{
		noTrack + hint + "clone; if it is vendored, declare it in the vendored roots (src-go/qa/docs)",
		noTrack + hint + "linked; if it is vendored, declare it in the vendored roots (src-go/qa/docs)",
	} {
		wantStdout(t, got, want)
	}
	if strings.Count(got.stdout, "a repository is nested") != 3 {
		t.Fatalf("every target under a nested repository carries the hint:\n%s", got.stdout)
	}
	w2 := newWorld(t, nil)
	w2.put("scratch/plain/x.md", "# x\n")
	w2.doc("t.md", "[a](../scratch/plain/x.md)")
	got = w2.check()
	if strings.Contains(got.stdout, "nested") {
		t.Fatalf("hint without a nested repository:\n%s", got.stdout)
	}
}

// D13
func TestD13LinksIntoAVendoredRepositoryAreCountedNeverFindings(t *testing.T) {
	w := newWorld(t, nil)
	w.doc("t.md",
		"[present](../.claude/hooks-daemon/README.md)",
		"[root](../.claude/hooks-daemon)",
		"[rootslash](../.claude/hooks-daemon/)",
		"[roles](../roles/vendor/x/README.md)",
		"[anchor](../.claude/hooks-daemon/README.md#no-such-heading)")
	got := w.check()
	wantExit(t, got, ExitOK)
	// The daemon tree is present (one file written), roles/vendor is absent.
	wantStdout(t, got, "VENDORED: 4 verified, 1 unverifiable (repo absent), 0 broken")
}

func TestD13ABrokenLinkIntoAPresentVendoredRepositoryIsAWarningNotAFinding(t *testing.T) {
	w := newWorld(t, nil)
	w.doc("t.md", "", "[moved](../.claude/hooks-daemon/gone.md)")
	got := w.check()
	wantExit(t, got, ExitOK)
	want := "⚠ docs: 1 link(s) into a PRESENT vendored repo are broken — it has probably moved the file:\n" +
		"    docs/t.md:2  ../.claude/hooks-daemon/gone.md\n" +
		"✓ docs: 6 files (6 tracked) OK"
	if !strings.HasPrefix(got.stdout, want) {
		t.Fatalf("stdout:\n%s", got.stdout)
	}
	wantStdout(t, got, "VENDORED: 0 verified, 0 unverifiable (repo absent), 1 broken")
	rep := w.report()
	if len(rep.Vendored.Broken) != 1 || rep.Vendored.Broken[0].File != "docs/t.md" || rep.Vendored.Broken[0].Line != 2 {
		t.Fatalf("%+v", rep.Vendored)
	}
	if rep.Status != "pass" {
		t.Fatalf("a vendored warning failed the gate: %+v", rep)
	}
}

func TestD13TheWarningBlockPrecedesTheFailLineAndSiblingsAreNotVendored(t *testing.T) {
	w := newWorld(t, nil)
	w.add("roles/vendor-extra/x.md", "# x\n")
	w.doc("t.md", "[moved](../.claude/hooks-daemon/gone.md)", "[sibling](../roles/vendor-extra/y.md)")
	got := w.check()
	wantExit(t, got, ExitFailed)
	if !strings.HasPrefix(got.stdout, "⚠ docs: 1 link(s)") || !strings.Contains(got.stdout, "\n✗ docs: 1 finding(s)") {
		t.Fatalf("stdout:\n%s", got.stdout)
	}
	wantStdout(t, got, "docs/t.md:2  ../roles/vendor-extra/y.md  — "+absent)
}

func TestD13AnAnchorIntoAVendoredTargetIsNotFollowedAndItsFileNeedNotBeRead(t *testing.T) {
	w := newWorld(t, nil)
	w.put(".claude/hooks-daemon/x.md", "# x\n")
	w.doc("t.md", "[a](../.claude/hooks-daemon/x.md#nope)")
	w.readFail["x.md"] = os.ErrPermission
	got := w.check()
	wantExit(t, got, ExitOK)
}

// D15
func TestD15HeadingSlugs(t *testing.T) {
	cases := []struct {
		heading, anchor string
		ok              bool
	}{
		{"## Plain Words", "plain-words", true},
		{"## Plain Words", "Plain-Words", true},
		{"## `code` and **bold** and _em_", "code-and-bold-and-em", true},
		{"## A [link text](https://example.com/x) here", "a-link-text-here", true},
		{"## A — B", "a--b", true},
		{"## A — B", "a-b", false},
		{"## Under_score and-hyphen", "under_score-and-hyphen", true},
		{"## Punct: (yes), no? 100%", "punct-yes-no-100", true},
		{"## Slash/semi;colon", "slashsemicolon", true},
		{"## Ünïcode Straße", "ünïcode-straße", true},
		{"## Cafe\u0301 au lait", "cafe\u0301-au-lait", true},
		{"## Ends with hashes ##", "ends-with-hashes", true},
		{"## Trailing  double", "trailing--double", true},
		{`## Escaped \*star\*`, "escaped-star", true},
		{"## Fish &amp; Chips", "fish--chips", true},
		{"## ![alt](./x.png) After image", "after-image", true},
		{"Setext Title\n============", "setext-title", true},
		{"Setext *Two*\n------------", "setext-two", true},
		{"- ## In a list", "in-a-list", true},
		{"> ## In a quote", "in-a-quote", true},
		{"```\n## In a fence\n```", "in-a-fence", false},
		{"    ## Indented code", "indented-code", false},
		{"## Real", "nope", false},
		{"#NotAHeading", "notaheading", false},
	}
	for _, c := range cases {
		t.Run(c.heading+"#"+c.anchor, func(t *testing.T) {
			w := newWorld(t, nil)
			w.add("docs/target.md", c.heading+"\n")
			w.doc("t.md", "[x](target.md#"+c.anchor+")")
			if c.ok {
				w.wantFindings()
			} else {
				w.wantFindings(hit(1, "target.md#"+c.anchor, "no heading matches the anchor"))
			}
		})
	}
}

func TestD15DuplicateHeadingsAreNumberedAndAnEmptySlugDefinesNothing(t *testing.T) {
	w := newWorld(t, nil)
	w.add("docs/target.md", "## Same\n\n## Same\n\n## Same\n\n## ???\n\n## Same-1\n")
	w.doc("t.md",
		"[a](target.md#same)", "[b](target.md#same-1)", "[c](target.md#same-2)",
		"[d](target.md#same-1-1)", "[e](target.md#same-3)", "[f](target.md#-1)", "[g](target.md#)")
	w.wantFindings(
		hit(5, "target.md#same-3", "no heading matches the anchor"),
		hit(6, "target.md#-1", "no heading matches the anchor"),
	)
}

func TestD15ASelfAnchorIsCheckedAgainstTheDocumentItself(t *testing.T) {
	w := newWorld(t, nil)
	w.doc("t.md", "# Title", "", "[a](#title)", "[b](#nope)")
	w.wantFindings(hit(4, "#nope", "no heading matches the anchor"))
}

func TestD15AnAnchorOnANonMarkdownTargetOrADirectoryIsNotChecked(t *testing.T) {
	w := newWorld(t, nil)
	w.add("docs/data.txt", "x\n")
	w.doc("t.md", "[a](data.txt#L10)", "[b](../playbooks/#anything)", "[c](../scripts/g1.bash#L3)")
	w.wantFindings()
}

func TestD15TheAnchorTargetMayBeOutsideThePopulation(t *testing.T) {
	w := newWorld(t, nil)
	w.add("helpers/loose.md", "# Loose\n")
	w.doc("t.md", "[a](../helpers/loose.md#loose)", "[b](../helpers/loose.md#tight)")
	w.wantFindings(hit(2, "../helpers/loose.md#tight", "no heading matches the anchor"))
}

// D16
func TestD16TheLineOfAnEmptyLinkTextIsTheParagraphsFirstLine(t *testing.T) {
	w := newWorld(t, nil)
	w.doc("t.md", "first", "second [](./a.md)")
	w.wantFindings(hit(1, "./a.md", absent))
}
