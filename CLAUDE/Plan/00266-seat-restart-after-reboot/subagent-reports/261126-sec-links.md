# sec-links: review findings 1, 6, 7 and part of 10 (report and handoff)

The lane is part done. Findings 1 and 6 are fixed and pushed. Finding 7 and most of the
detector sweep are not done yet; they are handed off below. The detector is written and
proven red, but it is **not registered** in `qa/rules/table.go` and not blocking, because the
sweep below is not finished.

## Done

- **8dec0ed53, findings 1 and 6 (ccy 3.124.121).**
  - New seam `internal/checkoutpin/checkoutfile.go` (C1-C3): `ReadFile`, `WriteFile` and
    `CopyOut`, each through an `os.Root` of the checkout. A link at the name is refused and
    named. A write is an exclusive temporary renamed into place.
  - New front questions, F25 in `internal/ccylaunch/checkoutfiles.go`:
    - `--env-local-copy CHECKOUT DIR` copies `ccy.env.local` into `CONFIG_TEMP`, and the
      launcher binds that copy.
    - `--checkout-files CHECKOUT gitignore|dist` writes the `.gitignore` and the dist.
  - F12's prelude (`inrepo.go`) and `--prevent`'s append go through the same seam.
  - In `common.bash`, the bash now asks the front. Its copies of the two texts are gone, it is
    110 code lines shorter, and the allowlist row is lowered.
  - Tests with real planted links:
    - Go: checkoutpin C1-C3; ccylaunch `TestEnvLocalCopy*` and `TestCheckoutFiles*`.
    - Bash: `scripts/test-ccy-env-local-dist.bash` (42 pass) and
      `scripts/test-ccy-gitignore-safety.bash` (13 pass). Both now build the front.
  - The launcher difftest passes, with no golden re-recorded.
- **Detector, Go half (committed with this report, unregistered):**
  - `qa/rules/checkoutpath.go`, `project_checkout_path_seam`, with its test and
    `testdata/checkoutpath`.
  - It flags a by-name call on a `.claude/ccy` path outside `os.Root` and checkoutpin:
    `os.ReadFile`/`WriteFile`/`Open`/`OpenFile`/`MkdirAll`/`RemoveAll`/`ReadDir`/`OpenRoot`/…,
    `filepath.Walk`/`WalkDir` and `fsx.WriteAtomic`.
  - It follows the path through constants, joins (`".claude","ccy"` pieces included), locals,
    package functions and helper arguments.
  - Proven red on the finding-6 shape in the fixture. Against the real module it was red on
    `inrepo.go`'s four calls (now fixed) and on the instances listed below.

## Left (the next agent's)

1. **Finding 7, the custom Dockerfile** (`claude-yolo:2023-2135`).
   - Add `KindDockerfile` (file `.claude/ccy/Dockerfile`) to checkoutpin, and add a verb
     `ccy-teams internal checkout-pin dockerfile true|false DIR`.
   - The verb reads the Dockerfile through `ReadFile` and verifies its digest.
   - Unattended: a Dockerfile nobody approved is exit 1, naming the file and
     `ccy-teams approve dockerfile <checkout>`.
   - Attended: show the content and ask with a default No, then `CopyOut` it into
     `CONFIG_TEMP`. The bash then builds `-f <copy>` and takes `md5sum` of the copy.
   - `pin.go`, `command.go` and `Approve`'s kinds list belong to sec-pin; keep the hunks small.
   - Move the bash block into a `common.bash` function so `claude-yolo` shrinks.
   - `CONFIG_TEMP` is created at `claude-yolo:2317`, after the build at 2028, so the copy
     needs its own private `mktemp -d`, or the block has to move below it.
2. **Go sweep, still red under `project_checkout_path_seam`:**
   - `internal/allowedhosts/allowedhosts.go:63`
   - `internal/conversation/seatrecord.go:323` (a `ReadDir`; the package already has a root)
   - `internal/teams/membership/report.go:82`
   - `internal/teams/chooser/ccydir.go:43,47`: **finding 6's twin**, a `.gitignore` write
     through a link, in ccy-teams
   - `internal/teams/teamtest/world.go:366,406`: test scaffolding outside a `_test.go` file.
     Decide whether `*test` packages are in the class; my view is that they are not host code.
   - Then register `CheckoutPathSeam` in `table.go` `All()`, bump `TestAllIsValid`'s count
     (other lanes have moved it to 75 in the tree), add its `GoProjectRules.md` row with the
     red/green proof, and run the driver over the tagged builds too.
3. **Bash sweep: the semgrep rule `ccy-host-applies-checkout-config` broadened to "names a
   checkout `.claude/ccy` or `$PWD/` path outside a comment, a message, a heredoc, an
   existence test or a git pathspec".**
   - The probe is in `untracked/scratch/seclinks/rule.yml` (pattern 4). It still needs:
     - the lookbehinds on the unquoted `[\s=(]` alternative;
     - an exclusion for `git ... ls-files|check-ignore` pathspecs;
     - fixture lines in `.semgrep/bash-conventions.bash`.
   - Red, at HEAD before 8dec0ed53, on:
     - `common.bash` lines 438, 656, 662, 666, 731 and 797. 438, 656, 662 and 666 are fixed;
       797 is inlined into git and test lines.
     - `claude-yolo`: 484-516 (the sessions migration's `find`, `rm -rf`, `rmdir`), 1129-1130
       (`--prevent`'s bash path), 1170 and 1176 (`--custom`/`--custom-docker` hand
       `.claude/ccy` to `dockerfile-custom.bash`, which writes there), 2023 (finding 7), 2262
       (the `$PWD/.claude` chmod sweep, a check-to-use race), 3656 and 3764 (the restart
       request).
     - `restart-request.bash:34`.
     - `session-lifecycle.bash:205`, the supervisor read. semgrep cannot parse that file, so
       check how qa-patterns treats it.
     - `common.bash:731`, the `allowed-hostnames` read.
   - Each read can go through a front question that prints the bytes through `ReadFile`.
4. **Docs:** QA.md's `ccy-host-applies-checkout-config` entry and GoProjectRules.md, for the new
   class definition.

## Not verified

- No podman here, so I could not confirm that podman follows a link in a bind source.
  - Finding 1's fix does not depend on it: the source is now a private copy.
  - The copy is 0600 and owned by the host user. That is readable by the container's root
    under rootless podman, but not proven under `--engine docker` with userns remapping.
- No full `qa-all` run; that is the lead's.
- The rules package's `TestAllIsValid` fails in the shared tree at 75 against 73, because of
  other lanes' uncommitted registrations. It is not mine.
