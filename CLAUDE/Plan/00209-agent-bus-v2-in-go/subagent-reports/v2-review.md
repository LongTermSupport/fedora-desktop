## QA Review: `v2` against F44 (`git diff origin/F44...origin/v2`, 65 commits, merge-base f2a002378)

**Verdict**: FIX-BEFORE-MERGE

### Blocking

1. **blocker: a partly assembled kit is either skipped silently or can never be repaired.** `playbooks/imports/play-teamchat.yml:445-453` guards `kit assemble` with `creates: <kit>/settings.json`. `kit.Assemble` writes `bin/teamchat`, then `settings.json`, then the plugin tree (`src-go/internal/teamchat/member/kit/kit.go:103-116`), and it refuses a directory that is not empty (`kit.go:73`). That gives two bad outcomes:
   - If it fails after `settings.json` is written, the next run skips the assembly. The only check (`:456-469`) is the binary's checksum, so `current` is pointed at a kit with no plugin.
   - If it fails before that, every later run fails on a non-empty directory, and only a manual `rm` clears it, which breaks the IaC rule.

   **Fix:** assemble into a temporary sibling directory and rename it into place. Alternatively, guard on the last entry (`SKILL.md`) and remove an incomplete digest directory before assembling. Also assert that `plugin/teamchat/.claude-plugin/plugin.json` exists before the link moves.

### Should fix

2. **Step 10 ships an owner decision that is still open, and it reaches v1 users.** `CLAUDE/Plan/00263-machine-qa-queue/PLAN.md` (Task 1.4) says "D1 (N) and D3 (the lane guard) are still open". Its DESIGN.md §5 Phase C.3 says "only if D3 includes B". Despite that, commit e42b4762e cites "Plan 00263 owner decision D3". `ccRunV1` merges the deny file (`ccrun.go:111-115`), so every **v1** agent-bus `cc --teams` session also loses `go test`, `make`, `ansible-playbook` and the rest. **Fix:** record the owner's D3 answer, or hold step 10 back.

3. **The deny rules block host-only QA, and no doc says so.** `Bash(*scripts/qa-*.bash*)` also denies `qa-deployed-drift.bash`, a gate that only has an effect on the host (it skips in ccy, `CLAUDE/QA.md:301`). It also denies `qa-changed.bash`, which CLAUDE.md requires before each commit. `QA.md` says nothing about host seats. **Fix:** say in QA.md who runs these gates on the host.

   On the wrappers you asked about:
   - `CLAUDE/Plan/meta-deploy.bash`, plan `deploy.bash` scripts and `run.bash` are **not** matched by any rule.
   - `shellscripts/ansible-run.bash` does not exist in the repo (`git ls-tree` shows no `ansible-run`).

4. **A host seat's declared duty is ignored by the static deny.** `docs/agent-bus-cross-network.md` lets a host member be given `worker` duty. The deny file applies to every `cc --teams` session whatever its duty. **Fix:** document the limit beside `duties`.

5. **The CCY changelog does not tell one story.** In `docs/ccy-changelog.md`:
   - 3.124.1 has container 2.84.51 but sits above 3.123.5 with container 2.84.52, so the container number goes backwards.
   - The 3.123.2 entry calls 3.123.1 / 2.84.46 an earlier unreleased step of the questions work, yet 3.123.1 / 2.84.46 has its own heading for `kit assemble`, which is host-cc work. Two branches used the same numbers.
   - 3.124.1 does not mention the host-lane deny rules (a change users will see, on v1 too) or the `cc-claude -- claude` form.

   **Fix:** fold the v2 steps into one coherent entry (3.124.6 / 2.84.57), or renumber the old ones. The final values do agree: Dockerfile LABEL and `REQUIRED_CONTAINER_VERSION` are both 2.84.57.

6. **The meta-deploy queue entries are out of date.** `CLAUDE/Plan/meta-deploy.list`:
   - The text still says "ccy 3.124.1, container 2.84.51" and "the deployed ccy is 3.124.1". The branch is at 3.124.6 / 2.84.57; the script itself reads `CCY_VERSION` (`acceptance-host-cc-v2.bash:91`).
   - The text for the play-teamchat and play-claude-yolo entries never mentions seat duty or the R0 verbs, which those runs deploy.
   - The `acceptance-host-cc-v2.bash` entry takes no arguments. Run by an agent with no terminal, it exits 2 straight away (`:62-66`), so the queued run adds no evidence.

   **Fix:** update the text, and take the acceptance run out of the batch (or feed it its team and checkout some other way).

7. **The design's agent verb table is out of date.** `DESIGN-v2-B-member.md:377-381` still lists `ask --asked-by`, `thread --ask` and `verify <event-id>`. The code has `ask-human`, `--for`, `close`, `retitle` and `bump` (`member/cli/verbs.go:150,256-258`). DESIGN-questions §8 amends only areas A and C. **Fix:** amend B §3.

8. **Plan status is out of date.** `PLAN.md` Task 4.6 still says "Being built test first on its own branch", but it was merged in fe67f23c3 and has its build report. **Fix:** mark it built.

### Nits

9. `Bash(*run.bash qa*)` is a dead rule: `run.bash` has no `qa` verb (no match for `-w qa`). `go run` is not covered either.
10. `cc-claude` now refuses a first argument of `--` unless `claude` follows it (`ccclaude/app.go` `Run`). So plain `cc -- <prompt>` exits 64, where before it went to claude.
11. `scripts/test-cc-teams.bash` now greps `ccrun.go` source text for `--plugin-dir` and the prompts instead of running anything. The Go parity test does cover the behaviour. The QA.md:200 row still says the seats are taken "through `agent-bus`".
12. Plans 00262 and 00263, which are not part of v2, ride on this branch. They are in separate commits, so this is only a note.

### Checked and clean

- **Conversation-record DBF rule:** the analyzer, its fixtures (red and clean forms) and the GoProjectRules row are coherent.
- **cc-run on v1:** the parity oracle runs the old bash against Go. The settings file is written 0600 and that is tested (`ccrun_parity_test.go:135`). `internal seat-check` and `seat-take` already exist on F44.
- **image-source-paths:** gains `duty` and `whoami`. The kit skill and the image's SKILL.md diffs match.
- **Public-repo safety:** no home paths, hostnames, IPs or install names in the added lines. `LongTermSupport/agentic-qa-queue` does not exist yet (404), so it is not a private repo name. "Owner: joseph" follows the existing mkplan convention.
- **Play order:** play-teamchat before play-claude-yolo. cc, cc-teams.bash and ccy-teams all ship in the same play. `bash-limits.allowlist` went down (87 to 61 lines).
- **Merge:** no file is touched by both F44's 8 newer commits and v2.

### Mechanical gates

- `qa-all.bash`, `plan-qa --sweep` and `--syntax-check` were **not run**. You asked for reading only, and running them on v2 would need a checkout, which is a mutation. Before the move, run `--syntax-check` on `play-teamchat.yml` and `plan-qa --sweep` on a v2 checkout.