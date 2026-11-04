# QA Review: CCY 3.68.0 relabel preflight (760f27c5, 52760692)

**Verdict**: BLOCK

The main problem is that CI on F44 is red right now. The new test only passes when run as root.
There are also two ways the preflight now stops ccy starting in projects that 3.67.x started fine.

## Blocking

1. **The new gate fails for every non-root user, and CI on F44 is already red.**
   `scripts/test-ccy-relabel-preflight.bash:157,162,194,199,213`
   - The fixtures `chown -R 4242:4242` / `150:150`. That only works as root.
   - CI run 36133416370 (head 52760692, branch F44) failed with `✗ ccy-relabel-preflight` and
     `chown: … Operation not permitted`. The runner is uid 1001.
   - Reproduced with `setpriv --reuid=1000`: 19 passed, 17 failed.
   - It passes in the container only because the container is uid 0. `qa-all.bash` on the
     owner's host will fail the same way.
   - Two cases pass without testing anything once the chown fails, because they then run with no
     foreign entries at all: "no relabel (SELinux off) … even with foreign entries" (:159) and
     "subordinate uid … no question" (:196).
   - Fix: stop relying on real chown to make an entry foreign. For example, give the stub a uid
     map that leaves out `$ME`, and have the stub `sudo` switch the map before the re-scan. Also
     assert each fixture's owner with `stat` before the case runs. Skipping the test when not
     root is not an option (fail-fast rule).

2. **A directory the user cannot read now stops the launch, even when podman could relabel it.**
   `files/var/local/claude-yolo/lib/common.bash:148-154`
   - Probe: a project with a 0700 directory owned by a mapped subordinate uid, which is what a
     rootless container creates (e.g. a database data dir).
   - Run as uid 1000: `find: '…/db-data': Permission denied`, then
     `ccy could not look through … (find exited 1)`, `rc=1`.
   - podman can relabel that directory: it owns the uid inside its user namespace. So this is a
     new refusal in a project that 3.65–3.67 started. The message gives no remedy.
   - The same partial walk is also treated as complete in two places:
     - When any foreign entry is found, `rc` is ignored (:148/:150). The "cannot relabel N
       entries" count is then based on an incomplete walk.
     - The re-scan after the fix ignores `rc` completely (:225). So "now yours" (:231) can be
       printed after a walk that failed.
   - Fix: walk the tree the way podman sees it, i.e. run the find under `podman unshare`,
     translating to inside uids. At minimum, treat EACCES on a directory owned by a mapped uid as
     "not blocking", and check `rc` on the re-scan.

## Should fix

3. **Any stderr output from podman is treated as an unreadable uid map, and the launch stops.**
   `lib/common.bash:136`
   - `$(container_cmd unshare cat /proc/self/uid_map 2>&1)` sends stderr into the text that gets
     parsed.
   - Probe: one podman-style `level=warning` line added to the stub's stderr gave
     `ccy cannot read the uid map…`, `rc=1`, with a valid map printed underneath.
   - Fix: parse stdout only, and capture stderr separately so it can be shown if the parse fails.

4. **The docker skip rests on a premise that does not hold.** `lib/common.bash:132-133`
   - The comment says "A rootful engine relabels as root". But the launcher refuses any engine
     that is not rootless before this point (`claude-yolo:1415`, `engine_assert_rootless`).
   - So the only docker that ever reaches line 133 is rootless docker. Rootless docker also
     relabels from inside a user namespace, so it likely hits the same EPERM (not measured).
   - Either measure rootless docker and cover it, or rewrite the comment to state the real reason
     for skipping it. The docker branch has no test case.

5. **`--ssh-agent` sessions are not considered anywhere.**
   - The launch adds `--security-opt label=disable` (`lib/ssh-handling.bash:813`). The preflight
     only looks at `CCY_MOUNT_RELABEL` (:131), and `SSH_AGENT_FORWARDED` is already set by
     launcher line 1050.
   - Neither the code, the test, nor the journal says whether podman still does the `:z` relabel
     when labelling is disabled. I could not measure it here (no podman in the container).
   - If podman skips the relabel under label=disable, this preflight stops `--ssh-agent` launches
     that would have worked. Measure on the host, then either skip when `SSH_AGENT_FORWARDED=1`
     or record why it must not skip.

## Nits

- `common.bash:176` and `:231` print `${#foreign[@]}` (every foreign entry) rather than
  `blocking`. When some foreign entries already carry the label, the count is too high.
- `common.bash:159-164` forks a `stat` for every foreign entry, but only uses `blocking > 0`. It
  can stop at the first one.
- `common.bash:217`: running out of the 3 tries prints the same message as answering "n".
  InteractiveScripts rule 2 asks for a distinct give-up message.
- `common.bash:176,231`: the colour codes are not guarded by a `[ -t 2 ]` check (rule 13). This
  matches existing ccy code.
- Journal 12:02 ends with a sentence fragment: "That ccy refuses to sign until D5's plays have
  run."

## Checked and clean

- **Find predicate** (`common-pure.bash:179-201`): the bounds `+(o-1)` / `-(o+c)`, the `o=0`
  case and the `c=1` case are all correct. The parse rejects an empty map and error text instead
  of reading them as "everyone is foreign".
- **Ownership premise, checked empirically**: under `unshare -r`, a foreign uid gets EPERM on
  `security.selinux`, while a foreign gid alone succeeds. So filtering on uid only is correct.
- **Label logic**: the block condition (foreign, and either the entry or the top lacks the
  label) covers every case that either reading of go-selinux's fast-mode skip would relabel. So
  it can wrongly stop a launch but never wrongly let one through. The journal states the skip is
  unmeasured.
- **Stderr hygiene**: every message goes to stderr, `read -p` prompts on stderr, and the `find`
  captures are clean.
- **Prompt loop**: bounded at 3, EOF means no, default is N, and no TTY stops with the fix
  command printed. The prompt is registered in `ccy_known_prompts`, which is only used to detect
  a waiting session.
- **Version bump**: `CCY_VERSION` is 3.68.0 and the `common.bash` header moved to 1.6.0. No image
  files changed, so no container-version bump is needed.
- **Docs and plan**: the changelog, the `docs/ccy.md` paragraph, the QA.md row and the qa-all
  gate (which prints a pass line) are all present. Task 3.1 is still 🔄 with HOST pending. The
  meta-deploy entry points at an existing play. The journal's "36 cases" matches.
- **Public repo**: the journal is anonymised ("project A", `<timestamp>`), and the test and docs
  use only generic uids. Nothing install-specific found.

## Mechanical gates

- **qa-all.bash**: passes in the container as root (`✓ ccy-relabel-preflight: passed: 36`).
  Fails as non-root, and fails in CI run 36133416370.
- **plan-qa --sweep**: 0 blocking, 12 advisories, none in files this diff touches.
- **syntax-check**: no playbook changed, so not triggered. `qa-helper-tests`, the extension
  checks and ESLint are also not triggered.

Probe files are in `<repo>/untracked/scratch/relabel-review/`, including `nonroot.log` and
`ci-failed.log`.
