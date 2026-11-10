# Plan 00241 Phase 3: v2 renders markdown as HTML

Tasks 3.1-3.3 are done on F44. The code is in `src-go/internal/teamchat/richtext` (new) and
`src-go/internal/teamchat/proto` (`send.go`, `content.go`, `format_test.go`).

## What changed

- **`richtext.HTML(markdown)`**: goldmark parses the markdown (CommonMark plus GitHub's table
  extension), and the package writes the HTML itself, node by node. It covers headings,
  paragraphs (a soft line break becomes `<br>`), nested `-`/`*`/`1.` lists (with `start`),
  emphasis, code spans, fenced and indented code (`language-x` only for a plain name), quotes,
  `<hr>` and tables (`thead`/`tbody`/`tr`/`th`/`td`). A node it does not draw is shown as
  escaped text: raw HTML, inline or a block. Links work only for `http`/`https` with a host,
  or for `mailto`. Any other destination keeps its text but loses the link, and this holds
  after entities and escapes are resolved (`javascript&#58;`). An image becomes a link to it.
  R1-R12 are in the package doc. The tests are expectation tables plus a check that the output
  holds only allowed tags.
- **Sender**: `formatted()` adds `format: org.matrix.custom.html` and `formatted_body` beside
  the plain `body` (which stays the markdown) for reports, questions, thread posts and
  `teamchat.text`. Pings, notices and carried items are generated lines, so they stay plain.
  If the HTML would push the content past `MaxContent` (16384), the event goes out plain
  rather than being refused.
- **Receiver**: `DecodeContent` used to drop any `format` as `schema`. It now accepts the
  exact pair: that format string with a string `formatted_body`. Anything else is still
  `schema`, and a receiver never reads the HTML.
- **Dependency**: `github.com/yuin/goldmark v1.8.6` is added to the main module. It has no
  dependencies and is MIT licensed; the qa module already uses it. It is vendored and has an
  allowlist row. goldmark's own HTML renderer is not used.
- **Docs**: DESIGN-v2-A 7.1 has a new `format` row and OD-A9 is now (b). STYLE.md is version 3:
  tables are allowed on v2, with rules that keep them readable as plain text. PLAN.md has the
  Phase 3 tasks ticked, with the decisions recorded.

## Findings for the coordinator

- **Rollout order matters.** A v2 binary from before this change drops any formatted event as
  `schema`. Every v2 member must run the new binary before any member sends a formatted post.
- **Element shows no table column alignment.** Matrix's recommended subset gives `th`/`td` no
  attribute for it, so `:--:` is parsed and then ignored.
- **No new defect class was found.** v1 stays frozen and its converter's limits were not
  ported. The bare-HTML hazard is removed by design: only `richtext` writes markup. No
  detector rule was added.

## Commit status: not committed

- The pre-commit hook refused the commit because `go.mod` and `go.sum` count as CCY files, so
  CCY_VERSION must be bumped.
- CCY_VERSION is in `files/var/local/claude-yolo/claude-yolo`, which the launcher agent has
  changed and not committed. A pathspec commit of that file would take their hunks too, so the
  bump was left to the coordinator.
- Everything is staged. The message is
  `untracked/scratch/commit-00241-phase3-261110.txt`.
- `qa-changed` is red only on the Go analyzers stage, at `internal/vmtest/link.go:146,188`
  (`project_walk_open`). That file is another agent's uncommitted work. Every other check
  passed, including go-deps, go-vuln, docs, teamchat-keeps-off-v1 and all the suites.

## Verification

- `go test ./internal/teamchat/...` passes.
- `golangci-lint` reports 0 issues on `richtext` and `proto`.
- Coverage is 90.3% for `richtext` and 98.4% for `proto`.
- The red runs came first: `HTML` was undefined, the `format` key was missing, and `ping ref`
  was refused.
