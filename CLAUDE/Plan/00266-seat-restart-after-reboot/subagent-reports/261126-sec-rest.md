# sec-rest: review findings 7, 8 and the rest of 10 (report and handoff)

This lane finishes the open findings of
[261126-phase-a-security-review.md](261126-phase-a-security-review.md), which the sec-links
([261126-sec-links.md](261126-sec-links.md)) and sec-pin ([261126-sec-pin.md](261126-sec-pin.md))
lanes left open. Every commit went from a temporary index, holding HEAD's content plus this
lane's own lines in the shared files, and each one is pushed to v2-smoke.

## Done

| Finding      | Commit      | ccy       | What changed                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ------------ | ----------- | --------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 10 (Go)      | `f28cb4a70` | 3.124.123 | `project_checkout_path_seam` is registered in `rules.All()` (74 at HEAD) and so blocks, with a GoProjectRules.md row and a red/green section. It was red on seven sites. Five host sites now go through an `os.Root`, each with a test that plants a link: `allowedhosts` (a link at the name is refused, H7), the `conversation` seat-record listing, the `membership` keeper report, and the chooser's `.claude/ccy` + `.gitignore`. The last is finding 6's twin: a linked `.claude` sent both outside the checkout, so the file is now an exclusive create. The two `teamtest` sites are test helpers: a function that takes `*testing.T`/`B`/`F` or `testing.TB` is exempt (fixture cases). |
| 10 (compose) | `01512eeed` | 3.124.124 | `checkoutpin.ComposeFiles` and `project_checkout_config_pin` name `container-compose.{yml,yaml}` and their overrides (red: `TestComposeDigest`, two fixture lines). The semgrep rule `ccy-host-applies-checkout-config` catches `podman compose` and options before the verb (red on two new fixture functions under the old rule). A sweep of the launcher tree finds nothing. QA.md and GoProjectRules.md are updated.                                                                                                                                                                                                                                                                         |
| 8            | `c9579afe1` | 3.124.125 | Attended `--read-launch-config` of choices ccy did not write: at a terminal, the front shows each one (through `Shown`) and asks `YesNo`, default no. A yes records them; Enter or no hands nothing back (exit 0, and the launcher asks each choice); with no terminal they are refused. Three tests were red first. The ccy-teams suite's attended case is updated (green).                                                                                                                                                                                                                                                                                                                     |
| 7            | `816f46c0d` | 3.124.126 | `KindDockerfile` and the verb `ccy-teams internal checkout-pin dockerfile true\|false DIR` (D1-D3 in `checkoutpin/dockerfile.go`). The file is read once through an `os.Root`, its digest verified (asking at a terminal, default no), and those bytes copied 0600 into a private `mktemp -d`. The launcher hashes and builds that copy. An unattended refusal goes through `ccy_unattended_stop`. `ccy-teams approve dockerfile` is added. The launcher block shrank, so its ratchet row is lowered to 2695/12/2628. The difftest passes.                                                                                                                                                       |

Also fixed in code these commits touch:

- `project_checkout_config_pin` now names `.claude/ccy/Dockerfile`. It was red on
  `ccylaunch/inrepo.go:64`, which now uses `checkoutpin.Dockerfile`.
- That rule no longer flags a use of another package's constant (`checkoutpin.Mounts`), as it
  never flagged a local one. It had flagged its own prescribed fix.
  **Owner to confirm**: this is a correction of a false positive, not a narrowing of the class.
  The constant is still reported where it is written.
- checkoutpin's two hand-rolled `~` checks (`pin.go` resolve, `composerefs.go` bind sources)
  go through `term.ExpandHome`. They were red at HEAD under `project_homeabbrev`.
  A `~user/...` bind source is now read as a named volume. Compose refuses that name.
- The new seat-record listing is sorted (`project_listing_order`).

## Verified

- `go test` passes on checkoutpin, ccylaunch (and difftest), conversation, allowedhosts,
  sessions, `teams/...` and `qa/rules`.
- golangci-lint reports 0 issues on the changed packages.
- The project-rules driver (`go vet -vettool`) is green over both modules and the
  ccyteamstest, difftest, busfailpoints and tuwunel builds.
- `semgrep --test` on the HEAD-plus-mine rule file and fixture: 15/15.

## Not verified, and left

1. **The bash half of finding 10's class** (sec-links item 3) is **not done**: semgrep naming
   any checkout `.claude/ccy` or `$PWD/` path outside a comment, message, heredoc, existence
   test or git pathspec. Its red list is in sec-links' report:

   - `claude-yolo`: the sessions migration, `--prevent`, `--custom`/`--custom-docker` handing
     `.claude/ccy` to `dockerfile-custom.bash`, the `$PWD/.claude` chmod sweep, and the restart
     request;
   - `restart-request.bash:34`, `session-lifecycle.bash:205` and `common.bash`'s
     `allowed-hostnames` read.

   Those files are sec-tmux's working area. The rule has to be written red first and its sweep
   done before it blocks.

2. **`--custom`/`--custom-docker`** still write `.claude/ccy` by path through
   `dockerfile-custom.bash`. The build now refuses an unapproved result unattended, and asks
   about it attended.

3. **`./scripts/qa-changed.bash`** was run over the shared working tree, which holds other
   lanes' uncommitted edits. Its verdict is in the reply to the lead, not here.

4. **`scripts/test-ccy-teams.bash`** has 34 failures, all in "the real launcher: refusals
   before anything runs" (`--headless --teams` and seat-check exit codes, 1 where 64 or 78 is
   wanted). Its Quick Launch checks pass. The failures were run against the shared working
   claude-yolo, whose uncommitted `CCY_VERSION` line (3.124.118) is not HEAD's. They are not
   triaged.

5. **podman-compose's default file list** was not read here: no podman-compose is installed.
   Adding `container-compose.*` to the pinned set is fail-safe either way.

6. **PLAN.md task lines** for findings 7, 8 and 10 are not updated. PLAN.md holds another
   lane's uncommitted edits.

## Notes for the coordinator

- The working tree's `scripts/bash-limits.allowlist` raises `common.bash`'s row from 531 to
  641 code lines. That is not this lane's change, and the ratchet's `--base` check refuses it.
- `.semgrep/bash-conventions.bash` fails `bash -n` at line 701 at HEAD. It is a semgrep
  fixture, so it is exempt, but the lint-on-edit hook reports it on every write of a copy.
