# QA Review — uncommitted working-tree change to `playbooks/imports/optional/common/play-qobuz.yml`

**Verdict**: FIX-BEFORE-MERGE

The core fix is correct. I verified the regex, the config key, the module semantics and the
notify path against upstream source rather than taking the description on trust, and all four
check out. Two things need addressing before this lands: the verify step cannot tell
"restarted with the new config" from "still running the old one", and `docs/playbooks.md` now
understates what the play does.

---

## Should fix

### 1. The verify step is satisfied by exactly the broken state this change exists to fix

`<repo>/playbooks/imports/optional/common/play-qobuz.yml:376-382`

```yaml
failed_when: rescrobbled_status.status.ActiveState != "active"
```

`ActiveState == active` is equally true for a `rescrobbled` process started *before*
`config.toml` was written. The restart only happens when Ansible observes the change **in this
run**. If a run aborts between the copy (line 289) and the flush (line 371), the notification
is dropped — there is no `force_handlers`, and `<repo>/ansible.cfg` sets
`any_errors_fatal = true`. There is a real abort window in between: the
`Manual Rescrobbled Setup Required` block (lines 324-354) contains two `ansible.builtin.pause`
prompts and a `fail` task. A Ctrl-C at the pause, or the session-file `fail`, ends the play
after the config is on disk.

Every subsequent run then sees an unchanged `config.toml`, notifies nothing, restarts nothing,
and prints `Rescrobbled successfully installed and running!` while the old process keeps
double-scrobbling. A clean result and a blind result are indistinguishable — the class in
`CLAUDE/AgentNotes.md` "A partial result read as a complete one".

- **Minimum fix**: `force_handlers: true` at play level.
- **Convergent fix**: the module returns the whole `systemctl show` dict, and
  `ExecMainStartTimestamp` / `ActiveEnterTimestamp` are in it (documented in the module's own
  RETURN sample, `systemd_service.py:160-194`), so the play can `stat` `config.toml` and drive
  the restart off "running process predates its config" instead of off `.changed`. That is
  idempotent, survives an interrupted run, and makes the verify actually verify the claim.

### 2. `HIFIRS_UNCHANGED` is written but never read

Lines 50 and 63.

```yaml
changed_when: "'HIFIRS_INSTALLED' in hifirs_install.stdout"
```

Only the `INSTALLED` marker is consumed. Absence of it is read as "unchanged", but absence is
equally the signature of a script that printed nothing at all. As written, `HIFIRS_UNCHANGED`
is dead output — it influences no decision. Either drop the echo (YAGNI) or, better, assert
exactly one marker so a silent no-op fails loudly rather than reporting `ok`:

```yaml
failed_when: >-
  hifirs_install.rc != 0
  or (('HIFIRS_INSTALLED' in hifirs_install.stdout) == ('HIFIRS_UNCHANGED' in hifirs_install.stdout))
```

The `rc != 0` clause is load-bearing — adding `failed_when` replaces the shell module's default
rc check, and dropping it would silently disable fail-fast on this task.

### 3. Doc drift: `docs/playbooks.md` still describes undifferentiated scrobbling

`<repo>/docs/playbooks.md:959` and `:972`

> `- **rescrobbled**: Last.fm scrobbling systemd service`
> `- Last.fm scrobbling integration`

After this change, ownership is deliberately split: rescrobbled scrobbles hifi-rs and is now
configured to ignore QBZ, which scrobbles itself. Someone debugging a missing or a duplicated
scrobble gets no hint from the docs that two different mechanisms are in play. One line in the
`play-qobuz.yml` section covers it. The in-play success message at line 384-391
(`Your music will now be scrobbled to Last.fm`) has the same gap.

---

## Nits

### 4. Double restart on a first deploy

Lines 361-366 vs 368-371. `Start and Enable Rescrobbled Service` runs before the flush, so on a
first install the service is started and then immediately restarted by the handler. Harmless;
moving the flush between the daemon-reload (line 359) and the start task would make the start a
no-op instead. The daemon-reload ordering stays correct either way.

### 5. `^QBZ$` has no staleness signal

The match is an exact anchor on an upstream-controlled string. If QBZ renames its MPRIS
identity in a Flathub update, the filter silently stops matching and double-scrobbling returns
with no error anywhere. There is no clean repo-side fix and the regex must **not** be widened.
Worth one line in the existing comment saying the failure mode is silent, so a future debugger
knows where to look.

---

## Checked and clean

### `notify` on a `no_log: true` task — reliable, the handler does fire

`no_log` is a display concern only. `_ansible_notify` is attached to the result at
`task_executor.py:773`, *after* `update_result_no_log` runs at line 434, and that function only
sets an `_ansible_no_log` flag (`ansible/playbook/base.py:773-793`) — it strips no keys. The
strategy fires on `if '_ansible_notify' in result_item and task_result.is_changed()`
(`plugins/strategy/__init__.py:620`); `changed` is a bool and survives sanitisation untouched.

### `flush_handlers` placement — correct, and the daemon-reload ordering holds

`Reload systemd daemon for user` (lines 356-359) is unconditional and precedes the flush at
370-371, so a handler notified by the unit-file task (line 317) always gets its reload first.

### `ansible.builtin.systemd` with a bare `name:` — genuinely non-mutating, `status.ActiveState` is the right key

I checked the obvious trap first: the module declares
`required_one_of=[['state','enabled','masked','daemon_reload','daemon_reexec']]`
(`systemd_service.py:357`), which looks like it should reject this task. It does not —
`daemon_reload`/`daemon_reexec` carry `default=False`, and `_set_defaults(..., set_default=False)`
injects any key whose default `is not None` into the parameter dict
(`common/parameters.py:505`) at `arg_spec.py:233`, *before* the additional checks run at line
243. `check_required_one_of` counts key presence, not truthiness (`common/validation.py:37`).

Confirmed by running the validator directly against the module's spec with
`{'name': 'rescrobbled', 'scope': 'user'}`:

```
ERRORS: []
PARAMS: {'name': 'rescrobbled', 'scope': 'user', 'daemon_reload': False, 'daemon_reexec': False,
         'no_block': False, 'state': None, 'enabled': None, 'force': None, 'masked': None}
```

With `state`/`enabled`/`masked` all `None` the module skips every mutation branch (lines 481,
502, 548) and exits with `changed=False` after a single `systemctl --user show`.

### Fail-fast is preserved and equivalent to the old `is-active` check

`systemctl show` returns rc 0 even for a nonexistent unit, with `ActiveState=inactive` and
`LoadState=not-found`, so `failed_when` fires cleanly. If the user bus is unreachable the module
hard-fails at `module.run_command(systemctl, check_rc=True)` (line 473). Neither path can pass
silently.

### `^QBZ$` is correct, and cannot match hifi-rs

Verified against upstream v0.10.0, fetched:

- `player-ignorelist` is the right key — `player_ignorelist` under
  `#[serde(rename_all = "kebab-case")]` (`src/config.rs:81,106`), documented in the README at
  lines 31-32 and 67-71. No `deny_unknown_fields`, and the `RegexSet` deserializer validates the
  pattern, so a typo would surface at startup rather than being ignored.
- `regex_set_contains` tests `player.identity()`, the prefix-stripped bus name, and the
  instance-stripped bus name (`src/player.rs:43-52`) — the anchors are necessary and correct for
  an unanchored `RegexSet`.
- The journal line quoted in the task context is printed with `player.identity()`
  (`src/mainloop.rs:64-68`), so the identity really is `QBZ`.
- hifi-rs returns the literal `"hifi-rs"` from `identity()` and registers
  `org.mpris.MediaPlayer2.hifirs` (`iamdb/hifi.rs`, `hifirs/src/mpris.rs:226-228` and `:37`).
  `^QBZ$` matches none of `hifi-rs`, `hifirs`, `hifirs`. rescrobbled keeps scrobbling hifi-rs.

### Stderr hygiene — compliant

`set -x` traces go to fd 2; the version-detail line now carries `>&2` (line 49); `wget` writes
progress to stderr and `tar`/`mv` are silent, so stdout on the install path is the marker alone.
`changed_when` is correct for the early-exit path: no `HIFIRS_INSTALLED`, no change. See finding
2 for the gap this leaves.

### Fail-fast annotations

No new `failed_when: false` or `ignore_errors: true`. The one pre-existing instance (line 438)
carries its `# FAIL-FAST-OK:`. `qa-ansible.bash` green.

### Permissions and ownership

No play-level `become` and `transport=local`, so the play runs as the invoking user;
`owner`/`group` set to `{{ user_login }}` are chown-to-self no-ops, and
`group: "{{ user_login }}"` matches the user-private-group idiom already used at line 427-428.
`0700` on `~/.config/rescrobbled/` is right for a dir holding the API secret and session token.
This brings the play *into* compliance with `CLAUDE/AnsibleStyle.md` "Always set `owner:`,
`group:`, `mode:` on every file task".

### IaC placement

No new play, no runtime probing for known state, no misplaced work. The handler pattern is
exactly what `CLAUDE/AnsibleStyle.md` prescribes ("Use handlers for service restarts triggered
by config changes"). `play-qobuz.yml` is optional and correctly not imported by
`playbook-main.yml`. This play is the sole owner of rescrobbled repo-wide — nothing else
references it except `scripts/check-pinned-versions.bash:84`, which tracks `rescrobbledVersion`
and is unaffected.

### Naming

Task and handler names are action-oriented and specific. No `: -x` pattern in any unquoted
`name:`. The comments explain WHY, all sit outside `shell:` blocks (so the 2.19 quote-balance
trap does not apply — the apostrophe in "player's name" at line 276 is safe), and the longest is
15 lines against a 40-line limit.

### Plan Commit Rule — not implicated

No plan tracks this work (`grep -rli 'rescrobbled|qobuz|scrobbl' CLAUDE/Plan/` finds only an
unrelated MusicCast research doc), and `git status --untracked-files=all CLAUDE/Plan/` is clean.

---

## Mechanical gates

| Gate | Result |
| ---- | ------ |
| `qa-all.bash` | **RED, pre-existing and unrelated** — see below |
| `scripts/qa-ansible.bash` | PASS |
| `scripts/qa-ansible-syntax.bash` | PASS — 78 playbooks (75 under `playbooks/imports/`, 3 elsewhere) |
| `plan-qa --sweep` | 2 findings, **0 blocking** (advisory staleness on unrelated dormant plans) |
| `ansible-playbook --syntax-check` on the changed play | PASS |
| `ansible-lint --profile production` on the changed play | PASS, 0 failures / 0 warnings |

`qa-all.bash` short-circuits with:

```
✗ python: ruff version mismatch — this gate's verdict is version-dependent.
    expected : 0.16.4  (.ruff-version)
    found    : 0.16.0  (<home>/.local/bin/ruff)
ERROR: Missing required tools. Install them and re-run.
```

This is a known accepted state: commit `d45da38` bumped the pin today and its own message says
"The CCY image needs a rebuild to match." The diff touches one `.yml`, so it cannot have caused
this. **The caller will be blocked at commit time and must not work around it** — per
`CLAUDE.md` "Missing Dependencies", the fix is to rebuild the CCY image so the container's ruff
matches the pin. Shellcheck reported 147 advisory issues; `bash: 203 files OK`.

### Conditional gates not triggered

- No `helpers/` or `tests/helpers/` change — `qa-helper-tests.bash` not required.
- No `extensions/` metadata or JS change — `check_extension_compat` and ESLint not required.
- No `files/var/local/claude-yolo/` change — **no CCY version bump is owed**.
- No Dockerfile or image-baked file touched — `REQUIRED_CONTAINER_VERSION` untouched.
- Shebang and exec bit on the playbook are unchanged and intact.
