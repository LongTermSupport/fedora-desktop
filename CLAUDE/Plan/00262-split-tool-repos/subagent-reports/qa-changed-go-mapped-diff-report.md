# qa-changed: a routed file outside src-go/ runs go-qa's diff mode, not the full gate

## What changed

- **go-qa-ci** (PR 20, merged as `774d1a4`): `go-qa run --changed --also-packages <dir>,...`
  (repeatable). The named packages and their importers join the diff run as if changed; an entry
  is a root-relative package directory or `<dir>/...`; one naming no package is exit 2. They are
  tested without `-race` and not counted by coverage (none of their statements changed). The scope
  line counts them.
- **fedora-desktop**: `src-go/qa/go.mod` pins that commit. `scripts/qa-routing.tsv` has an optional
  fifth column, the go packages: required on a row routing a file outside `src-go/` to `go`,
  refused elsewhere; `@full` for the files that can change every Go result. `runGoGate` runs
  `--changed --base REF --also-packages <sorted union>`; the whole gate only for `@full`, an
  outside file no row named, or `--files`.

## Explicit file list

`--files` keeps the whole gate. go-qa's diff mode computes its change from git, not from the
list, so a mapped diff would judge a different change than the one named.

## Mapping (row, then packages; relative to src-go)

| Row                                                                               | Packages                                                                                                                                                                |
| --------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ccy launcher, `lib/*.bash`, Dockerfile                                            | internal/ccylaunch/..., internal/ccclaude, internal/registry, internal/restore, internal/screen, internal/sessions, internal/transcript                                 |
| `helpers/pingbus/*.py`, `agent_bus/*.py`, `agent_bus_lxc/*.py`, `stdin_text/*.py` | internal/bus/declare, internal/bus/seat, internal/declare, internal/teams/lxcadd, internal/pingbus/statusline/..., internal/teams/membership, qa/cmd/qa-vmtest-manifest |
| `tests/helpers/pingbus/fixtures/tuwunel/*` (new row)                              | internal/teamchat/matrix/fakehs                                                                                                                                         |
| Matrix clients list, play, tasks, settings, agent-bus-claude                      | internal/matrixclients                                                                                                                                                  |
| `rclone-cache-warm`                                                               | internal/rclonecache                                                                                                                                                    |
| `files/*.service`, drop-ins, agent-bus resolve                                    | internal/unitsandbox, internal/teamchat/v1names                                                                                                                         |
| Watchlist list and run manifests                                                  | internal/watchlist                                                                                                                                                      |
| `tests/fixtures/ansible-pause/*`                                                  | qa/ansiblepause, qa/cmd/qa-ansible-pause                                                                                                                                |
| workflows and actions                                                             | qa/toolchain, qa/pushgate                                                                                                                                               |
| `vars/golang.yml`, `play-md-viewer.yml`                                           | qa/toolchain                                                                                                                                                            |
| `helpers/version_pins/manifest.py`                                                | qa/cmd/qa-version-pins                                                                                                                                                  |
| `tests/fixtures/ansible-{regex-backref,regex-class-escape,root-umask,sudoers}/*`  | qa/ansible                                                                                                                                                              |

Always full (`@full`): `scripts/qa-routing.tsv`, `scripts/qa-trees.tsv`, `scripts/qa-gates.tsv`,
`scripts/qa-go-qa.bash`, `.qa-versions`.

Routing gaps found and fixed on the way: the Python bus rows routed only `declare.py`, `admin.py`,
`checkout.py` and `helpers/pingbus/*.py`, but the parity oracles import the rest of
`helpers/agent_bus`, `helpers/agent_bus_lxc` and `helpers/stdin_text`, and `internal/bus/seat`'s
parity runs `cli.py`; the Tuwunel captures the fake homeserver replays were not routed to Go.

## Tests

- `route_test.go`: the column parsed (`packages`, `@full`, a src-go row without it); 21 refusals,
  each naming the line.
- `checks_test.go` `TestTheGoGateRunsGoQaOverTheSameChange`: eight rows classified by the real
  table, then the go check run: src-go only (diff, nothing added); a mapped launcher library;
  a mapped file beside src-go; three mapped rows (union, sorted); a routing table, `.qa-versions`,
  the wrapper (whole); `--files` (whole). `TestTheGoGateRunsWholeForAnOutsideFileNoRowNamed`.
  Proven red by three mutations: ignoring the full reasons fails the four whole rows; the old
  "outside means whole" rule fails the three mapped rows; dropping the unnamed-file guard fails the
  last test.
- `realtree_test.go`: `TestEveryRealGoReaderOfARoutedFileIsNamedOnItsRow` (a src-go package whose
  Go source names a routed path in a string literal must be on the row; mention-only pairs listed
  with reasons). It was red first: it found `qa/pushgate`, `internal/teamchat/v1names` and
  `qa/cmd/qa-vmtest-manifest`, now on their rows. `TestEveryRealGoPackageColumnNamesPackagesThatExist`.
- go-qa-ci: `internal/changed` (seven tests) and `internal/cli` (scope line, usage errors, help).

## Known limits

- The reader test sees string literals only; a path built with `filepath.Join` pieces (difftest,
  the Python bus parity tests) is named in the row's comment instead.
- Locally, Go's test cache does not recheck a test whose input lies outside its module, so a cached
  pass of a named package can survive the change; this was already true of the full local run. CI
  is never cached.
