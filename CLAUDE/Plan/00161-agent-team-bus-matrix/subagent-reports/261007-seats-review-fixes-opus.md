# Seats review fixes (Plan 00161)

The fixes for the qa-reviewer's BLOCK on `origin/F44...origin/seats-integration`
([review](261007-seats-qa-reviewer-opus.md)). Findings 1, 3, 4 and 5 are fixed here.
Finding 2 is not: when a ccy upgrade discards the Quick Launch choices, U20's
prerequisites fail on the first meta-deploy. The owner is deciding that behaviour, and it is
unchanged. Each fix started with a failing test.

## 1. Root no longer writes in a checkout (blocking; D61)

**What was wrong.** `seat take` ran `sudo agent-bus add-member … --out=<checkout>/…/seats/<team>/<seat>`.
The root wrapper then ran `install -d -o "$SUDO_UID"` and `install -o "$SUDO_UID"` along
that path. A sibling seat's session can rewrite the checkout, because container root is the
host user. It could swap a path component for a symlink between the user-side check (D58)
and root's write.

**Fix, checkout side** (`helpers/agent_bus/checkout.py`):

- `take_actions` now asks for `add-member … --out=-`.
- `claim_seat_dir` walks from the checkout one opened directory at a time:
  - Each directory is opened with `O_DIRECTORY|O_NOFOLLOW` and `fstat`-checked as owned by the user.
  - It makes `.claude/ccy/pingbus/`, `seats/` and `seats/<team>/` (mode 0700) where they are missing.
  - It then makes the seat's own directory fresh, mode 0700. If that directory already exists the take is refused (78), before any sudo.
- `SeatClaim.place` checks the tar it got back (`read_bundle`):
  - It must hold exactly `member.json`, `token` and `README`.
  - Each must be a regular file of at most 64 KiB.
  - Anything else is refused.
- It then writes each file through the open directory with `O_CREAT|O_EXCL|O_NOFOLLOW`, mode 0600, `member.json` last.
- If `add-member` fails, the empty directory is removed again.
- A bundle that cannot be placed exits 78 and names `agent-bus seat remove`.
- `System.agent_bus` now returns stdout as bytes.

**Fix, wrapper side** (`files/usr/local/bin/agent-bus`):

- `add-member --out=-` unpacks the tool's tar in root's private temporary directory. It re-packs only the three files (owner 0, mode 0600) as a tar on stdout and writes nothing else.
- `--out=<dir>` stays, because the README guidance, `rotate-token` and the acceptance harnesses use it. The directory and files are now written as the sudo user (`setpriv --reuid/--regid --clear-groups`, `install -d`, `dd`, `chmod`), never as root. A re-pointed path therefore leads only to places that user could write anyway.
- `agent-bus-install` now lists `util-linux` in its packages, for `setpriv`.

**Tests:**

- `tests/helpers/agent_bus/test_checkout.py`. The fake host now hands the bundle back as a tar on stdout. New cases:
  - the modes and owner of the placed files
  - a team directory swapped for a symlink while `add-member` runs: nothing is written through it
  - a symlinked component refused at the claim
  - malformed bundles (missing, extra, `../` and symlink members): refused, with no secret placed
  - a seat directory that appears between the observe step and the claim: refused before sudo
  - a failed `add-member`: the empty directory is removed again
- `tests/helpers/agent_bus/test_wrapper.py`. New cases:
  - `--out=-` prints exactly the three files, 0600, and writes nothing
  - extra or hostile tar members are dropped
  - an incomplete bundle prints nothing
  - `--out=-` is refused for `rotate-token`
  - as root, an `--out` in a directory only root can write is refused, whether reached directly or through a symlink. This proves the write is the user's.
  - the existing fixtures give the sudo user a home of their own

**Docs:**

- DESIGN.md §5.1, §5.5 (two launches racing for one seat), §5.6, and the U15 row.
- A new decision row, D61. D58's rationale is left as it was written; D61 replaces its "symlinked tree" defence.
- `docs/agent-bus.md`: steps 2 and 3, the seats section, and `rotate-token`.

No token reaches argv or a log. It travels only on the sudo pipe.

## 3. Every `PINGBUS_*` variable is refused from `ccy.env` and `ccy.env.local` (D62)

**The decision:** the code now matches the docs, rather than the docs being narrowed to the code.

- DESIGN §5.3 and D33 say the checkout carries nothing about the bus.
- No design path sets a bus variable from those files. A ccy session's forge credential is the launch's `GH_TOKEN` (`--token`), and a non-GitHub forge is not part of v1.

**Fix** (`entrypoint.sh`):

- PROJECT-ENV takes a `NAME=%q` snapshot of every `PINGBUS_*` variable (`${!PINGBUS_@}`).
- AGENT-BUS compares that snapshot after the files have run. Any variable that was set, changed or unset refuses the launch, and the error names each one.
- The check for a launcher older than the image (`PINGBUS_TEAMS` or `PINGBUS_HOME` arriving from the launcher) is unchanged.

**Tests** (`scripts/test-ccy-agent-bus.bash`):

- `PINGBUS_FORGE_TOKEN`, `PINGBUS_FORGE_TOKEN_FILE`, an arbitrary `PINGBUS_ANYTHING` (not exported) and an unset `PINGBUS_ENV`: each refused, with the variable named.
- A bus variable that the launcher passed and neither file touches: accepted.
- `MY_PINGBUS_X`: accepted.

**Docs:**

- `docs/ccy.md` and the 3.86.0 changelog entry now state the rule exactly.
- DESIGN §5.3's entrypoint bullet, and D62.

## 4. The headless Quick Launch banner goes to stderr

`claude-yolo` now wraps the whole Quick Launch block (`load_launch_config`, the banner, the
"Headless launch: using the previous configuration." line and the interactive prompt) in
`{ … } >&2`. Every line in the block is status or prompt text. The block's `read -rp`
already prompted on stderr. `scripts/test-ccy-teams.bash` now checks that a headless
replay of the block prints nothing on stdout, and prints the headless line on stderr.

## 5. The real `pingbus seat exec` command line is gated

`scripts/test-ccy-agent-bus.bash` builds the real zipapp again, with
`python3 -m helpers.pingbus.bundle` as the play does. The exec stub also records the step's
argv, NUL-separated. A new section replays that exact argv, from `seat` on, through the
zipapp's own `cli.main`. The parser, the real claim and the real `execvpe` all run.

Only the two paths fixed for a container are redirected into the test tree: `/workspace`'s
seats and `/tmp/pingbus-home`. The test cannot write to the real `/workspace`.

Stand-ins for the wrapper and for `claude` print what they receive. Cases:

- With a wrapper (`--arm --`), the wrapper gets its own `--`, then `claude` with the plugin
  arguments, `PINGBUS_HOME` set to the session home and `PINGBUS_TEAMS=team-a,team-b`.
- Without a wrapper, `claude` itself gets the plugin arguments.
- A seat with no directory gets the real claim's 78, which names the seat.

The real pingbus also refused two seats sharing one token, so each fixture seat has its own.

## Versions

- CCY 3.86.1 → **3.86.2**, with a `docs/ccy-changelog.md` entry.
- Container 2.47 → **2.48**: the Dockerfile `LABEL` and `REQUIRED_CONTAINER_VERSION`. `entrypoint.sh` is in the image.
- The CCY and container versions in `deploy.bash`'s comment are updated.
- `meta-deploy.bash` already lists 00161. Its `deploy.bash` reinstalls the wrapper and `agent-bus-install` (step 3) and rebuilds ccy (step 11).

## Targeted QA (all green)

- `python3 -m unittest`, run over:

  - every `tests/helpers/agent_bus/test_*` module
  - `tests.helpers.pingbus.test_seat`, `test_cli` and `test_bundle`
  - all 408 tests pass

- The shell suites:

  | Suite                             | Result     |
  | --------------------------------- | ---------- |
  | `test-agent-bus-install`          | 261 passed |
  | `test-ccy-agent-bus`              | 59 passed  |
  | `test-ccy-teams`                  | 74 passed  |
  | `test-ccy-project-env`            | 7 passed   |
  | `test-ccy-env-local-dist`         | 35 passed  |
  | `test-ccy-restore-askpass`        | 87 passed  |
  | `test-ccy-container-version-hook` | 13 passed  |

- `shellcheck -S warning` on every touched bash file: clean.

- ruff 0.16.8 (the pinned version) on the touched Python: clean.

- `qa-all.bash` was not run, as the task instructed.
