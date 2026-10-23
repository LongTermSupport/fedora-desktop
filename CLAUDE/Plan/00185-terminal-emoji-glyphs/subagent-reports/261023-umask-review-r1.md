# Review r1: Plan 00185 DBF remediation, ansible-umask-pinned (dcb76af56..10118f437)

Verdict: **CHANGES** (one must-fix; the mechanism itself is sound).

## What was verified

- `python3 -m unittest tests.helpers.umask_pin.test_umask_pinned_rule tests.helpers.umask_pin.test_umask_pin_against_real_ansible`: 14 tests OK (real ansible-core 2.19.14, probe 0644 under inherited 077, control 0600).
- `ansible-playbook --syntax-check` of play-fonts.yml and play-basic-configs.yml: clean.
- Red commit 3a9eee25f holds only the rule, the probe and QA.md; ansible.cfg and the plugin arrive in the green commit, so both red failures are real.

## Mechanism (callback in ansible.cfg vs umask in run.bash/meta-deploy)

Effective. Read from the installed ansible-core 2.19 source:

- `PlaybookExecutor.run()` calls `tqm.load_callbacks()` before the first play; `TaskQueueManager.run()` calls it again (no-op) before `_initialize_processes`; `WorkerProcess` forks happen in the strategy afterwards. The constructor's `os.umask` therefore reaches every worker, every local-connection module and every command child.
- Ad-hoc `ansible` (vault.bash, run.bash vault probes, push.bash) is covered too: `ansible.cfg` sets `bin_ansible_callbacks = true`, so `run_additional_callbacks` is true.
- sudo uses the union of the caller's umask and sudoers' 0022, so 022 in gives 022 out for become tasks.
- Self-update: `cycle.py` sets `ANSIBLE_CALLBACK_PLUGINS={clone}/callback_plugins:...`, and the clone's ansible.cfg enables the pin. Its unit already has 0022 anyway.
- Pinning in Ansible rather than in run.bash is the right call: it covers bare `ansible-playbook`, `qa-changed` (ANSIBLE_CONFIG), and the self-update. No plan script still in use writes system files with a direct sudo (only a 00073 acceptance probe), so a meta-deploy-level umask would add little.

fontconfig repair: `fc-cache -f` writes each cache through `FcAtomic` with `open(..., 0666)` (this is what produced 0600 under 077), so under 022 every rewritten file is 0644. The `find` plus `rejectattr('roth')` gate is correct. It runs as root through the play-level `become`, and its task-level `vars` are visible in `when`. So the next run does rebuild even though the conf file is unchanged. Handlers run in definition order, so the system cache is rebuilt first and then the user's. `become: false` is the controller user, which is the desktop user from run.bash and from the self-update (`play_environment(user=...)`). That is correct.

## Must fix

1. **meta-deploy.list is not exact.** The commit removes the (already run) `changed-plays` entry and changes `play-basic-configs.yml`: the yq `chmod 0755` plus a new `file` 0755 task (H2), and `/etc/vimrc.local` `mode: "0644"` (H7). Nothing in the queue runs that play, so the two repairs the DBF report lists as done never reach the host. `changed_plays.py` would pick it up, because the play's own file changed. Add a `changed-plays` entry, with a description naming play-basic-configs (yq/vimrc modes) and play-fonts, above `00185-terminal-emoji-glyphs`, as the previous entry was placed. Alternatively add a `playbooks/imports/play-basic-configs.yml` entry.

## Should fix (not blocking on their own)

2. **The runtime failure mode is fail-open.** If `umask_pin` cannot load, `load_callbacks` only warns ("Skipping callback plugin ... unable to load", or `error_as_warning` on a constructor exception) and the run proceeds under the inherited umask. The static rule checks the configuration, not that the plugin file exists. The e2e probe does cover that, but only in qa-all/CI, because qa-changed does not route ansible.cfg or callback_plugins/ to these tests (the report records this gap). Either route those paths in qa-changed or have run.bash check the pin. The cheap fix is routing.
3. **QA.md overclaims, against its own rule docstring.** QA.md's "How to fix" says "the one pin covers every launcher ... the plan scripts ... a bare `ansible-playbook`". Its "Not caught" list omits "a run from a directory with no ansible.cfg" (Plan 00062 A14/A15), which the test module's docstring lists. Align the two.
4. **Changelog narrative in comments** (R-COMMENT-CHANGELOG spirit):
   - play-basic-configs: "an install run under a restrictive umask left it 0700" is a past event, and as far as the report shows it was never observed on the host. Say what is true now ("`creates:` skips the install, so the mode is enforced separately").
   - play-fonts: "(a run from a cc session before umask_pin, Plan 00185)" has the same problem.
5. H3 to H6 (root git clones, the ansible-galaxy collections, `dconf update`) are left to the owner as "a read-only host triage" that nobody queued. A small triage script in the plan, queued in meta-deploy, would turn the referral into evidence. The rule says only a decision is asked for separately.

## Checked, no finding

Fail-fast (no failed_when/ignore_errors added), 2.19 parser (no `: -x` in names, no apostrophes in shell-block comments), stderr hygiene (acceptance/triage use the plan's ok/bad/run helpers), plan-script standards (acceptance DECLARED bumped to 18, check numbered), docs/playbooks.md and deploy.bash header updated, PLAN.md Task 2.3 and JOURNAL in step. No install-specific names or paths in the diff. The independent-search report uses only repo paths. The pin makes cc-launched plays write 0644 where they wrote 0600, which matches a terminal run, so it reopens nothing Plan 00098 protects (that is ~/.claude, written by cc itself, not by Ansible).
