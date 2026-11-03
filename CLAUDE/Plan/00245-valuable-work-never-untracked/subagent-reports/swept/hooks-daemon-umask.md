# Bug report: daemon writes world-writable files (`os.umask(0)` never restored)

**Component**: `claude-code-hooks-daemon`
**Version observed**: 3.51.0
**Severity (reporter's assessment)**: Medium — confidentiality/integrity of archived conversation transcripts on multi-user or backed-up hosts. Not remotely exploitable; requires local access or an off-host copy.
**Environment**: Linux, rootless Podman container, daemon running in self-install mode

---

## Summary

The daemon calls `os.umask(0)` during daemonization and never restores a
restrictive umask. Every file and directory it subsequently creates without an
explicit mode is therefore created **world-writable** (`0666` files, `0777`
directories).

The most sensitive of these is `transcript_archiver`, which writes **verbatim
copies of entire Claude Code conversations** to
`<untracked>/transcripts/transcript_<timestamp>.json`. In the project where this
was found, that directory held **8.3 MB of conversation archives at mode `0666`
inside a `0777` directory**.

Conversation transcripts routinely contain API keys, tokens, `.env` contents,
command output and private data. Anthropic's own documentation states that
Claude Code session files are not encrypted at rest and that **OS file
permissions are their only protection**
(<https://code.claude.com/docs/en/claude-directory>, "Plaintext storage"). The
daemon removes that single protection for the copies it makes.

---

## Evidence

All commands run inside the affected container; outputs unmodified apart from
redacting session UUIDs.

### 1. The archives, and their modes

```console
$ ls -ld  .claude/hooks-daemon/untracked/transcripts
drwxrwxrwx. 1 ... .claude/hooks-daemon/untracked/transcripts

$ ls -l .claude/hooks-daemon/untracked/transcripts
-rw-rw-rw-. 1 ... 2635355 Jul 30 08:12 transcript_20260730_081207.json
-rw-rw-rw-. 1 ... 3250838 Aug  7 06:18 transcript_20260807_061838.json
-rw-rw-rw-. 1 ... 2792824 Aug 12 13:49 transcript_20260812_134908.json
```

Contents are the full transcript, not a digest:

```json
{"archived_at": "2026-07-30T08:12:07.250011",
 "transcript_path": "<home>/.claude/projects/<project>/<session-uuid>.jsonl",
 "transcript": "{\"type\":\"last-prompt\", ...
```

### 2. The umask of the live daemon

```console
$ grep -i '^Umask' /proc/<daemon-pid>/status
Umask:	0000
```

Against every other process in the same container:

```console
Umask:	0022      # PID 1 (init)
Umask:	0022      # the Claude Code process
```

This confirms the daemon is not *failing to inherit* a umask — it explicitly
**overwrites** its own. Umask survives `fork()`, so no amount of setting a
restrictive umask in the parent (a container entrypoint, a shell, a systemd
unit) can influence it.

### 3. Still reproducing on current code

After restricting the whole tree by hand, two files reappeared world-writable
**within the same minute**:

```
666 .claude/hooks-daemon/untracked/context-sidecar/<session-uuid>.json
666 .claude/hooks-daemon/untracked/thread-registry/<session-uuid>.json
```

So this is continuous, not a historical artefact of one old release.

---

## Root cause

`src/claude_code_hooks_daemon/daemon/cli.py:575`, in the double-fork
daemonize path:

```python
    # First child - decouple from parent environment
    os.chdir("/")
    os.setsid()
    os.umask(0)          # <-- never restored
```

`os.umask(0)` is the classic textbook daemonization step (Stevens, *APUE*). Its
purpose is to stop an **inherited** umask from silently clearing bits a daemon
explicitly requests — i.e. it is only safe when every subsequent create passes
an explicit mode. This daemon does that for exactly two artifacts:

| Path        | Mode    | Where                                               |
| ----------- | ------- | --------------------------------------------------- |
| Unix socket | `0o660` | `daemon/server.py:546` — `socket_path.chmod(0o660)` |
| Lock file   | `0o600` | `daemon/server.py:616` — `os.open(..., 0o600)`      |

Every other create relies on the default and therefore lands at `0666`/`0777`.
For the archiver specifically:

- `handlers/pre_compact/transcript_archiver.py:106` — `archive_dir.mkdir(parents=True, exist_ok=True)` → `0777`
- the subsequent transcript write → `0666`

The socket and lock lines are good evidence that the "explicit mode" half of the
`umask(0)` contract was understood; it simply was not applied to the data files.

---

## Impact

What this does **not** do: it does not expose anything remotely, and on a
single-user machine with no other local accounts the practical exposure is
limited.

What it **does** do:

1. **Any local user can read every archived conversation** — API keys, tokens,
   file contents, customer data. `0666` also means any local user can *modify*
   them, so the archives are not trustworthy as a record.
2. **It defeats the only protection the upstream vendor documents.** Anthropic's
   position is explicitly "permissions are the only protection". A client
   project that hardens `~/.claude` and its own state directory still leaks via
   the daemon's copies, in a directory it may not know exists.
3. **It cannot be mitigated by the host.** Because the umask is overwritten
   after fork, a project cannot fix this by setting a umask in its container
   entrypoint, launcher or unit file. The only downstream mitigation is a
   repeating `chmod` sweep, which is inherently racy — the daemon re-creates the
   files continuously.
4. **It widens copy-based exfiltration.** `untracked/` is gitignored, so git is
   not a leak path — but `.gitignore` does nothing for `rsync`, `restic`,
   `borg`, `tar`, Dropbox or Syncthing, and the directory sits inside the
   project working tree by design.

**Directory containment is the decisive factor, and it is exactly what is
missing here.** Reaching a file requires search (`+x`) permission on every
ancestor directory, so a `0666` file inside a `0700` directory is unreachable by
other users. That is why the `0777` archive directory — not the file mode — is
the core of this report: a world-writable directory contains nothing.

Worth noting for triage rather than as a criticism: the daemon ships a
`dangerous_permissions` handler that blocks *users* from creating world-writable
files, and `constants/rule_ids.py:82` describes that rule as
"world-writable permissions". The standard the project enforces on its users is
the one being missed internally, which suggests this is an oversight rather than
a deliberate design decision.

---

## Reproduction

1. Install the daemon in any project and start it (`bin/hooks-daemon restart`).
2. Confirm the daemon's umask:
   `grep -i '^Umask' /proc/$(cat <untracked>/daemon-*.pid)/status` → `0000`.
3. Trigger a `PreCompact` event (let a session compact naturally, or dispatch a
   synthetic `PreCompact` with a `transcript_path`).
4. `ls -l <untracked>/transcripts/` → files at `0666`, directory at `0777`.

Faster path with no compaction needed: start a session and inspect
`<untracked>/context-sidecar/` and `<untracked>/thread-registry/` — both are
`0666` as soon as they appear.

---

## Suggested fixes

Listed most-preferred first. All are small; the choice is about how defensive
the project wants to be.

### A. Set a restrictive umask instead of `0`

```python
    os.setsid()
    os.umask(0o077)   # daemon-private by default; explicit modes still win
```

One line, fixes every current and future create at once. Safe for the two
explicit-mode call sites: the lock at `0o600` is unaffected, and the socket is
set by an explicit `chmod` **after** bind, so it still ends at `0o660`.

*Caveat to check before adopting*: if any deployment genuinely relies on a
second user (not just a second process of the same user) reading daemon runtime
files, `0o077` would break it and `0o007` would be the group-preserving
equivalent. The socket's `0o660` hints group access may be intended there — but
since the socket mode is set explicitly, the umask does not govern it.

### B. Pass explicit modes at every sensitive create

Keep `os.umask(0)` and make the contract real, at minimum in
`transcript_archiver`:

```python
archive_dir.mkdir(parents=True, exist_ok=True, mode=0o700)
# and write via os.open(..., O_WRONLY | O_CREAT | O_EXCL, 0o600)
```

Note `Path.mkdir(mode=...)` is itself umask-masked, which under `umask(0)` is
fine, but this option needs auditing at every write site — more places to get
wrong later than option A.

### C. Both

A as the default posture, B on the transcript archive specifically, since that
is the one artifact whose contents are known to be sensitive. Defence in depth,
and it survives someone later "fixing" the umask line back.

### Regression test suggestion

A test that daemonizes (or calls the daemonize helper), creates a file through
the normal path, and asserts `stat().st_mode & 0o077 == 0` would pin this. A
unit test asserting `os.umask` is called with a restrictive value is weaker but
cheap.

### Possible follow-up

Existing installations already have world-writable archives on disk; fixing the
umask does not retro-fix them. A one-line note in the upgrade guide — or a
startup pass that restricts the daemon's own `untracked/` tree — would close
that out for users who never think to look.

---

## What we did downstream (for context, not as a request)

We did **not** modify anything under `.claude/hooks-daemon/` — it is a vendored
upstream dependency in our repo and we treat it as read-only.

Our mitigation is a launch-time sweep over the whole project `.claude/` tree
that clears group/other bits while preserving owner bits:

```bash
find "$PWD/.claude" \( -type f -o -type d \) -perm /077 -exec chmod go= {} +
```

This is explicitly a workaround, and a poor one: it is racy against a daemon
that re-creates the files continuously, so between launches the exposure
returns. It is in place only because there is no way for us to influence the
daemon's umask from outside the process.

Happy to test a patch against this environment if that is useful.
