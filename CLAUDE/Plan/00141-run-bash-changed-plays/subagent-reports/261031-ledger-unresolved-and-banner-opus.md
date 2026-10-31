# Signing plays UNRESOLVED, and the banner read before the last collection

Branch `fix/ledger-unresolved-and-banner`, from 84a69b260.

## 1. UNRESOLVED in play-git-configure-and-tools.yml and play-github-cli-multi.yml

The input set comes from `helpers/self_update/affected_plays.play_inputs`, which
`changed_plays.judge` calls. `root_dir` is not the problem: each play defines it as
`{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}`, and the mapper reads both
spellings as the checkout root.

- **`:21` / `:7` `"{{ root_dir }}/vars/git-signing.yml"   # git_signing_key_path`.** The rooted
  scan already made `vars/git-signing.yml` an input. `_classify` also saw the line, through
  `_unquote`, which stripped quotes only when the value ended in a quote. A trailing comment
  kept them, the value no longer started with the root, and it was reported. Class: *a YAML
  scalar classified with its syntax (quotes, comment) still on it*. Fix: `_unquote` reads a
  quoted scalar up to its closing quote and drops a comment after it.
- **`:125` `"{{ git_signing_key_path }}.pub"` (`slurp: src:`).** `vars/git-signing.yml` sets
  `git_signing_key_path: "{{ git_signing_key | default('/home/' + user_login + '/.ssh/id') }}"`;
  `git_signing_key` is set only in host_vars (gitignored, not in the checkout).
  **Decision: excluded from the input set, not left UNRESOLVED.** It is never a checkout file,
  so no checkout change can stale the play through it. The rule (`_is_host_path_var`) is
  narrow so the Fail Fast meaning holds for anything else: every walked file that sets the
  variable must set it to `{{ X | default('/<absolute>...') }}`, and neither a walked file nor
  `environment/localhost/group_vars/desktop.yml` may set `X`. A relative default, or an `X` the
  checkout sets, stays UNRESOLVED. Such references are judged after every file is walked
  (`finish_headed`), so the answer does not depend on whether the vars file is read before the
  use.

`INVENTORY_GROUP_VARS` now lives in `affected_plays` (`group_vars.GROUP_VARS` is it); its
`not-play-inputs.json` entry moved with the literal.

Left UNRESOLVED on purpose: `play-self-update.yml:395/405`, the vault password file from
`DEFAULT_VAULT_PASSWORD_FILE`, joined to `root_dir` when relative. It can name a checkout path,
so the mapper cannot rule it out.

## 2. The "needs attention" banner after a batch

The callback restarted `host-health-collect.service` with `--no-block` after each play's
records. The host's meta-deploy log shows the batch ending with play-displaylink and the
shell meta-deploy handed back printing the banner within the same second, naming
play-displaylink as changed. That shell read the document of the collection the previous
play's restart had started, which ran during play-displaylink. The collection after the last
record had only been queued. A collection that then failed was reported to nobody either:
`--no-block` returns once the job is queued. Class: *a refresh that returns before the work
it asks for is done*.

Fix: `health_refresh.request` restarts the unit and waits for it, with a timeout (180 s) above
the unit's `TimeoutStartSec=120` (a test pins it). `restart` still ends a collection already
running (timer or previous play) and collects again after the records. A failed collection
now prints `HEALTH-REFRESH-FAILED` with systemd's reason in the run. Cost: one collection per
playbook run, waited for. The unit is unchanged, so nothing is deployed. The callback and
helpers run from the checkout. No meta-deploy.list edit.

Not verified on the host, because the container has no user manager: whether that last
collection also failed. If it did, the next run now says so.

## Defence Before Fix

- Class 1a: test-level guard `TestRealRepo.test_no_unresolved_value_is_still_quoted_or_a_followed_reference`
  (every host play: no UNRESOLVED value still quoted, no rooted literal that is also an input).
  Red on the defect, green after. Sweep: no other YAML scalar reader in helpers/play_ledger or
  helpers/host_health (group_vars compares blocks textually, which errs toward stale).
- Class 1b: `TestHostOnlyPaths` fixtures plus `test_the_signing_plays_have_nothing_unresolved`.
  A generic detector cannot tell a host-only variable from an unknown one. That is the mapper's
  own decision, so the guard is the tests.
- Class 2: `TestNoFireAndForgetRefresh` scans helpers/play_ledger, helpers/host_health and
  callback_plugins for a `--no-block` systemctl argument. Red on `health_refresh.py:85`, green
  after. No other instance.
