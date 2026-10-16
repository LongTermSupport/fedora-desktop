# Plan 00204 Task 2.1: the dependency infrastructure

Branch `feat/go-vendored-deps`, cut from F44. The owner approved D2 (vendored, pinned
dependencies) and accepted D1 (the Charm stack) as "go with Go best practice"; both are recorded
in [PLAN.md](../PLAN.md#decisions) in his words, with the Composer explainer. D3-D8 stay open.
No UI migration: that waits for `ui.Action` from `dbf/hidden-bindings`.

## Modules

All in `src-go/go.mod`, each on `src-go/go-deps.allowlist` with its reason.

| Module                                  | Version                            | Why                                                                    |
| --------------------------------------- | ---------------------------------- | ---------------------------------------------------------------------- |
| `charm.land/bubbletea/v2`               | v2.1.0                             | direct: the TUI engine                                                 |
| `charm.land/bubbles/v2`                 | v2.2.1                             | direct: key bindings, help footer (list, input, viewport later)        |
| `charm.land/lipgloss/v2`                | v2.0.6                             | direct: styling and layout                                             |
| `github.com/charmbracelet/x/ansi`       | v0.11.9                            | direct (frame's tests strip styling); required by all three            |
| `github.com/charmbracelet/colorprofile` | v0.4.3                             | colour detection and downsampling                                      |
| `github.com/charmbracelet/ultraviolet`  | v0.0.0-20261008173134-6b8d4baf91b4 | Bubble Tea v2's renderer; **the one pseudo-version** (never tagged)    |
| `github.com/charmbracelet/x/term`       | v0.2.2                             | raw mode, window size                                                  |
| `github.com/charmbracelet/x/termios`    | v0.1.1                             | termios calls (Ultraviolet)                                            |
| `github.com/charmbracelet/x/windows`    | v0.2.2                             | Windows console calls, never compiled for Linux                        |
| `github.com/clipperhouse/displaywidth`  | v0.11.0                            | string width in cells (Lip Gloss)                                      |
| `github.com/clipperhouse/uax29/v2`      | v2.7.0                             | grapheme segmentation (x/ansi)                                         |
| `github.com/lucasb-eyer/go-colorful`    | v1.4.1                             | colour conversion                                                      |
| `github.com/mattn/go-runewidth`         | v0.0.30                            | wide-rune widths (x/ansi)                                              |
| `github.com/muesli/cancelreader`        | v0.2.2                             | cancellable terminal read, so the TUI can hand over to a child process |
| `github.com/rivo/uniseg`                | v0.4.7                             | grapheme clusters (Lip Gloss)                                          |
| `github.com/xo/terminfo`                | v1.0.0                             | terminfo reader (colorprofile)                                         |
| `golang.org/x/sync`                     | v0.22.0                            | errgroup (Ultraviolet)                                                 |
| `golang.org/x/sys`                      | v0.47.0                            | terminal ioctls                                                        |

govulncheck v1.9.0 is pinned by the `tool` directive of `src-go/qa/go.mod` (QA module, not
vendored, not in the binaries).

## Sizes against the research

| Measure                      | Research (with Huh) | Now (no Huh)                                         |
| ---------------------------- | ------------------- | ---------------------------------------------------- |
| modules                      | 26                  | 18                                                   |
| `vendor/` on disk            | 15 MB               | 14 MB                                                |
| `.go` files / lines          | 638 / 295 k         | 570 / 278 k                                          |
| files in `vendor/`           | not counted         | 674                                                  |
| scratch binary over hello    | +3.4 MB             | +2.9 MB (5.29 MB against 2.39 MB)                    |
| `ccy-teams` / `ccy-sessions` | 6.0 / 4.5 MB        | 6.07 / 4.54 MB, unchanged (they do not import frame) |

Built offline: `GOFLAGS=-mod=vendor GOPROXY=off go build -trimpath -buildvcs=false`, Go 1.26.9.

## Gates

| Row (`qa-gates.tsv`) | Script                    | Checks                                                                                                                                                                                                        |
| -------------------- | ------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `go-deps`            | `scripts/qa-go-deps.bash` | every required module allowlisted with a reason; a pseudo-version or `replace` needs its own row; no stale row; `go mod verify`; `go mod tidy -diff` empty; `go mod vendor -o` equals `vendor/` byte for byte |
| `go-vuln`            | `scripts/qa-go-vuln.bash` | `go tool govulncheck -C src-go ./...` over the vendored tree; status 3 is exit 1, any other failure exit 2 (never a pass)                                                                                     |

Real tree: `GO-DEPS-OK 18 module(s) allowlisted (1 at a pseudo-version, 0 replaced); go.sum verified, tidy, vendor/ in sync (674 files)` and `GO-VULN-OK src-go: govulncheck found no vulnerability the code reaches`. Red controls (each undone): an edited vendored file, a removed
pseudo row plus a stale row, and a `replace` directive each fail with exit 1 naming the cause.
Spec: [SPEC-qa-go-deps.md](../SPEC-qa-go-deps.md). Wired into `qa-routing.tsv` and
`qa/changed` (`go-deps`, `go-vuln`), QA.md, `qa-trees.tsv` (`src-go/vendor` excluded from the
shell and Python gates) and the bash-limits exemptions. qa-changed's golden replay gained
deviation B48; qa-all's replay treats the two gates as added after the port.

## Build paths

- `tasks/build-go-command.yml`: `GOFLAGS=-mod=vendor` for src-go builds; the `go_command_module`
  branch (md-viewer, an external module) keeps `-mod=readonly`.
- `scripts/qa-go.bash`, the test suites that build binaries, golangci-lint and `qa-changed`'s
  closure all run in `src-go`, where a `vendor/` directory makes `-mod=vendor` Go's default.
- CI: `setup-go` reads `src-go/go.mod`. `go mod tidy` normalises `go 1.26` to `go 1.26.0`, which
  `setup-go` would install exactly, so `go.mod` carries `toolchain go1.26.9` (the CCY image's
  `GO_VERSION`). The qa module and govulncheck come from the proxy, as the qa module already did.

## The pre-commit secret scan

Three vendored files carry their authors' real contact addresses (the Bubble Tea and Lip Gloss
upgrade guides, go-runewidth's SECURITY.md), and the hook rejected the commit. They cannot be
edited (the vendor tree must be byte for byte what `go mod vendor` writes), so the hook's scan
list excludes `src-go/vendor` by pathspec on its existing line (the allowlisted hook does not
grow). Test first in `scripts/test-ccy-container-version-hook.bash`, the suite that drives the
real hook: a vendored address passes, the same address elsewhere is still rejected. gitleaks in
CI still scans the whole tree, and finds nothing in `src-go/vendor`.

## Not done, and why

- No host run queued in `meta-deploy.list`: the deployed binaries do not import frame, so the
  next `play-claude-yolo.yml` builds the same binaries, now from `vendor/`.
- No CCY or image bump: govulncheck needs no install anywhere.
- `project_seam_imports` (only `internal/ui` imports Charm) is Task 2.2; src-go/CLAUDE.md says
  review holds it until then.
