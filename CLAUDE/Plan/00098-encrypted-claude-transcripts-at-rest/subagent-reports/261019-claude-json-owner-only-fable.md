# Review: fix/claude-json-owner-only at c669e49e (Plan 00098)

Reviewed read-only against F44 9ffe988a. One commit; touches
`playbooks/imports/play-claude-yolo.yml`, `docs/playbooks.md` and a new
`CLAUDE/Plan/00098-encrypted-claude-transcripts-at-rest/JOURNAL/00098-Journal-26-10-19.md`.
`ansible-playbook --syntax-check playbooks/imports/play-claude-yolo.yml` exits 0. Nothing changed.

## Verdict: FIX-FIRST

1. **Check mode fails on exactly the host this fixes.** `ansible.builtin.file` supports check
   mode and reports `changed` without chmodding; the re-`stat` then still sees `0644` and the
   `assert` fails hard. The existing tree repair beside it survives `--check` only because
   `command` is skipped there and Ansible does not evaluate `failed_when` on a skipped result
   (`executor/task_executor.py`, `if 'skipped' not in result`). `--check` is a documented
   workflow (`docs/development.md:274`, `CLAUDE/InfrastructureAsCode.md:79`), so the new
   assert regresses it. Fix: add `- not ansible_check_mode` to the `when:` of the re-check and
   the assert (a check run has nothing to assert about).
2. **Symlinked `~/.claude.json` passes the repair and then fails the assert.** `file` defaults
   `follow: true` (chmods the target); `stat` defaults `follow: false`, so the re-stat returns
   the link's own mode (`0777`), `mode[-2:]` is `'77'`, the play fails after a correct repair.
   Claude Code itself writes through the symlink (`allowSymlink: true`, "Writing through
   symlink", see below), so following it is the right behaviour here; put `follow: true` on
   both stat tasks. This is a one-file case, unlike the tree find that excludes symlinks for
   the reason its comment gives. Low likelihood, but a hard failure when it bites.
3. **PLAN.md not advanced.** Only a journal entry was added. Task 1.2 is the task this extends
   ("Repair the desktop store") and still names `play-claude-code.yml`, a play that no longer
   exists (it is `play-claude-yolo.yml`). Per the Plan Commit Rule the task text should record
   that `~/.claude.json` is covered, and the stale play name is the moment to fix it.

## What is right

- **Not a recurring repair — high confidence.** I read the installed Claude Code 2.1.303 bundle
  (`strings` of `bin/claude.exe`). The config save (`saveConfigWithLock`, minified `Ho`) ends
  in an atomic write (`pC`) called with `{mode: 384, allowSymlink: true}`. With `allowSymlink`
  it `stat`s the existing target and **preserves its mode** on the temp file before the
  rename ("Preserving file permissions", "Applied original permissions to temp file"); `0600`
  is applied only to a new file ("Setting permissions for new file"). The in-place fallback
  path also keeps the inode's mode. So once the play sets `0600`, every later rewrite keeps
  `0600`; the host's `0644` is a legacy creation that this one-off repair ends.
- **Backup mode copy confirmed.** `Ks` backs up with `fs.promises.copyFile(src, backups/ .claude.json.backup.<ms>)` (mode copied from source) and keeps 5; the storage-backend variant
  (`KO`) writes with `exactMode: statMeta(globalConfig).mode` defaulting to `0600`. The play
  comment "copies its mode into backups" is accurate, and the recurring `changed` on
  "Restrict Claude Code State to the Owner" stops once the source is `0600`.
- Assert expression: `stat` returns `mode` as `"%04o" % S_IMODE` (modules/stat.py:374), always
  four octal digits, so `mode[-2:] == '00'` is correct for every form including setuid/sgid.
- `file` with only `mode:` leaves owner and group alone; `go-rwx` keeps owner bits; `changed`
  only when bits actually differ; absent file is skipped on every task (never created);
  runs as the login user like the surrounding tasks (play is `become: false`, no task-level
  `become`), which is the right user for a file in that home.
- Naming is Title Case per AnsibleStyle; placement directly after the tree assert is right;
  `claude_state_file` beside `claude_state_dir` with a reason comment; no install-specific
  names in play, docs or journal; docs/playbooks.md line is accurate.
- Container evidence agrees: this container's own `~/.claude.json` is `0600` under umask
  `0077`, as a fresh create under the preserved-or-0600 rule predicts.

## Not covered

No test exercises these tasks (fixer says so too); the verification above is from reading
Ansible and Claude Code source, not a run. A host deploy is still needed to confirm.
