# Task 7.5: Quick Launch kept across ccy versions (CCY 3.86.3)

Owner decision (2026-10-07): option A, keep the saved Quick Launch settings across ccy
versions when their shape has not changed. Built on `seats-integration` (which carries
CCY 3.86.2 and Plan 00161's seats work).

## The format version already existed

The brief asked for an explicit format version for `.claude/ccy/.last-launch.conf`. One was
already there: `CONFIG_VERSION=1` in the launcher, written into every saved file as
`SAVED_CONFIG_VERSION=1` and checked by `load_launch_config` before the version check.
`git log -S` on each of `LAST_TOKEN=`, `LAST_SSH_KEYS=` and `LAST_NETWORK=` shows them all
introduced in the same commit as the file itself (`2a9d337e`) and unchanged since. So format
1 has meant the same keys all along, and a second format line (for example
`LAST_LAUNCH_FORMAT`) would have duplicated it. Nothing new was added to the file. The
launcher comment on `CONFIG_VERSION` now says it is the file's format, bumped only when the
keys or their meaning change, and that a file of this format is kept across `CCY_VERSION`
changes.

## The rule now (`load_launch_config`, `files/var/local/claude-yolo/claude-yolo`)

1. The `SAVED_*` and `LAST_*` variables are unset, then the file is sourced as before. A file
   that fails to source is still discarded as corrupted.
2. If `SAVED_CONFIG_VERSION` is not `CONFIG_VERSION`, or is missing, the file is discarded
   with the existing banner, reworded: "Saved launch configuration is in another format",
   then the saved format (or `none`) and the format this ccy reads.
3. New: if `LAST_TOKEN`, `LAST_SSH_KEYS` or `LAST_NETWORK` is not set (empty is allowed,
   because empty is a choice), the file is discarded and the missing key is named. Unsetting
   in step 1 means the environment cannot supply a missing key.
4. Same `CCY_VERSION` with a different `CCY_HASH` (a change shipped without a version bump)
   still prints the DEVELOPER ERROR banner but no longer discards. The format decides
   whether the file can be read, and an unbumped change says nothing about that.
5. Otherwise the file is kept, whichever ccy version wrote it. The "CCY version changed"
   discard is gone.

Every check on the saved values themselves is unchanged. They are handed to the same
variables the flags set (`SPECIFIED_TOKEN`, `SSH_KEYS`, `SPECIFIED_NETWORK`), so the token
lookup ("No token found with name"), `build_ssh_mounts_and_validate`, the headless
passphrase-key refusal, and the network existence check with its "Saved network no longer
exists" path all still apply. The headless rules from Plan 00161 D57 are unchanged: an empty
key or network means none, and no token is refused.

## Files an older ccy wrote

They are **migrated as they are, with no step needed.** Every file any ccy has written
carries `SAVED_CONFIG_VERSION=1` and the three choice keys, so it passes steps 2 and 3 and is
kept. The next launch rewrites it through `save_launch_config` with 3.86.3's version and
hash. The first reboot or headless launch after the 3.86.3 deploy therefore takes the
choices an earlier ccy saved. A file with no format line was not written by ccy and is
discarded. A format-1 file missing a choice key cannot be read, so it is discarded too.

## Other code that reads the file, checked so all of them agree

- `lib/network-management.bash` (`ccy --disconnect` clearing `LAST_NETWORK`) reads and
  rewrites only the `LAST_NETWORK` line and does not depend on the version. Unchanged.
- `lib/common.bash` only mentions the file in the gitignore checks. Unchanged.
- The session registry and restore code replay through the Quick Launch block and never
  read the file. The comment beside the SSH key recording (`ccy_registry_record_ssh_key`)
  said the settings do not survive a version change; it now gives the new rule.
- The headless refusal message said "no saved Quick Launch choices for this ccy version".
  It now says "no saved Quick Launch choices this ccy can read".
- Plan 00161 U20 prerequisite: `u20_check.py launch-keys` used to take CHECKOUT,
  CCY_VERSION and CONFIG_VERSION, and refused a record from another ccy version. It now
  takes CHECKOUT and CONFIG_VERSION only, and refuses on the same terms as the launcher:
  another format, no format, or a missing choice key. `_acceptance-u20.inc.bash` was updated
  to match. Its owner-needs message still names the installed ccy version.
- `u01_probe.py` (and through it `ccy-token`) parses only `LAST_TOKEN`. Unchanged.
- `files/var/local/claude-code/cc` writes a different file (`~/.claude/.last-launch.conf`,
  for the status line). Not affected.

## Tests (TDD: red before the change, green after)

`scripts/test-ccy-teams.bash` has a new section, "Quick Launch across ccy versions". Before
the change 6 of its cases failed; after it, all 87 checks pass. The cases:

- A file from another ccy version in the same format is taken headless, with the same
  token, key and network as before. The file is kept and no discard is announced.
  Interactive launches still offer it (the prompt is asked).
- Same version with another hash: a warning naming the developer error, and the choices are
  kept.
- Format 2: discarded, the file removed, the headless launch refused, the message names the
  format.
- No format line: discarded.
- Format 1 with `LAST_NETWORK` missing: discarded, and the message names the key.
- Format 1 with `LAST_TOKEN` missing and `LAST_TOKEN=leaked` in the environment: discarded,
  so the environment value is not used.

`test_u20_check.py LaunchChoicesTest` was rewritten for the new rule and fails against the
old signature. It covers: another ccy version taken; another format or no format refused;
each missing choice key refused; the CLI. All 68 tests in the module pass.

Targeted QA (all green): `test-ccy-teams` (87), `test-ccy-network-disconnect` (138),
`test-ccy-restart-request` (159), `test-ccy-session-registry` (157),
`test-ccy-sessions-reboot` (200), `test-ccy-info-flags` (43),
`test-ccy-container-version-hook` (13). `ruff check` on the two Python files is clean.
Shellcheck on the touched bash files, invoked as `qa-bash.bash` invokes it, reports only
info-level findings that were already there (SC1091 on the library sources, SC2086 at the
`$NETWORK_FLAG` expansion). Following the sources (`-x -P SCRIPTDIR`) also shows SC2178 and
SC2128 on `COMPOSE_FILES`, in code this change does not touch. `qa-all.bash` was not run, as
instructed.

## Version and docs

- `CCY_VERSION` is 3.86.3; the container stays at 2.48 (no image change). There is a
  `docs/ccy-changelog.md` entry.
- `docs/ccy.md`, in the paragraph on how a restore uses Quick Launch: the settings survive
  an upgrade and are discarded only on a format change or a missing key.
- Plan 00135 PLAN.md: Task 7.5 is done, with the decision recorded. Task 7.4's expected
  result (`WAITING-AT-PROMPT` on the first reboot) is replaced: with 3.86.3 such a session
  is restored without a prompt. Task 7.2's closing sentence is updated.
- Plan 00161: D57 and D59 in DESIGN.md state the new rule. The PLAN.md note that left this
  to the owner now records the settlement. There is a journal entry (action).
- Plan 00135 journal entry (decision).

## Host run

Nothing new for meta-deploy: 3.86.3 reaches the host through the same
`play-claude-yolo.yml` run that Plan 00161's `deploy.bash` already does, once
`seats-integration` is what the host deploys.
