## QA Review: `origin/F44...origin/seats-integration` (Plan 00161 U29–U33, reworked U20)

> Committed by the fixing agent, since the read-only reviewer could not. The text is the
> reviewer's, with one worktree path replaced by a placeholder. The fixes are in
> [261007-seats-review-fixes-opus.md](261007-seats-review-fixes-opus.md).

**Verdict**: BLOCK

I wrote no report file, made no `seats/review` branch, and committed or pushed nothing. My reviewer role forbids any mutation of the repo or git state, and the task text came from a workflow script, which cannot override that. The findings below are the full report; the caller has to commit them if it wants them on a branch.

### Blocking

1. **Root writes inside a checkout that live container sessions can rewrite. This is a time-of-check/time-of-use (TOCTOU) path an agent could use to reach host root.**
   - Locations: `helpers/agent_bus/checkout.py:192`, `:403-409`; `files/usr/local/bin/agent-bus:89-90`, `:105`, `:109`.
   - `seat take` runs `sudo /usr/local/bin/agent-bus add-member … --out=<checkout>/.claude/ccy/pingbus/seats/<team>/<seat>`.
   - As root, the wrapper checks `! -e $out` and then runs `install -d -o $SUDO_UID $out`, then `install -o $SUDO_UID … "$out/$file"` for `member.json`, `token` and `README`.
   - Every parent of that path is in `/workspace`. That is bind-mounted read/write (`docs/ccy.md:393`), and ccy runs rootless with no `--userns` flag (no match in `claude-yolo`), so container root is the host user.
   - A sibling seat's session in the same checkout is exactly what this feature exists to allow. Such a session can swap `seats/<team>` or `$out` for a symlink between the checks and root's writes.
   - D58 ("A symlinked tree would steer root's install -d outside the checkout") only adds a pre-check as the user, so the race remains.
   - The result is a user-owned directory, or user-owned files with fixed names, created by root wherever the symlink points. That crosses DESIGN §0's boundary ("secrets belong to a system user no agent runs as").
   - It also contradicts the wrapper's own refusal text: "root never writes in a user's checkout" (`agent-bus:117`).
   - Fix: root must not touch the checkout path. Have `add-member` hand the bundle tar to the caller. `checkout.take`, which already runs as the user, then unpacks it into the seat directory. The other option is to place the files as `$SUDO_UID` with `setpriv`/`runuser`, never as root.

### Should fix

2. **A `meta-deploy.bash` run fails at acceptance every time it installs a new ccy version.**

   - Locations: `CLAUDE/Plan/00161-agent-team-bus-matrix/PLAN.md:198`, `_acceptance-u20.inc.bash:214-216`, `deploy.bash:144-145`.
   - `deploy.bash` upgrades ccy to 3.86.1. `load_launch_config` (`claude-yolo:503-515`) deletes any `.last-launch.conf` saved under another version. The U20 prerequisites then fail with "OWNER: launch ccy 3.86.1 interactively".
   - meta-deploy runs deploy and then acceptance with no pause between them. So the PLAN's "the owner launches ccy here interactively once between the ccy upgrade and the acceptance" cannot happen inside one run.
   - As handed over, the result is a guaranteed red run, an interactive launch, and a second full redeploy.
   - Fix: either have the harness pass `--token`/`--ssh-key` explicitly (it already reads the token name from the record), or split the run and say so in the PLAN.

3. **The docs claim every `PINGBUS_*` variable is refused, but only three are.**

   - `docs/ccy.md:929` says "the container refuses to start when either sets a `PINGBUS_*` variable".
   - `docs/ccy-changelog.md:42` says "A `PINGBUS_*` variable set, changed or unset … refuses the launch".
   - `entrypoint.sh:422` and `:540` check only `PINGBUS_SEATS`, `PINGBUS_TEAMS` and `PINGBUS_HOME`. `pingbus` also reads `PINGBUS_FORGE_TOKEN` and `PINGBUS_FORGE_TOKEN_FILE`, which `ccy.env` can still set.
   - Fix: name the three variables in both docs.

### Nits

4. **A flagless headless launch prints the Quick Launch banner and "Headless launch: using the previous configuration." on stdout** (`claude-yolo:1133-1172`, new line `1162`).
   - A headless session's stdout is its payload; `u20_check.py:188` has to skip lines that are not JSON.
   - Fix: send these lines to stderr when `HEADLESS_MODE` is set.
5. **`scripts/test-ccy-agent-bus.bash:51-53` now uses a stand-in zipapp; it used to run the real one.**
   - No gate now runs the real `pingbus seat exec -- <wrapper> -- claude …` command line from the entrypoint.
   - I checked by hand that Python 3.11's argparse keeps the full remainder, including both `--`.
   - Until U20 runs on the host, nothing re-checks this automatically.

### Checked and clean

- **Version bumps.** The two commits that touch ccy files (U31, U20) each bump the version: CCY 3.85.2 → 3.86.0 → 3.86.1, and LABEL and `REQUIRED_CONTAINER_VERSION` both go 2.45 → 2.46 → 2.47. `agent-bus-seats` is in `CCY_LIBS` and installed by `play-claude-yolo.yml:461`. The changelog has entries for both versions.
- **IaC placement.** No new play. The lib is installed by the play that owns ccy. `deploy.bash` leg 11 was updated. `meta-deploy` lists 00161.
- **Design match.** Checked against D33–D60:
  - `--teams` parsing, the canonical list and the 64/75/78 exit codes
  - restart and restore replay
  - the entrypoint's refusal of bus variables set in `ccy.env`
  - the claim: lock per seat, release on failure, home made with mode 0700
  - registry v2: parked handles, counter raised by a numbered `--seat`, v1 loading
  - `<host>` read from `ccy.env.local` without sourcing it, else `local`
  - `seat remove` holds the lock and prunes empty directories
  - the `history` scan states when it was cut short (`complete` flag plus a stderr line)
- **Restart exit code.** Exit 75 from `seat exec` cannot be mistaken for a restart: with no request file it is passed straight through (`claude-yolo:3512-3514`).
- **Public-repo safety.** Grepped the added lines for home paths, emails, private IPs and install-specific names. Only example values and test fixtures turned up.
- **Fail-fast.** No `|| true` or swallowed errors in the new bash; the only `2>/dev/null` uses are `kill -0` liveness probes.
- **Plan state.** PLAN.md marks U29–U33 built and U20 as host run pending, which is accurate. There is no new plan folder.

### Mechanical gates

All runs used an untracked snapshot of `origin/seats-integration` (via `git archive`), because this worktree sits at F44.

- **`qa-all.bash`**: could not give a meaningful result on the snapshot.
  - `ansible-syntax` failed on all 82 playbooks: `vault-pass.secret` is missing.
  - `js` failed: there is no `node_modules`.
  - Both failures come from the snapshot setup, not the diff. bash (399 files), python (280 files), patterns and ansible-fail-fast all passed.
- **Targeted gates run instead**: all passed.
  - `test-ccy-teams.bash`: 73 passed, 0 failed
  - `test-ccy-agent-bus.bash`: 45 passed, 0 failed
  - `test-ccy-env-local-dist.bash`: 35 passed, 0 failed
  - `test-ccy-restore-askpass.bash`: 87 passed, 0 failed
  - `test_u20_check`: 66 tests OK
- **`qa-helper-tests.bash`** (required, since `helpers/` changed): 3642 tests, 1 failure. The failure is `test_vaulting_plays`, which needs `git ls-files`, and the snapshot is not a git repo.
- **`plan-qa --sweep`**: 0 blocking findings, 11 advisories, none about 00161.
- **Syntax check**: `ansible-playbook --syntax-check playbooks/imports/play-claude-yolo.yml` passed.
- **Extensions**: no extension files changed, so neither extension gate was needed.
- **Merge**: the branch merges cleanly into the current `origin/F44` (`git merge-tree` reports no conflicts).

Scratch artefacts are in `<reviewer worktree>/untracked/scratch/`: the snapshot `qa-seats-snap/` and the logs `qa-all.out` and `helper-tests.out`. They are untracked and gitignored.
