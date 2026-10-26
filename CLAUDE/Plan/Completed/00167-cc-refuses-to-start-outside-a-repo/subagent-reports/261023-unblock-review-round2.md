# Review round 2: plans 00183, 00177, 00167 (commits 1210f607e, 2798b0041)

Reviewer: adversarial, read-only. Diff: `git diff F44...HEAD` (two commits). The second commit
answers round 1 (`261023-unblock-review-round1.md`).

**Verdict: MERGE.** Every round 1 FIX is answered, except FIX 1, which is the known constraint:
the coordinator queues the acceptance in `meta-deploy.list`, and Task 3.3 says so. The checker
is green against a real build, and red where it should be. No new BLOCK or FIX. The NITs below
are worth folding in when the plans are next touched.

## Round 1 findings, checked

| Round 1                     | Status                       | Evidence                                                                                                                                                                                                                                                        |
| --------------------------- | ---------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| FIX 1 meta-deploy.list      | Known constraint             | Task 3.3 item 1 names `00167-cc-refuses-to-start-outside-a-repo/acceptance.bash` for the coordinator to queue.                                                                                                                                                  |
| FIX 2 restore needs a tty   | Fixed, reproduced            | `restore` now runs `CCY_SESSION_RESTORE=1 on_pty 'y\n'`. Run with stdin `</dev/null` against a build: exit 0, offers 0, refusal.                                                                                                                                |
| FIX 3 cc recipe tmux server | Fixed                        | It uses `env -u TMUX cc` in a pane of the agent's own server and `tmux -L ccy list-sessions`. With `TMUX` unset, `ccy_tmux_insulate` (tmux-session.bash:336-427) goes on to the offer and to start_detached and attach on `-L ccy`. The session offer is named. |
| FIX 4 00177 owner/agent     | Fixed                        | Tasks 6.4, 7.5 and 9.5 name the host agent (9.5 with the owner present, for sudo). A `correction` entry (a valid category, PlanJournalling.md:91) retracts "which the host agent is running".                                                                   |
| FIX 5 offer-yes checkout    | Fixed                        | See "offer-yes against root_dir" below.                                                                                                                                                                                                                         |
| NIT env/ssh in 00183 2.6    | Fixed                        | The shell is the new server's command, under `env -u SSH_AUTH_SOCK -u SSH_CONNECTION`.                                                                                                                                                                          |
| NIT capture-pane -e         | Fixed                        |                                                                                                                                                                                                                                                                 |
| NIT 2.6 ordering, 2.3 trim  | Fixed                        |                                                                                                                                                                                                                                                                 |
| NIT on_pty                  | Fixed                        | It checks `command -v script`, sets `SHELL=/bin/bash`, and reports 124 separately from other failures.                                                                                                                                                          |
| NIT SC2 tick (00183)        | Left as is; still defensible |                                                                                                                                                                                                                                                                 |
| NIT fakes not kept          | Still the case               | The "seven fakes red" claim cannot be re-run; I reproduced the restore one myself (below).                                                                                                                                                                      |

## Reproduced

The build was `go build -ldflags "-X main.RootDir=<this worktree>" ./cmd/cc-project-dir`. I ran
each leg with `CHECK_CC_PROJECT_DIR_BIN` set and stdin `</dev/null`. All six legs pass:

- `no-stdin`, `restore`, `git-top`: pass.
- `offer-yes`: offers 1, and stdout is this worktree, which equals the walk's `checkout`.
- `offer-no`: offers 1, exit 1.
- `offer-retry`: offers 2, with "Please answer y or n.".

**`CCY_SESSION_RESTORE` as a prefix to a shell function.** I ran the restore leg against a fake
that prints what it sees. It printed `flag=1 tty=yes`, so bash exports the prefix assignment to
the function's children, and both `timeout` and `script` pass it on to the binary.

**False green, restore.** A fake that offers whenever stdin is a terminal, ignoring the flag,
takes the typed `y` and exits 0 with `/x`. The leg FAILs on it, so the leg is not vacuous.

**Where restore alone falls short.** Run on its own, the restore leg cannot tell "the flag
stopped the offer" from "the binary did not see a terminal". The offer legs in the same
acceptance run close that gap. Not a finding.

**offer-yes against root_dir.** The play sets
`root_dir = lookup('config','CONFIG_FILE') | dirname` (play-claude-yolo.yml:14) and bakes it in
with `-X main.RootDir={{ root_dir }}` (:727).

- The checker walks up from its own folder to the first `ansible.cfg`. The repository tracks
  exactly one `ansible.cfg`, at its root, so the walk lands on the root that meta-deploy runs
  the plays from.
- Both sides are `pwd -P`-resolved, so a symlinked path does not break the comparison.
- A binary from another clone fails, with a message naming the re-run. A host whose last
  `play-claude-yolo.yml` ran from a different checkout fails loudly, which is correct.

**"cc-project-dir's code is unchanged since dddf4063".** True:
`git log dddf4063..F44 -- src-go/cmd/cc-project-dir src-go/internal/ccproject files/var/local/claude-code/cc`
lists nothing.

**Fail-fast and quoting.** The checker and acceptance.bash keep `set -euo pipefail`. Every
captured status is checked. The paths inside the `script -c` string go through `%q`. Nothing is
installed in the container, and nothing is papered over.

## Findings

No BLOCK. No FIX.

### NIT 1: 00167 Task 3.3 item 2, the token chooser "with its default"

The host-mode chooser has no Enter default. An empty answer is "Invalid selection: (empty)"
(token-management.bash:1703-1708), and after `max_tries` cc exits 1 (:1686-1688).

The menu does label `d` "Desktop (... current cc default)" (:1655), so a careful reader lands
on `d`. Still, say `d` (or a named token's number) so the host agent does not press Enter.

### NIT 2: 00167 Task 3.3 item 2, when to list the sessions

The recipe says "before and after each run, list `tmux -L ccy list-sessions`". After `y` and
`/exit`, claude returns, the session ends and the trampoline removes its record. So the new
`cc-<project>` session must be seen while it runs (before `/exit`), not after.

### NIT 3: 00167 text that still describes the old legs

- Task 3.2's first sentence still says the restore leg runs "with `CCY_SESSION_RESTORE=1` and
  stdin the terminal" (the old leg), next to the new pseudo-terminal description.
- Success Criterion 4 and the 08:27 journal entry say "a subfolder and a deeper folder give the
  repository's top". The `git-top` leg runs only from `sub/deeper`, both in F44 and here. Say
  "a nested subfolder".

### NIT 4: 00183 Task 2.6, the "unreachable ssh URL"

git-prompt-fetch adds `-o BatchMode=yes` but no `ConnectTimeout`. An address that drops packets
keeps ssh in TCP connect for minutes. At the 10 s look, `~/.cache/git-prompt-fetch.log` would
then not yet name a failed fetch, and the agent would report a false red.

Name a URL that fails at once, for example a host under `.invalid`
(`ssh://git@origin.invalid/x.git`, fails at DNS).

### NIT 5: the seven fakes are not kept (carried over)

The red proof the journal cites cannot be re-run from the repository.

## Public-repo check

No install-specific names appear in the added lines. The paths are generic (`/var/local/...`,
`~/.cache/...`, placeholders such as `<own>` and `<clone>`).
