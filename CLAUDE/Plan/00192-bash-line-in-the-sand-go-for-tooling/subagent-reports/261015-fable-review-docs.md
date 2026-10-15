# Adversarial review: `go-port/qa-docs` against `integration/261014`

Branch head `97fe96d2`, diff `origin/integration/261014...origin/go-port/qa-docs`, read against
`SPEC-qa-docs.md` (D1-D25, B1-B15) and `SPEC-qa-discovery.md` D18/D19. Method:
`src-go/CLAUDE.md` "How a port is built". PLAN.md not recording the port is known and skipped.

What was run (worktree `untracked/scratch/rev-docs`, removed afterwards): `go test` over
`qa/docs`, `qa/cmd/qa-docs`, `qa/discovery`, `qa/changed`, `qa/gates` (green);
`golangci-lint run` over the three new or changed packages (0 issues);
`scripts/test-qa-docs-exit-codes.bash` (6/6); the wrapper over the live tree
(`82 files (82 tracked)`, 8 unverifiable vendored links, exit 0, matching the journal); and the
built binary over an adversarial fixture repository (41 links of every CommonMark shape, 20
heading shapes, a gitignored target, an untracked target, a symlinked target, a nested-repo
hint, a stale QA.md row, a bad import mention). `qa-all.bash` was not run.

## Verdict

Merge after the FIX items. No BLOCK. The port does what the spec says, the tests are
expectation tests with teeth (every D number has at least one, the fake git refuses a run with
no deadline, the on-disk world lets a test plant exactly one defect), and nothing the Python
caught is lost: the fixture shows reference links, titled links, links in tables, quotations and
list items, Setext and nested headings, entities, escapes, em-dash double hyphens, emoji and
unicode headings, percent-encoding, absolute paths, paths leaving the root, untracked and
ignored targets, the nested-repository hint and both inventory directions all judged correctly.
None of the earlier reports' defect classes recurs **except one**: the stale exit-2 stage text
(TC-2, ansible-syntax F3, qa-all B27-B31), which the author saw and left (F1).

## Findings, ranked

### F1 — FIX — the `docs` stage row still says "zero-file scan or checker crash"; the precedent says fix it and pin the golden

`scripts/qa-gates.tsv` row `stage docs`: `ERROR: docs gate could not produce a result (zero-file scan or checker crash).` There is no checker to crash any more (B11: one program),
and the tool exits 2 for a different and longer list: bad usage, an unresolvable root, an empty
scope, a document or anchor target that cannot be read, a playbook list that does not parse or
imports nothing, a refused gate table, a QA document with no gate row, a missing catalogue or
index document, a git that fails, an unwritable report, a closed stdout (D1, D2, D5, D6,
D17-D21, D25). `qa-all` prints this row's text as the LAST line of an aborted run, after the
tool's true `ERROR:` line, so on every one of those causes the last line the reader sees names
the wrong thing. SPEC-qa-all B27-B31 fixed exactly this for `ansible-syntax`, `bash`, `python`,
`patterns` and `js` and pinned each as a deviation; the journal's reason for not doing it here
("changing it moves two qa-all goldens") is the cost those five rows already paid, and the
count is one, not two: the only golden that carries the text is
`src-go/qa/all/testdata/golden/exit2-docs.json` (grep over `src-go`).

Fix: change the row to the shape the five siblings use (`ERROR: docs gate could not produce a result (an empty scope, an unread or unparsed file, a broken discovery, or the tool failed: see the ERROR line above).`), keeping the `ERROR: docs gate could not produce` opening that
`TOOL_ABORT` reads; re-record `exit2-docs.json`; add SPEC-qa-all **B32** ("`QA` exit-2 aborts",
the `docs` row) and the `exit2-docs` entry in `src-go/qa/all/testdata/deviations.json`, the way
B31/`exit2-js` is written. Correct the journal's "two goldens" in a correction entry.

### F2 — FIX — the anchor is slugged before comparison, so an anchor that is broken in a browser passes (D15)

`src-go/qa/docs/links.go:128` compares `doc.headings[slugOf(anchor)]`, and
`src-go/qa/docs/links_test.go:231` pins it: `{"## Plain Words", "Plain-Words", true}`. A
rendered heading has `id="plain-words"`; a fragment is matched by the browser byte for byte, so
`#Plain-Words`, `#plain%20words` (decoded `plain words`, slugged `plain-words`) and
`#plain_words`-style near misses are dead links on GitHub and clean here. Spec D15's last clause
("the anchor is compared as `slug(anchor)`") writes the Python's leniency
(`link_check.py:518`, `slug(frag)`) into the new spec, against the method's own rule that the
old code is not an oracle. The live tree has no anchor that is not already a slug (grep of
every `](...#...)` in the population for an upper-case letter, a `%` or a space: none), so the
tightening costs nothing today.

Fix: compare the percent-decoded anchor verbatim (`doc.headings[anchor]`); change D15's last
clause and the `Plain-Words` case to `false`; add a `#plain%20words` false case.

### F3 — FIX — any QA.md table whose first cell is a simple code span is read as the gate inventory (D21)

`src-go/qa/docs/markdown.go:99-117` (`rowOf`, `gateName = ^[A-Za-z0-9._-]+$`) and
`catalogue.go:206-225`. The fixture's `| `QA_JSON_OUT` | report |` in a second table produced
`CLAUDE/QA.md:10  QA_JSON_OUT  — this document lists the gate but qa-all.bash does not run it`.
The live `CLAUDE/QA.md` escapes this only by accident: its non-gate tables have first cells with
parentheses (`HelperCounts()`, L339-341), slashes (`scripts/qa-toolchain.bash`, L960-963) or two
spans (`.js`/`.mjs`, L1097). The first author to add an environment-variable or flag table to
QA.md (it already documents `QA_JSON_OUT`, `GO_COVERAGE_FLOOR`, `QA_BASH_LIMITS_BASE` in prose)
turns the gate red with a message that is wrong. D21 states the rule, so this is a spec defect
rather than a code defect, and a cheap one to close.

Fix: a documented gate's name has the shape D20 gives it — `^[A-Za-z0-9_-]+\.bash$` or
`^helpers(\.[A-Za-z0-9_]+)+$` — so use that shape in `gateName` and in D21, and add a test with
a code-span first cell of another shape in a second table.

### F4 — FIX (routing) — deleting or renaming any file a document links to does not run the docs check before the commit

`scripts/qa-routing.tsv` routes `docs` for `*.md` (first row L124, whole, so a deleted document
qualifies: `classify.go:203-214`), for playbooks and `playbooks/*` YAML
(`classify.go:253-269`, `r.yaml && !deleted` so never for a deletion), and for the gate's own
inputs (the new `also` row, B48). Nothing routes it for the deletion or rename of a `.bash`,
`.py`, `.yml`, `.tsv` or `.json` file that a core document links to, which is the most common
way a link breaks: the journal's own example (`CLAUDE/QA.md` linking the deleted
`helpers/docs/link_check.py`) ran the docs check only because QA.md was also edited; the
deletion alone would have passed `qa-changed` and failed `qa-all`. B48's text claims the
"never ran the docs check" holes are closed; this one is open and stated nowhere.

Fix: the gate takes 0.4 s, so route `docs` for every deleted path (a `post` rule on the
deleted set, or `deleted && !def.whole`'s sibling: a `whole` check that every deletion owes),
state it in SPEC-qa-changed beside B48, and add the classify test (`w.classify(nil, "scripts/gone.bash")` has `docs`). If that is refused, write the limit into B48 the way B42
writes its stated limit.

### F5 — LOW — a GitHub footnote definition whose body looks like a path is a false finding

`markdown.go:40` builds the parser with `extension.Table` only. Without `extension.Footnote`,
`text[^1]` + `[^1]: docs/x.md` parses as a reference link and the fixture reported
`README.md:10  docs/missing-footnote.md  — target does not exist`. No in-scope document uses a
footnote today (grep `[^`), so this is latent. Adding `extension.Footnote` makes `[^n]` a
footnote and costs nothing; D8 should then say footnotes are not links.

### F6 — LOW — the topic-index token is the bare file name, so a path elsewhere satisfies it (D19)

`catalogue.go:133-165`: the token for `CLAUDE/Other.md` is `Other.md`, and `mentions` accepts
it preceded by `/`, so the fixture's index that named only `CLAUDE/core/Other.md` passed for
`CLAUDE/Other.md`. Prose anywhere in `CLAUDE.md` ("see CLAUDE/Release.md") also counts as "a
row in the index". Both are what D19 says; neither bites on the live tree. If a row is meant,
require the token `CLAUDE/<name>` (not preceded by a letter, digit, `_`, `-`, `.` or `/`), which
also removes the `core/` collision.

### F7 — LOW — `.claude/rules/qa-gates.md` still triggers on `helpers/docs/**`

`.claude/rules/qa-gates.md:10` lists `"helpers/docs/**"` in `paths:`; the tree no longer exists.
Replace with `src-go/qa/docs/**` and `src-go/qa/cmd/qa-docs/**` (the rule's purpose is to load
when a gate is edited, and this gate's code moved). No other reference to `helpers/docs` or
`link_check` survives outside plan documents; `tests/helpers/docs/` is gone with its
`__init__`, nothing imports it, CI's helper-tests gate counts modules dynamically.

### F8 — LOW — plan documents left pointing at the Python

`CLAUDE/Plan/00192-.../INVENTORY.md:133,156` still list `helpers/docs/link_check.py` as a
standalone Python CLI awaiting a port (P10). `SPEC-qa-all.md:17,479` name `link_check.py` as
the reader of `qa_gates`; those are bash-era history and can stay, but the INVENTORY row is a
to-do list and is now wrong. `CLAUDE/QA.md:68` scope column says `CLAUDE/*.md` while D4 and
`scope.go:29` scan everything under `CLAUDE/` except `Plan/` (including the daemon-owned
`CLAUDE/core/*.md`, three tracked files; a daemon upgrade that breaks a link there fails the
core gate for a file this repo must not edit, which is worth one sentence in D4).

### F9 — LOW — goldmark: justified and pinned, but a year behind

`src-go/qa/go.mod`: `github.com/yuin/goldmark v1.7.13`; `go.sum` carries both hashes and no
transitive dependency was added, so the choice is sound (CommonMark-conformant, GFM tables, the
Go module ecosystem's standard parser, zero dependencies; the main module stays
dependency-free). `go list -m -versions` shows v1.8.6 current. Nothing records why 1.7.13; pick
the current release at merge or write the reason in the journal.

### F10 — LOW — duplicated logic and a purity claim the code does not keep

- `src-go/qa/cmd/qa-docs/main.go:41-69`: `stdoutWasClosed`, `devNullDevice`, `writeReport` are
  the eighth copy of the same forty lines (`cmd/qa-bash`, `qa-python`, `qa-js`, `qa-patterns`,
  `qa-ansible`, `qa-ansible-syntax` carry the others). Not this branch's debt alone, but each
  port adds one; a `qa/internal/cmdmain` would end it.
- `markdown.go:68-84` re-implements `goldmark/ast.Walk`.
- `links.go:212-224,229-239` call `os.Stat`/`os.Lstat` directly while SPEC §4 says the package
  is "pure except for the injected `Env`" and "the file system is read through the root".
  Either add a `Stat` to `Env` or make §4 say what is true.

### F11 — INFO — exit-codes suite, qa-changed wiring, discovery lists: checked and sound

- `scripts/test-qa-docs-exit-codes.bash` drives the real wrapper over fixture trees outside
  the checkout, needles are branch-specific, exit 1 is proven distinct from exit 2 with a valid
  payload; row `docs-exit-codes` is in `qa-gates.tsv` and QA.md. One nit: the header says "Two
  of them need a tree" and lists three.
- `qa/changed`: `docs` is `whole` and needs `go` (checks.go:79); the `also` row and the three
  tests, one against the real routing table, hold; the `file-scripts-qa-docs-bash` golden
  change is exactly `check docs` and is pinned as B48.
- `qa/discovery` D18/D19: `markdown` may be empty (the caller judges), `tracked-files` drops
  `--deleted` paths and refuses an empty list; four tests, each cited.
- D24 holds with a symlinked root in both the unit and the real-git test; the `Details:` line
  prints `QA_JSON_OUT`, which is the caller's path, not the checkout's.
- No install-specific name in the diff (`/etc/hostname` is a test fixture string).

## What held up against the hunt list

Empty scope exit 2 (D5, suite and real-git test). Every document read or exit 2 (D6, including
an anchor target outside the population). Links the walk could miss: reference, collapsed and
shortcut references, titled, angle-bracket, in table cells, headings, quotations, nested lists,
links around images, CRLF — all found; images, autolinks, raw HTML, code spans, fences, indented
code, HTML comments, unused reference definitions — none found, as D8 says (the HTML-link and
image exclusions are the spec's choice and the live tree has neither). Slugs: em-dash double
hyphen, `&amp;` double hyphen, `\*` escapes, emphasis and code in headings, `:rocket:`, `A: B; C`, `Émile`, `日本語`, Setext, headings in lists and quotations, fenced "headings" rejected,
duplicates numbered `-1`/`-2` the way github-slugger numbers them (including the `Same-1`
collision). Gitignored and untracked targets, directories with no tracked file, `..` and `../`
are findings; a tracked symlink is followed for its headings. The QA.md inventory is read from
the parser's table rows in both directions at the row's own line. `playbook-main.yml` is YAML:
a quoted import, a commented import, a `hosts:` play, a non-string import, an empty list.

Fixture evidence (anonymous, no paths): 23 findings from 41 planted links, each the expected
one; 0 unexpected passes other than F2 and F6 above; 1 unexpected finding (F5).
