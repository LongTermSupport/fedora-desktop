# Removing `plan_gate_change` from every remaining caller

`plan_gate_change` is gone from every script in the tree.

## Scripts changed (13 files, all `bash -n` + `shellcheck -x` clean)

Gate call deleted in each; description string disposition per file:

| File                                                                                                | Gate description                                                                                                                                                                                                                                             |
| --------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| `<repo>/CLAUDE/Plan/032-compression-helpers/deploy.bash`                                        | **Folded in** — the header literally said "the change gate below names it", so I replaced that deferral with the ouch binary path, the two 0755 wrappers, and the "no archive is created or modified" line. The ncompress refusal was already in the header. |
| `<repo>/CLAUDE/Plan/00079-podman-container-control/deploy.bash`                                 | Already covered by the `echo` block printed immediately above the gate.                                                                                                                                                                                      |
| `<repo>/CLAUDE/Plan/00092-ccy-child-claude-spawn-mode/deploy.bash`                              | Already covered by the header's `WHAT IT CHANGES`.                                                                                                                                                                                                           |
| `<repo>/CLAUDE/Plan/00099-rclone-rc-auth-broke-unmigrated-clients/deploy.bash`                  | **Folded in** — added a `WHAT IT CHANGES ON THE HOST` paragraph (credential file, `rclone-rc-auth.bash`, the three migrated helpers, ftp-camera); the mount rewrite/restart was already there.                                                               |
| `<repo>/CLAUDE/Plan/00104-suspend-aborts-on-dock-unplug-never-resuspends/deploy.bash`           | Already covered — the header's five-path `WHAT IT CHANGES` list is a superset.                                                                                                                                                                               |
| `<repo>/CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/deploy.bash`         | Already covered by the four-leg `THE LEGS, IN ORDER` block, MOK/reboot warning included.                                                                                                                                                                     |
| `<repo>/CLAUDE/Plan/00112-gnome-extensions-enabled-state-declared/deploy.bash`                  | Already covered by `EFFECT ON THE HOST`.                                                                                                                                                                                                                     |
| `<repo>/CLAUDE/Plan/00124-chrome-install-gpg-failure-on-upgraded-host/deploy.bash`              | **Folded in** — header also deferred to the gate; added gnupg2, the key file, `/etc/default/google-chrome`, the repo file, and the three browser packages. The keyring erase/import was already there.                                                       |
| `<repo>/CLAUDE/Plan/Completed/00102-dash-to-dock-does-not-dodge-ptyxis-terminal/deploy.bash`    | Already covered; named the key (`intellihide-mode -> ALL_WINDOWS`) in the header while removing "Gated before anything mutates (R8)".                                                                                                                        |
| `<repo>/CLAUDE/Plan/Completed/00103-slack-flatpak-rejects-md-dropped-from-nautilus/deploy.bash` | Same; named `filesystems=home:ro`.                                                                                                                                                                                                                           |
| `<repo>/CLAUDE/Plan/Completed/00110-vm-lifecycle-acceptance-testing/deploy.bash`                | Already covered by the long `EFFECT ON THE HOST` paragraph.                                                                                                                                                                                                  |
| `<repo>/CLAUDE/Plan/Completed/00111-terminal-death-takes-all-ccy-sessions/deploy.bash`          | **Partly folded in** — the gate's "host cc wrapper redeployed by play-claude-code.yml" now sits in the header.                                                                                                                                               |
| `<repo>/CLAUDE/Plan/Completed/00122-lxc-freeze-thaw-shared-with-podfreeze/deploy.bash`          | Already covered by the itemised `EFFECT ON THE HOST` list.                                                                                                                                                                                                   |

Also `<repo>/CLAUDE/Plan/Completed/00122-lxc-freeze-thaw-shared-with-podfreeze/acceptance.bash`: its
`STANDARD-EXCEPTION(R7)` note justified declaring `gather` partly on the grounds that `deploy`
"would demand `plan_gate_change` (R8)". That rationale no longer exists, so I rewrote it to rest
on what remains true — what it changes it created and destroys.

No `plan_confirm` call was touched.

## Docs

- **`<repo>/CLAUDE/PlanScriptStandards.md`** — rewrote the gate rule in place and retitled it
  *"No blanket confirmation prompt; running the script IS the consent"*, with the three reasons
  (the invocation is the statement of intent; the batch-run prompt reads as a hang; a gate people
  type through trains the reflex it exists to interrupt), then the three things that *do* protect
  a state-changing run: `plan_mode deploy` vs `gather`, `--check`, `plan_require_host`. It states
  the description string's substance is still owed to the reader, in the header comment. R5's
  title dropped `plan_gate_change` and now draws the line between a specific question
  (`plan_confirm`) and a blanket "may I proceed?".
- **`<repo>/CLAUDE/PlanWorkflow.md`** — removed `plan_gate_change` from the primitives list;
  the mandatory-`deploy.bash` bullet now says the header comment names what changes, and that the
  script does not stop to ask permission.
- **`<repo>/CLAUDE/Plan/CLAUDE.md`** — one stale mention ("the prompts, the change gate or the
  ansible invocation") that the grep pattern did not catch, since it names the concept in prose
  rather than the symbol.

## Two things to decide, and one correction

**The rule is R8, not R9.** In `PlanScriptStandards.md` the gate was
`### R8 — Gate on state-change, not on target`; R9 is *"Triage gathers facts; acceptance renders
the verdict"*. I rewrote R8 in place, so all numbering including R10+ is unchanged. But
`scripts/test-planlib.bash:355` — your file — asserts with the message
`"plan_gate_change does not exist (see PlanScriptStandards.md R9)"`, which now points a reader at
the triage/acceptance rule. That citation needs to be R8.

**`_planlib.inc.bash:745` still documents the removed function.** The `-y|--yes` help text reads
`consent non-interactively (skips plan_confirm / plan_gate_change prompts)`. You own that file, so
I left it — it is absent from my final grep only because your edits to other lines landed after my
first grep ran.

**A knock-on in `00079/deploy.bash`.** The gate was that script's *only* prompt, which made
`-y|--yes` dead. I removed the flag, its `case` branch and its two usage lines. No caller passes it
(the only repo references are prose in `PLAN.md`). Say the word if you would rather it stayed as an
accepted no-op.

**Pre-existing lint defect I had to fix to proceed.** Four `Completed/` scripts (00102, 00103,
00110, 00111) carried `# shellcheck source=../_planlib.inc.bash` — correct while the plan was
active, one level too shallow once `git mv`'d into `Completed/`. `shellcheck -x` failed SC1091 on
all four *before* my changes, which blocked every edit via the lint hook. I corrected them to
`../../`. 00122 already had it right, which is how I confirmed the intended form.

## Verification

`bash -n` and `shellcheck -x` run on all 13 touched scripts: all clean.
`bash scripts/test-planlib.bash` passes (library version 1.4.0).

Final grep — live code and docs are clean. Remaining hits:

- `CLAUDE/PlanScriptStandards.md:205` — deliberate: the new R8 names the removed function so a
  future author does not reinvent it.
- `scripts/test-planlib.bash:354-356` — your negative assertions that it stays removed.
- 8 hits in `subagent-reports/`, 1 in `JOURNAL/` — untouched as instructed.
- **`CLAUDE/Plan/Completed/00110-vm-lifecycle-acceptance-testing/DESIGN.md:401` and `:1893`** — the
  one judgement call I did not make for you. Neither `subagent-reports/` nor `JOURNAL/`, so it
  falls outside your exclusion as written, but it is a completed plan's design record describing
  what was built at the time (`:1893` is a ticked task-list entry, `:401` a design rationale).
  Rewriting it would falsify the record the same way, so I left it. Tell me if you want it updated.

Nothing committed, no playbook or `deploy.bash` run.
