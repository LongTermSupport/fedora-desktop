# Plan 00192 Task 4.2: second qa-reviewer pass, fixes

The review is `untracked/agent-reports/auto/261028-044629-qa-reviewer-a5ed04a2bb3a28691.md`, of F44 at `e3d67048d`.
The fixes are on `plan/00192-ssh-with-key-merge`, which is F44 plus `abf7cd03c`. None of it is pushed.

| Finding                                                                                          | Fixed in                                        |
| ------------------------------------------------------------------------------------------------ | ----------------------------------------------- |
| 1. Plan 00131 names the deleted watermark and nord                                               | `858b6c604`                                     |
| 2. retention.py `prune-plan`, margin defined twice, two disk checks                              | `903d27702`                                     |
| 5. `NVIDIA RTX 500` in sections_test.go                                                          | `461aeac48`                                     |
| Nit: copyid.go "already installed" through failf                                                 | `86251bfa2`                                     |
| 6 and 7, nit `uk-london`: nord doc plaintext credentials, skip-and-warn firewalld task           | `c06543d2b`                                     |
| 3, 4, nits: Status header, stale merged/queued lines, success criterion 1, Delivery & Milestones | the bookkeeping commit that carries this report |

## What changed

1. **Plan 00131.** watermark is struck from Task 2.1. Task 2.2 (nord) is struck and ticked, because no shell is left to fix. The count was measured again against the tree with the widened pattern: 7 live sites in 4 files (clean-paste 41; rclone-cache-warm 71 and 103; commit-msg 45 and 93; pre-commit 320 and 338), where it used to say 18 in 8. Phase 2's heading and the success criteria now use the new count. rclone-cache-warm's second site has moved from line 99 to line 103.

2. **vmtest retention.**

   - Callers of `retention.py`: only vmtest, through its helper seam, plus the file's own tests. No play, task or other helper imports or runs it.
   - Moved to `src-go/internal/vmtest/retention.go`:
     - the RAM ceiling (75% of MemTotal, `Env.HostMiB`)
     - the rebuild floor (room for two bases and the margin)
     - the prune rule and the run-passed rule
     - the sweep of `runs/` (the newest 10 passing runs kept) and of `bases/*.build`
   - The 2 GiB margin now has one home. `TestDiskMarginIsRetentionsMargin` is deleted.
   - A run and a member now have one disk check, the guard (E64), and after it only the RAM check. A rebuild keeps its two-bases floor, because the guard's fixed 26 GiB cannot know the base's size.
   - `retention.py` keeps only `sweep-spool`, which bounds the bridge spool. It stays Python because spool.py's pinned descriptors are shared with the bridge helpers (bridge_watcher, bridge_run, request and others). A Go copy would duplicate that security walk.
   - The Python floor, prune-plan and lab sweep are deleted, with their tests.
   - Departures from the old behaviour:
     - A response.json that exists but cannot be read now stops the sweep and the prune. The Python read it as "did not pass".
     - A floor refusal now gives its figures in the error, not in VMTEST-DISK and VMTEST-RAM lines.
   - Docs updated: SPEC-vmtest E16, E33, E51, E56, E61 and E64, the intro, PLAN Task 3.10, src-go/CLAUDE.md and app.go's package doc.
   - Red and green are journalled (`JOURNAL/2026-10-28/045732Z-decision-b2daad.md`). Red was a build failure of the new tests, then 5 of 6 Python tests failing. Green: `go test` passes with 90.4% coverage, the Python suite passes 6 of 6, and go-qa passes in diff mode.

3. **Status header.**

   - Host agent: the queued `changed-plays` run and its checks, plus the existing list (ssh-with-key H2 and H4's guest part are already in it, after the prune).
   - Owner: vmtest H7 `--yes` and H6, nord H4-H7, nvidia-status H5, ssh-with-key H5 and setup-rclone H1-H4, added to the existing list.
   - The other Task 3.3 bullets were checked, and they hold nothing else.

4. **Stale lines.**

   - PORTS.md: reclaim is merged at `2e32edc2a`; reclaim 2.0.5's recheck passed at `071969780`; nord is merged at `075d2418f`; the nord play ran by name at `037892368`.
   - The ssh-with-key "Host run queued" line says the run happened at `e3d67048d`.
   - PLAN.md: 2.0.3's check ran, and 2.0.4 fixes what it found. The ssh-with-key bullet already said it was merged.

5. **Fixture.** The fixture now uses `NVIDIA GeForce RTX 3050 Laptop GPU/PCIe/SSE2`. Nothing in src-go names the other GPU.

6. **nord doc.**

   - Step 1 and the Security bullets now say the play saves the credentials vault-encrypted, through `ask-and-save-secret.yml`.
   - The firewall troubleshooting section is replaced. It said to start firewalld; it now says to run the play again after firewalld is started.
   - The rest was checked against nord's usage.txt (commands, names, files, the log) and agrees.

7. **nord play.**

   - The warning debug is removed. The probe's comment says why the firewall tasks are skipped when firewalld is not running.
   - The query-then-add tasks are unchanged, so the play stays idempotent.
   - The usage message now uses `uk1234` and `us5678`.

**Nits.**

- sshkey's `notef` is the status path to stderr, and `failf` now calls it. The output is unchanged.
- Success criterion 1 is ticked, on Task 1.3's controls.
- Delivery & Milestones lists the F44 merges.

## Host runs

`CLAUDE/Plan/meta-deploy.list` now holds one `changed-plays`. The entry for `e3d67048d` it replaces has run. The new run is expected to pick up:

- **play-vm-test-lab.yml**: it rebuilds vmtest and redeploys the helpers. The host agent then checks H5 (`vmtest nightly`, the sweep log, the lines ending `VMTEST-SPOOL-SWEEP-DONE`) and H7's dry run.
- **play-basic-configs.yml**: it rebuilds the ssh-with-key binaries, after the sshkey nit.
- **play-nordvpn-openvpn.yml**: a run that reports no change and no firewalld warning.

No ccy file changed, so there is no CCY bump.

## Not done

Nothing from the review is left open. qa-all.bash was not run; that is the coordinator's.
