# Design proposals — Plan 00098

Three independent designs, each written to be as strong as its angle allows.

## Blast-Radius Reduction (ccy-state-hygiene)

**Stop treating "plaintext on disk" as one problem: fix the 879 currently-world-readable state files, cut retention 30d→7d with a pinned managed-settings file, block credential reads at source, mark the tree non-backupable, and age-seal only the *archived* sessions with a key the container never sees — leaving the live transcript untouched so nothing in Claude Code or the hooks daemon can break.**

### Architecture

Six layers, deployed as one play, ordered by measured risk-reduction per unit of complexity. No FUSE, no loop device, no CAP_SYS_ADMIN, no new mount, no interception of any write Claude Code performs.

L0 — POSTURE (the hole that is actually open today). Probed in this container: `.claude/ccy` is mode 755, `file-history/` 755, `shell-snapshots/` 755, and 879 of 978 state files are other-readable — including 344 verbatim pre-edit file bodies. `projects/` is already 700, so the directory the user wants to encrypt is the one directory already protected; the leak is beside it. Root cause is that nothing sets a umask: `command grep -c umask entrypoint.sh` → 0. Fix: `umask 077` in `entrypoint.sh` before it execs Claude Code (every new file 0600, every new dir 0700, forever), `umask 077` in the desktop `cc` wrapper at `/var/local/claude-code/cc`, and a one-time reconciliation in the playbook (`mode: "u=rwX,go="` recursive over `~/.claude` and `.claude/ccy`) plus an idempotent repair in the CCY preflight so drift cannot re-accumulate.

L1 — SOURCE CONTROL. `files/etc/claude-code/managed-settings.json` (Ansible template) with `permissions.deny` rules for `Read(**/.env*)`, `Read(**/*.pem)`, `Read(**/id_ed25519)`, `Read(**/id_rsa)`, `Read(**/vault-pass.secret)`, `Read(**/.credentials*)`, plus matching `Bash(cat:*)` shapes. Managed scope is highest-precedence and cannot be overridden by a poisoned project settings file — the CVE-2026-21852 shape. Second, repo-owned layer: a new project handler `.claude/hooks/handlers/pre_tool_use/credential_read_blocker.py`, sitting beside the existing `system_paths.py` and `ansible_enforcement.py`, so the rule travels with the repo and is testable. A secret that never enters the transcript needs no encryption.

L2 — RETENTION. Same managed-settings file pins `cleanupPeriodDays: 7`. Pinning matters: a malformed user settings file makes the sweep skip entirely (`skip_reason: settings_invalid_key_set`) and retention silently degrades to forever; a managed value makes it run anyway. Measured today: oldest transcript is 29 days old, so the default sweep works — this cuts the window ~77%.

L3 — SEAL THE ARCHIVE, NOT THE LIVE FILE. A new host helper `files/home/.local/bin/ccy-seal`. On every CCY launch (host side, before `podman run`) it walks `.claude/ccy/` and seals everything that does not belong to the N most recent sessions — `projects/**/*.jsonl`, `subagents/`, `tool-results/`, `file-history/`, `shell-snapshots/`, `paste-cache/`, and rotated `history.jsonl` chunks — into `.claude/ccy/.sealed/<relpath>.age`, then unlinks the plaintext. Sealing uses `age -R recipients.txt`: PUBLIC-key encryption, so it needs no passphrase, no prompt, and no usable key material anywhere on the box. The live session's `.jsonl` is never touched, so `transcript_reader.load_tail()`/`read_incremental()` byte-offset reads keep working, `/resume` keeps working, and the Stop handlers this repo depends on keep working.

L4 — BACKUP BOUNDARY. `.claude/ccy/CACHEDIR.TAG` (standard `Signature: 8a477f597d28d172789f06886806bc55`), written by `entrypoint.sh` and by the playbook for `~/.claude`. borg, restic, tar `--exclude-caches` and rsync `--exclude-caches` all honour it. This is the cheapest possible fix for one of only two scenarios where encryption genuinely beats the existing LUKS FDE — sync/backup exfil of the working tree — and costs 43 bytes.

L5 — DESKTOP PARITY. `~/.claude/projects/` gets the identical treatment: umask via the `cc` wrapper, mode fix, `CACHEDIR.TAG`, the same managed-settings file (it is host-wide), and the same `ccy-seal` binary driven by a systemd **user** timer `claude-state-seal.timer` (daily) with `--root "$HOME/.claude"`. Desktop CC has no launcher hook, so a timer is the honest mechanism. Same recipients file, same sealed format, one `ccy-seal --unseal` for both stores.

Container never participates: sealing and unsealing run only on the host, in the launcher. The container has no `age` key, no recipients file, no ability to unseal. The thing running untrusted agent code holds no key material at all — a property neither a FUSE-mount nor an in-container-crypto design can claim.

### Key handling

DELIBERATELY ASYMMETRIC — sealing needs no secret, so the common path has no prompt.

Generated once by the playbook via `age-keygen`:
- `~/.config/claude-state-seal/identity.age` — the private identity, itself passphrase-encrypted by `age -p`, mode 0600. Generated only if absent AND non-empty (per the repo's `ansible.builtin.password` gotcha: the playbook `stat`s it and `assert`s `size > 0`, with a `fail_msg` naming the file to delete, so a truncated identity can never be treated as "already generated").
- `~/.config/claude-state-seal/recipients.txt` — the public recipient (`age1...`). NOT a secret, mode 0644.
- `~/.config/claude-state-seal/canary.age` — 32 bytes of known plaintext sealed to the same recipient. Used to validate a passphrase in milliseconds instead of failing halfway through a 45 MB archive.

SEAL PATH (every launch, silent): reads only `recipients.txt`. No passphrase exists in the process, no prompt, nothing to type, nothing in argv, nothing in `/proc/*/environ`. This is why the design imposes no launch-time friction.

UNSEAL PATH (rare, explicit): `ccy --unseal <session-id>` or `ccy-seal --unseal`. The wrapper first `age -d`s `canary.age` — age prompts for the passphrase itself, reading silently from `/dev/tty`, never echoing, never taking it from argv or env, with its own bounded 3 attempts. Our layer wraps that: pre-checks the identity exists and is non-empty; on a non-zero exit prints "Cancelled — no changes made. Nothing was decrypted." and exits 1 (clean EOF/Ctrl-D handling); on canary success proceeds to bulk-decrypt into `.claude/ccy/projects/...` at 0600 via `umask 077`. Because the passphrase is consumed by `age` on a tty rather than captured by us, there is no variable in our shell holding it, nothing to `unset`, and nothing to leak into a transcript. That is strictly safer than reading it ourselves and is why the design does not hand-roll `read -rs`.

Passing a key INTO the container: never happens. There is no `-e` for it — which sidesteps the self-defeating loop where a key in the container environment gets dumped into the very transcript it protects.

Launch-flow integration (the brief's requirement): immediately after `discover_and_select_ssh_keys "ccy"` at `claude-yolo:870`, a `report_state_hygiene()` block prints a non-blocking status line ("state: 0 world-readable, 12 sessions sealed, 2 live, retention 7d"). If `--unseal` was passed, the unseal ceremony runs there, in the same visual block, in the same style. An opt-in `CCY_SEAL_PROMPT_AT_LAUNCH=1` in `.claude/ccy/ccy.env` makes the prompt appear on every launch for users who want the ritual — off by default, because a passphrase you type 40 times a day is a passphrase you shorten.

### IaC shape

NEW play (core, not optional — a security control that is opt-in is a control that is off):
- `playbooks/imports/play-claude-state-hygiene.yml`, `scope: general`, no scope guard. Installs `age` (`age-1.3.1-4.fc44`, plain `package:`); creates `~/.config/claude-state-seal/`; runs `age-keygen` with `creates:` + a `stat`/`assert` non-empty gate; seals the canary; deploys `ccy-seal`; deploys managed-settings; fixes modes on `~/.claude` and every `.claude/ccy` under `~/Projects` via a `find`-driven loop; writes `CACHEDIR.TAG`; installs and enables the user timer.
- `playbooks/playbook-main.yml` — `- import_playbook: imports/play-claude-state-hygiene.yml` inserted after line 36 (after `play-claude-code.yml`), because it must run once both Claude Code installs exist.

NEW files:
- `files/home/.local/bin/ccy-seal` — the sealer/unsealer/auditor. Interactive-script rules apply (`--help` parsed before any environment resolution; `-y`; `--dry-run`; friendly errors; stderr for chatter, stdout is the machine-readable report). Linted by the existing `qa-bash.bash` discovery.
- `files/var/local/claude-yolo/lib/state-hygiene.bash` — sourced by `claude-yolo`; provides `assert_state_posture()`, `repair_state_posture()`, `assert_scanner_can_see()`, `scan_live_state()`, `seal_archive()`, `report_state_hygiene()`. Deployed by the EXISTING lib loop at `play-claude-yolo.yml:249` — add one list item, no new task.
- `files/etc/claude-code/managed-settings.json.j2` — templated so `cleanupPeriodDays` is a var (`claude_state_retention_days: 7` in `vars/`).
- `files/home/.config/systemd/user/claude-state-seal.service` + `.timer`.
- `.claude/hooks/handlers/pre_tool_use/credential_read_blocker.py` + `.claude/hooks/handlers/pre_tool_use/tests/test_credential_read_blocker.py` (test written FIRST — the `tdd_enforcement` handler blocks the source file otherwise).
- `docs/claude-state-hygiene.md`, indexed in `docs/README.md`.

MODIFIED:
- `files/var/local/claude-yolo/claude-yolo` — source the new lib; call the preflight before `podman run`; add `--unseal`, `--seal-now`, `--state-report` flags; add `repair_state_posture` to `cleanup()` at :1700 so an interrupted session cannot leave loose modes. **CCY_VERSION 3.30.2 → 3.31.0** (new feature, backward compatible) with the comment updated per `CLAUDE/ContainerRules.md`, and `REQUIRED_CONTAINER_VERSION 2.25 → 2.26`.
- `files/var/local/claude-yolo/entrypoint.sh` — `umask 077` before the state symlink at :185; write `CACHEDIR.TAG`; assert `/etc/claude-code/managed-settings.json` is present and parses, failing the container start if not.
- `files/var/local/claude-yolo/Dockerfile` — `COPY` the rendered managed-settings into the image (host `/etc/claude-code` cannot be bind-mounted into `container_t` under SELinux enforcing without relabelling a system directory, which podman's own docs warn against); bump the version label to 2.26. The existing "Calculate Dockerfile Hash for Build" task at `play-claude-yolo.yml:352` picks the change up and rebuilds automatically.
- `playbooks/imports/play-claude-code.yml` — `umask 077` in the `cc` wrapper installed at :118.
- `.gitignore` — `.claude/ccy/.sealed/` is already covered by the existing `.claude/ccy/*` rule; `CACHEDIR.TAG` likewise. No change needed, verified.

QA: `./scripts/qa-all.bash` (new bash + Ansible + the Python handler), `./scripts/qa-helper-tests.bash` is not applicable, and the handler test runs under the daemon's own suite. Plan-local `acceptance.bash` under `CLAUDE/Plan/00098-encrypted-claude-transcripts-at-rest/` proves: 0 other-readable files after deploy, a planted canary secret is found by the scanner, a sealed session round-trips, and a wrong passphrase fails against the canary in under a second.

### CCY launch UX

Normal launch (the 99% case) — no new prompt, one new informational block printed right after the existing SSH-key selection:

    ════════════════════════════════════════════════════════════════
    Claude state hygiene
    ════════════════════════════════════════════════════════════════
      posture     0 group/other-readable files (repaired 3)
      retention   7 days (pinned, managed-settings)
      archive     14 sessions sealed · 2 live · 0 unsealed leftovers
      backup      CACHEDIR.TAG present
      scan        clean (scanner self-test passed)
    ════════════════════════════════════════════════════════════════

Resuming a sealed session — the only place a passphrase appears:

    $ ccy --unseal 5cdaf206
    Sealed session 5cdaf206 (4.4 MB, sealed 2026-07-21).
    Enter seal passphrase: ▮            <- age prompts on /dev/tty, silent, 3 attempts
    ✓ passphrase verified (canary)
    ✓ unsealed 5cdaf206 → .claude/ccy/projects/-workspace/ (0600)
    Note: this session is now plaintext until the next launch re-seals it.
    [normal CCY launch continues]

Wrong passphrase, third attempt:

    ✗ Passphrase incorrect (canary failed).
      Giving up after 3 attempts. Nothing was decrypted, nothing changed.
      Retry:  ccy --unseal 5cdaf206

Ctrl-D at the prompt:

    Cancelled — no input. Nothing was decrypted.

Preflight refusing to launch (a control is not verifiably active):

    ✗ CCY REFUSES TO LAUNCH — state hygiene guard failed
      /etc/claude-code/managed-settings.json is missing.
      Retention is therefore unpinned and credential deny-rules are absent.
      Fix (on the HOST):
        ansible-playbook playbooks/imports/play-claude-state-hygiene.yml
      This is not skippable. Do not install the file by hand.

Other flags: `ccy --state-report` (audit, no launch), `ccy --seal-now <session>` (seal one session immediately), `ccy-seal --dry-run` (what would be sealed).

### Fail-fast story

This is where Angle C has to be precise, because its live path IS plaintext — so "never silently write plaintext" cannot be the invariant. The invariant is:

  **No CCY session starts unless every control that is supposed to be active is verifiably active, and every plaintext file that is supposed to be sealed is sealed.**

`assert_state_posture()` runs before `podman run` and exits 1 — no launch, no fallback, no warn-and-continue — on ANY of:
1. `age` binary absent → names `play-claude-state-hygiene.yml`, forbids manual install.
2. `~/.config/claude-state-seal/recipients.txt` absent, empty, or containing no `age1…` line → refuse. (Zero-byte files are explicitly rejected, not treated as present, per the repo's `ansible.builtin.password` gotcha.)
3. `canary.age` absent or not decryptable-as-a-file → the seal key is unusable, so sealing would produce archives nobody can open. Refuse.
4. `/etc/claude-code/managed-settings.json` missing or failing `jq -e '.permissions.deny and .cleanupPeriodDays'` → retention is unpinned and deny-rules are absent. Refuse.
5. Any file under `.claude/ccy` still group/other-readable AFTER the repair pass → the repair failed; refuse rather than launch with the hole open.
6. A `.sealed/x.age` whose plaintext twin still exists → a previous seal died mid-flight. Refuse; print the exact reconciliation command.
7. `seal_archive()` returning non-zero for even one file → refuse. There is no "sealed 11 of 12, carrying on".
8. The credential scan finding a live-key-shaped string in the remaining plaintext → refuse, name the file, offer `--seal-now` or `--purge-state`.

The scanner is itself fail-fast, and this is the subtle part. A scanner that cannot see returns "clean" — indistinguishable from safety. So `assert_scanner_can_see()` runs FIRST: it writes a temp file inside `.claude/ccy` containing a synthetic canary token, runs the real scan function against it, and asserts the token is found. If the self-test fails — because someone reintroduced the ugrep function, because `--ignore-files` swallowed the tree, because a pattern regressed — CCY refuses to launch and says "the credential scanner cannot see its own canary; a clean result would be a lie." A clean scan is only trusted when the scanner has just proved it can detect.

Disabling the scan is possible (`CCY_STATE_SCAN=off` in `ccy.env`) but never silent: the launch banner prints a red `scan DISABLED by ccy.env` line on every single launch, so the degraded state is impossible to forget.

Nothing here uses `|| true`, `2>/dev/null`, `failed_when: false`, or a `debug` warn-and-continue. Every probe is the sanctioned probe-then-check shape: capture output and exit status into variables, then explicitly test them.

### Failure modes

- Scanner false positive bricks the launch: a high-entropy string in a transcript that is not a credential (a git SHA run, a base64 test fixture, a UUID cluster) trips the pre-launch credential scan and refuses to start. Bounded by using only live-key-shaped patterns with entropy tails — never the `password`/`.env`/`passphrase` prose that appears 2,360/263/1,978 times in the current corpus — plus a named `ccy --seal-now <file>` escape printed in the failure message.
- The ugrep trap: `grep` inside the container is a shell function wrapping Claude Code's bundled ugrep with `--ignore-files`, which honours `.gitignore`; since `.claude/ccy/.gitignore` is `*`, any recursive grep of the state tree returns ZERO hits with exit status 0. A scanner written naively reports 'clean' forever. Handled by `assert_scanner_can_see()` (below) and by using `command grep` exclusively.
- Half-sealed state: the process dies between writing `<file>.age` and unlinking the plaintext, leaving both. Detected as a hard error on the next launch (plaintext twin of a sealed file present) rather than silently re-sealing or silently deleting.
- Seal races a live session: a session the user is about to `--resume` gets sealed because the N-most-recent heuristic misjudges it. Mitigated by sealing on mtime AND by never sealing anything referenced in `sessions/` (Claude Code's liveness files) or newer than the retention floor.
- Key loss: the age identity passphrase is forgotten, or `identity.age` is deleted. All sealed archives become permanently unreadable.
- Mode repair fights another process: `repair_state_posture()` chmods a file Claude Code is mid-write on. Harmless (chmod does not disturb an open fd) but noted because the repair also runs in the EXIT trap.
- Desktop timer silently stops: `claude-state-seal.timer` gets masked or the user session has no lingering, so `~/.claude` stops being sealed while CCY keeps reporting itself healthy. The CCY preflight cross-checks the timer's `ActiveState` and fails the launch if it is not active — the desktop store's health is enforced from the container launcher, which is the only path the user reliably traverses.
- `age` missing after a distro upgrade or a partial play run: every seal becomes a no-op. Hard failure at preflight, naming the playbook.
- `claude project purge` semantics: its `--yes` non-interactivity is documented but unverified here, so purge is NOT wired into any automatic path — it is a manual `ccy --purge-state` only, pending a host triage probe.
- The live session's own transcript is plaintext for the whole session. This is not a bug in the design, it is the design's stated boundary — and it is equally true of every launch-time-key scheme including a FUSE mount.

### Threats covered

- Another local (non-root) user reading CCY/desktop state. MEASURED and currently open: 879 of 978 files under `.claude/ccy` are other-readable today, including 344 verbatim pre-edit file bodies (7.7 MB) in `file-history/` at mode 644 inside a 755 directory. `umask 077` + the mode reconciliation takes this to 0. This is the single largest real, present exposure and no encryption scheme addresses it — a FUSE mount with default perms leaves the same 755 dirs.
- Backup / cloud-sync / rsync exfiltration of the project working tree. `.gitignore` is a git construct and does nothing for restic, borg, rclone, Syncthing or tar; `.claude/ccy` sits inside the tree by design. CACHEDIR.TAG excludes it from every backup tool that honours the standard, and whatever does get copied is 87% sealed ciphertext.
- Cold read of the archive by anyone with filesystem access while no session is live — everything outside the retention floor and the N most recent sessions is `age`-sealed to a recipient whose identity is passphrase-protected.
- The retention window itself: 30 days → 7, pinned in managed scope so a malformed user settings file cannot silently pause the sweep (the `settings_invalid_key_set` degradation, which fails toward 'keep forever').
- Future secrets entering the transcript at all: managed-scope `permissions.deny` on `.env`, `*.pem`, `id_*`, `vault-pass.secret`, `.credentials*`, backed by a repo-owned PreToolUse handler. Managed scope cannot be overridden by a poisoned project-level settings file.
- Stolen laptop, powered off — already covered by the existing LUKS FDE on `/dev/mapper/luks-…`; this design does not claim credit for it.
- Half of the stolen-suspended-laptop case: keys in RAM expose the live plaintext, but the sealed archive stays sealed because the age identity is not loaded (there is no launch-time unlock).
- The desktop `~/.claude/projects/` store — covered by the same umask, mode fix, CACHEDIR.TAG, managed-settings and a daily seal timer. Not an afterthought: it is the store with no container boundary at all.

### Threats NOT covered

- Code execution as the user while a session is live — an infostealer, a malicious npm postinstall, a hostile MCP server. It reads the live plaintext transcript exactly as Claude Code does. This is NOT an Angle C weakness: any design whose key is supplied at launch has the key loaded precisely when this attacker runs. Angles A and B are identical here and should not be allowed to claim otherwise.
- A prompt-injected Claude session reading its own state directory. The agent is inside the trust boundary by construction; it holds the decrypted view whatever the storage layer does.
- The live session's transcript, for the duration of that session. Plaintext by design so that `transcript_reader.load_tail()` and `/resume` keep working. If the user needs zero plaintext for one sensitive session, the honest answer is the upstream kill switch: a `ccy --ephemeral` flag exporting `CLAUDE_CODE_SKIP_PROMPT_HISTORY=1`, which writes nothing at all — at the cost of `/resume`, up-arrow history, and the hooks daemon's Stop handlers.
- Root on the same box. Reads the identity, the passphrase from process memory, or the plaintext directly.
- Suspended laptop for the live/recent portion — the correct fix is at the LUKS/systemd suspend layer for the whole disk (which also protects `~/.ssh`, browser tokens and `.env` files that this design never touches), not an application-layer scheme for one directory.
- Anything already exfiltrated. This is prospective protection; the 27 `$ANSIBLE_VAULT` blobs across 7 historical transcripts prove the capture mechanism works end to end, and past copies are past copies.
- Server-side retention of the conversation by Anthropic — orthogonal, and arguably the larger exposure.
- `/tmp` residue: `CLAUDE_CODE_TMPDIR` defaults outside the state directory and was not probed. If Claude Code spills anything sensitive there, no layer here catches it. Listed as an open question, not a covered threat.

### Effort

medium

### Risks

- SECURITY THEATRE IN REVERSE — the honest headline risk of the whole angle: this design does not encrypt the live transcript, so a user who believes 'my transcripts are encrypted now' will be wrong for the session they are currently in. The docs and the launch banner must state the boundary in plain words ('live session: plaintext; archive: sealed'), and the banner deliberately says '2 live' rather than hiding it.
- The pre-launch credential scan is the sharpest edge. A false positive blocks a launch — the one path every session traverses. Mitigated by high-confidence patterns only, an actionable escape command in the failure text, and the loud-but-possible `CCY_STATE_SCAN=off`. Still the most likely source of user pain, and the first thing to tune after deployment.
- Adding ANY gate to the CCY launch path increases the chance of a bricked launch. Countermeasure: every guard failure prints the exact playbook to run, and none of the guards depend on the network, the container, or a running daemon.
- Key loss destroys sealed archives — bounded, but real (see openQuestions for the escrow decision).
- `umask 077` in entrypoint.sh is a behaviour change for every file the container writes. Low risk (nothing in CCY relies on group-readable state) but it touches the container image, so it needs the container version bump and a rebuild — and a mis-ordered umask (set after something already wrote) would leave a partial fix that looks complete.
- Managed settings baked into the image pins `cleanupPeriodDays` to build time. The existing Dockerfile-hash rebuild trigger handles it, but a user who edits the host file expecting the container to follow will be surprised. Documented explicitly.
- The `permissions.deny` rules will occasionally block legitimate work (reading a `.env.example`, a `*.pem` fixture). Patterns must be tight, and the deny message must name how to proceed.
- Scope creep into Angle A: once `ccy-seal` exists, it is tempting to extend it to live files. It must not be — that is precisely where the hooks daemon's byte-offset reads break.
- Plan 00098 is already In Progress with a two-store split assumption; this design should be appended to that plan's JOURNAL/ rather than opening a new plan (number collision risk).

### Open questions

- ESCROW OR NOT? If the passphrase is forgotten, all sealed transcripts are gone. What is actually lost is: old conversation history and old pre-edit file snapshots — never the repo, never the plans (tracked in git), never the current session, never any credential the user does not already hold elsewhere. My position is that this IS acceptable and that escrow adds a second copy of the key to protect data whose loss is survivable. But it is the user's call: the alternative is a second `age -R` recipient held offline (a YubiKey via age-plugin-yubikey, or a printed identity), which costs one line in recipients.txt and no code. Decide before the first seal runs, because retro-fitting a recipient requires unsealing and re-sealing everything.
- Does `permissions.deny` cover `@file` inlining? Every redaction-hook project documents `@file` as the universal bypass because it never invokes the Read tool. If the deny rule is enforced at the permission layer rather than the tool layer it may cover it; if not, L1 has a known hole and the PreToolUse handler cannot close it either. Needs an empirical test — plant an `@`-mention of a deny-listed path and check the transcript.
- Is `claude project purge --yes` genuinely non-interactive, and does it delete or merely unlink? Decides whether purge can ever be wired into the exit trap. Until answered, purge stays manual.
- What is the actual size and secret content of the HOST `~/.claude/projects/<slug>/` store? Unmeasured from inside the container — this is the store with no container boundary, so it may well be the larger exposure. Needs a host-run `triage.bash` probe before the timer's retention floor is chosen.
- Is the host SELinux Enforcing or Permissive? Evidence from inside (container_t writing user_home_t with no `:Z`) points to Permissive. This design avoids every SELinux-sensitive mechanism (no FUSE, no `/dev/fuse`, no system-dir bind), so the answer does not change the design — but it should be established, because if the host IS enforcing then the current CCY mount is behaving unexpectedly and that is worth knowing independently.
- Does Claude Code write anything sensitive outside the config dir (`CLAUDE_CODE_TMPDIR`, a project-local `tmp/attachments`)? If so the boundary is incomplete and needs one more sweep path.
- Should `CLAUDE_CONFIG_DIR` replace the `/root/.claude` symlink at entrypoint.sh:195? It is the officially documented mechanism and the symlink is not. Not required by this design, but it is a free correctness win while the file is open — and it would also relocate the Linux credentials file into the same governed tree.
- What is the right retention floor — 7 days, or 3? And should the seal keep N most-recent sessions or N days? A `--resume` of a 10-day-old session becomes a passphrase ceremony at 7 days. Needs one week of observed resume behaviour to answer honestly rather than guessed at now.

## CryptView — host-side gocryptfs volume bind-mounted as the Claude state directory

**A gocryptfs ciphertext dir lives in the repo; the CCY launcher prompts for one passphrase, mounts a decrypted view under $XDG_RUNTIME_DIR on the HOST, and bind-mounts that mount root directly at /root/.claude in the container — so Claude Code and the hooks daemon see an ordinary POSIX directory, plaintext never touches persistent storage, and ciphertext is what lands in the repo tree, in backups, and in any rsync/Dropbox copy.**

### Architecture

STORAGE SPLIT (the key move: the plaintext view lives OUTSIDE the bind-mount source).

- Ciphertext, per project: `$PWD/.claude/ccy/crypt/` — an ordinary btrfs dir in the repo tree, already covered by `.claude/ccy/.gitignore`'s bare `*`. Holds `gocryptfs.conf` (passphrase-wrapped master key) + the encrypted tree. Only persistent representation of session state.
- Plaintext view, per project: `$XDG_RUNTIME_DIR/ccy-plain/<h>`, `<h>` = first 16 hex of sha256(realpath $PWD), mode 0700. XDG_RUNTIME_DIR is tmpfs, so a stale mount leaves nothing on disk and dies at logout.
- `.claude/ccy/` is NOT overlaid. The four TRACKED files (ccy.env, Dockerfile, claude-supervise.py, .gitignore) and .last-launch.conf stay in place, readable by the launcher before any key exists. Dissolves the bootstrap chicken-and-egg; `git status` stays clean.

WHY THIS SHAPE SOLVES THE PODMAN PROBLEM. Podman `-v` is a non-recursive `bind` with `private` propagation, so a FUSE submount INSIDE $PWD is silently dropped and the container writes plaintext into the underlying dir — precisely the silent-plaintext failure the fail-fast rule forbids. Putting the plaintext mount root outside $PWD and giving it its OWN `-v` means podman binds the gocryptfs mount ROOT directly: no submount, propagation irrelevant. Ordering is trivially satisfied (launcher mounts, then runs podman).

FUSE + USERNS. Rootless podman puts the container in a different user namespace; since ~4.18 `fuse_allow_current_process()` denies access when `current_user_ns() != fc->user_ns` unless allow_other is set (confirmed by podman maintainer, podman#14488). So `-allow_other` is MANDATORY, which requires `user_allow_other` in /etc/fuse.conf. Exposure is contained by mounting with `-ko noexec,nosuid,nodev`, mountpoint 0700, parent `ccy-plain/` 0700 — allow_other bypasses FUSE's owner check but not DAC.

SELINUX (enforcing). FUSE gets `fusefs_t` from genfscon and cannot be relabelled (`:z`/`:Z` MUST NOT be used on this mount; gocryptfs `-context` needs root). Access is granted by the persistent boolean `virt_use_fusefs` (podman's documented remedy for sshfs/FUSE-backed rootless volumes), set via `ansible.posix.seboolean` — the same module already used in play-ftp-camera.yml. Because the boolean name is the one thing I could not verify on this host, the launcher does not trust it: it runs a 2-second throwaway probe container that writes through the mount, and hard-fails naming the playbook if denied.

CONTAINER SIDE. entrypoint.sh's `ln -sf /workspace/.claude/ccy /root/.claude` (entrypoint.sh:195) is deleted. `/root/.claude` becomes the second bind mount. The `rm -rf /root/.claude` branch (entrypoint.sh:189-194) MUST go — with a mount there it would delete live state. It is replaced by three assertions (device-number differs from /root, sentinel token matches, write probe succeeds), any of which exits 1 before `exec claude`.

CONCURRENCY. Six CCY sessions on one project share one mount. `podman run` gains `--label ccy.cryptvol=<h>`; the launcher mounts only if `findmnt` says nothing is there, and prompts only if it mounts. Cleanup unmounts only when `podman ps -q --filter label=ccy.cryptvol=<h>` returns zero rows; a non-zero exit from that query is a loud error that deliberately does NOT unmount (unmounting under a live session is the worse failure).

WHY THIS BEATS FILE-LEVEL / SEAL-AT-EXIT. The hooks daemon's `core/transcript_reader.py` does byte-offset `open('rb')` + seek (`load_tail`, `read_incremental`) across 38 files, and `/resume` needs the transcript live. A transparent POSIX view keeps all of that working unchanged, with zero changes to Claude Code. And the state is encrypted continuously, not merely "at session end".

FILENAME ENCRYPTION. Default EME names + `-longnames` (on by default) — gocryptfs hashes over-long encrypted names to `gocryptfs.longname.<44>`, which is exactly why it does not hit the eCryptfs ENAMETOOLONG class that silently broke Claude Code in issue #80753. `acceptance.bash` proves it by creating a 250-char path inside the mount. `-plaintextnames` is the documented fallback if that ever regresses (costs: session UUIDs and project slug leak).

### Key handling

ONE passphrase, N volumes (per-project CCY volume + the desktop volume). Typed once per launch, or not at all when the volume is already mounted by a sibling session.

PROMPT. New `files/var/local/claude-yolo/lib/crypt-handling.bash`, modelled on lib/ssh-handling.bash's `discover_and_select_ssh_keys()` position in the flow but NOT its `read -rp`. Per CLAUDE/InteractiveScripts.md rule 07: `read -rs` from /dev/tty, prompt and all diagnostics to stderr, value on stdout, non-zero return on EOF. MAX_TRIES=3; a wrong passphrase (gocryptfs exit 12) re-prompts with "That passphrase did not unlock the store — try again"; exhaustion exits 1 with "Giving up after 3 attempts. No container started."; Ctrl-D exits 1 with "Cancelled — no input. Nothing was mounted."

CUSTODY (never argv, never env, never disk).
1. Launcher reads it into a shell variable.
2. `printf '%s' "$pass" | keyctl padd user ccy:passphrase @s` — `padd` takes the payload on STDIN, so it never appears in /proc/*/cmdline. `unset pass` immediately.
3. `gocryptfs -q -allow_other -ko noexec,nosuid,nodev -extpass /usr/local/lib/claude-crypt/extpass-keyring <cipher> <plain>`. The helper is 4 lines: `keyctl print "$(keyctl search @s user ccy:passphrase)"` (gocryptfs strips the trailing newline). Only the key *description* is ever in argv.
4. After the last volume mounts: `keyctl unlink` the key. Passphrase lifetime is ~2 seconds of non-swappable kernel memory.
5. The derived master key then lives only in the gocryptfs process. It is passed to podman by NOTHING — the container never sees a key, only a mounted view.

Explicitly rejected: `-e CRYPT_PASS` (podman persists `-e KEY=value` into on-disk container config, and Claude Code dumps its own environment into the very transcripts being protected — a self-defeating loop); `-passfile` (writes to disk); inline `-extpass "echo $pass"` (argv).

FIRST-RUN CEREMONY (`gocryptfs -init`). Passphrase entered twice with mismatch re-prompt; gocryptfs prints the master key, which the launcher displays ONCE on stderr with "WRITE THIS DOWN — it is the ONLY recovery path if you forget the passphrase" and requires the literal string `I HAVE SAVED IT` to continue (bounded, EOF aborts). The master key is never logged, never written to a file, never passed to podman.

ROTATION. `claude-yolo --crypt-passwd` wraps `gocryptfs -passwd`, which re-wraps the master key in gocryptfs.conf without re-encrypting 62 MB.

PERSISTED HANDLE. `.last-launch.conf` (mode 0600) gains only `LAST_CRYPT_VOL="<h>"` — a non-secret path handle. No key material, no salt, nothing derived. The file is `source`d, so it must stay non-secret by construction.

WIPE. Passphrase: `unset` + keyring unlink. Plaintext: `fusermount3 -u` on the last container exit, plus `gocryptfs -idle 30m` as the SIGKILL/power-loss backstop, plus XDG_RUNTIME_DIR teardown at logout. There is no plaintext to shred because none was ever written to a block device.

### IaC shape

NEW `playbooks/imports/play-claude-crypt.yml` (scope: general, executable + `#!/usr/bin/env ansible-playbook`) — shared host prerequisites for BOTH Claude Code paths. Justified as a new play rather than an edit because its lifecycle is genuinely independent (it must run before both play-claude-code.yml and play-claude-yolo.yml, and it owns root-level system state neither of those should touch). Tasks:
1. `ansible.builtin.package: name=[gocryptfs, fuse3, keyutils] state=present` (all three are in Fedora 44: gocryptfs-2.6.1-5.fc44, keyutils-1.6.3-7.fc44).
2. `ansible.builtin.lineinfile: path=/etc/fuse.conf line=user_allow_other regexp='^#?\s*user_allow_other' become=true` — required for rootless podman to traverse the FUSE mount.
3. `ansible.posix.seboolean: name=virt_use_fusefs state=yes persistent=yes become=true` (same module already used at play-ftp-camera.yml:204).
4. `ansible.builtin.copy` of `files/usr/local/lib/claude-crypt/claude-crypt.bash` (sourced library: cc_prompt_passphrase, cc_keyring_stash, cc_init, cc_mount, cc_assert_mounted, cc_fsck_if_dirty, cc_unmount_if_last) and `files/usr/local/lib/claude-crypt/extpass-keyring`, mode 0755, owner root.
5. `ansible.builtin.file` create `/home/{{ user_login }}/.local/share/claude-code-crypt` (desktop ciphertext) mode 0700.
6. `assert` that `getsebool virt_use_fusefs` reports `on` after the change, so a renamed/absent boolean fails the play loudly instead of leaving a broken launch path.

MODIFY `playbooks/playbook-main.yml` — import play-claude-crypt.yml immediately after play-podman.yml and before play-claude-code.yml / play-claude-yolo.yml.

MODIFY `playbooks/imports/play-claude-yolo.yml` — add `files/var/local/claude-yolo/lib/crypt-handling.bash` to the existing "Install Shared Helper Libraries" task list (line ~249); add a preflight assert that `/usr/local/lib/claude-crypt/claude-crypt.bash` exists (mirrors play-claude-code.yml:20's existing "Preflight — Assert ccy Lib Is Deployed" pattern).

MODIFY `files/var/local/claude-yolo/claude-yolo` — CCY_VERSION 3.30.2 → **3.31.0** (minor: new feature), REQUIRED_CONTAINER_VERSION 2.25 → **2.26**. Source crypt-handling.bash; call the mount flow next to the existing SSH-key prompt (claude-yolo:870); extend DOCKER_MOUNTS (claude-yolo:1781-1784) with `-v "$PLAIN:/root/.claude"` and the run invocation with `--label ccy.cryptvol=<h>` and `-e CCY_MOUNT_TOKEN` (by NAME, per the BSH-09 precedent at claude-yolo:2756-2762); extend `cleanup()` (claude-yolo:1700, `trap cleanup EXIT` at :1721) with cc_unmount_if_last; add `--crypt-passwd` and `--crypt-fsck` subcommands.

MODIFY `files/var/local/claude-yolo/entrypoint.sh` — delete lines 183-195 (mkdir + rm -rf + `ln -sf`), replace with the three mount assertions. Dockerfile version label → 2.26.

MODIFY `files/var/local/claude-yolo/lib/common.bash` — extend `check_ccy_gitignore_safety()` (:144) to also assert `.claude/ccy/crypt/gocryptfs.conf` is git-ignored, and to refuse launch if any file under `crypt/` is tracked.

MODIFY `files/var/local/claude-code/cc` + `playbooks/imports/play-claude-code.yml` — desktop guard (see below). Add a `become: true` task running `chattr +i` on the empty `~/.claude` mountpoint so a bypassed/unmounted invocation gets EPERM instead of writing plaintext.

MODIFY `.claude/ccy/.gitignore` — add an explicit `crypt/` line for readability (the bare `*` already covers it; explicitness is what the QA/safety check asserts against).

PLAN-LOCAL (Plan 00098, already In Progress): `CLAUDE/Plan/00098-encrypted-claude-transcripts-at-rest/{triage.bash,deploy.bash,acceptance.bash}`, logs to `<plan>/logs/` resolved from BASH_SOURCE[0]. triage.bash probes `getenforce`, `sestatus`, `getsebool -a` grep fuse, `ausearch -m AVC -ts today`, `grep user_allow_other /etc/fuse.conf`, `rpm -q gocryptfs keyutils`, `findmnt -no FSTYPE,PROPAGATION`, plus whether any backup/sync tool (rclone/syncthing/restic/Dropbox) covers the project dir — that last one largely decides whether this feature is worth building at all.

DOCS: new section in `docs/ccy.md` (passphrase ceremony, master-key backup, recovery, `--crypt-passwd`, what is lost on key loss).

QA: `./scripts/qa-all.bash` covers the new bash and the new playbook (`qa-ansible.bash` + `--syntax-check`). No `helpers/` Python needed — every playbook task is trivial/declarative.

### CCY launch UX

Unchanged up to the SSH-key prompt, then one new step immediately after it (same screen, same style, `read -rs` instead of `read -rp`):

  SSH keys selected: 1 key
  
  Encrypted Claude state — unlocking .claude/ccy/crypt
  Passphrase:                       <- typed, never echoed, stderr prompt

  Unlocking...  OK  (mounted at /run/user/1000/ccy-plain/9f2c1ab4e0d73c85)
  Verifying container can read the decrypted view... OK
  Starting Claude Code...

FIRST RUN in a project (no gocryptfs.conf yet):

  No encrypted Claude state store exists for this project.
  Creating one at .claude/ccy/crypt (62M of existing plaintext state will be migrated).
  
  New passphrase:                   <- read -rs
  Confirm passphrase:               <- mismatch => "Those didn't match — let's try again." and re-prompt, 3 tries
  
  Initialising... OK
  
  ================= MASTER KEY — WRITE THIS DOWN =================
   1a2b3c4d-...-...  (8 groups)
   This is the ONLY way to recover your Claude history if you
   forget the passphrase. Store it in your password manager NOW.
  ================================================================
  Type  I HAVE SAVED IT  to continue: 

  Migrating 62M ... 969 files copied, verified, originals removed. OK

SECOND CONCURRENT SESSION on the same project: no prompt at all. The launcher sees the volume already mounted, verifies the sentinel, and goes straight to launch.

WRONG PASSPHRASE: "That passphrase did not unlock the store — try again. (attempt 2 of 3)". After 3: "Giving up after 3 attempts. No container started." exit 1.

Ctrl-D at any prompt: "Cancelled — no input. Nothing was mounted, nothing changed." exit 1.

NON-INTERACTIVE / no TTY: fails immediately with "No TTY available to read the store passphrase. Run ccy from a terminal, or unlock the volume first with: claude-yolo --crypt-unlock". Never hangs on an unanswerable prompt.

EXIT: the last container to leave unmounts. `ccy --crypt-status` prints mount state and the live-session count.

### Fail-fast story

There is no code path that writes Claude state to an unencrypted location. Eight sequential gates, every one of them a hard `exit 1` with no fallback, no `|| true`, no `2>/dev/null`, no skip-and-warn:

G0 (playbook) — `assert` that `getsebool virt_use_fusefs` is on after the seboolean task; a renamed/absent boolean fails the deploy, not the launch.
G1 (launcher) — gocryptfs, keyutils, fusermount3 present and `user_allow_other` set, else abort naming the playbook. Never self-installs.
G2 — `gocryptfs -fsck` when the `.ccy-dirty` marker is present; exit 26 aborts.
G3 — mount exit status non-zero aborts. There is no "continue without encryption" branch anywhere in the code; the plaintext path simply does not exist as a variable.
G4 (post-mount, host) — `findmnt -no FSTYPE "$PLAIN"` must equal `fuse.gocryptfs`. Then a CANARY: write 32 random bytes into the mount, `command grep -r` those bytes across the ciphertext tree (with `command grep`, never the ugrep shell function that honours .gitignore and silently returns zero hits), and ABORT if they are findable in cleartext. That single check is what proves encryption is actually happening rather than assumed.
G5 (pre-launch, SELinux) — a 2-second throwaway `podman run --rm` with the same mount writes and reads a file through the view. Denial aborts with the ausearch excerpt.
G6 (container) — entrypoint.sh asserts (a) `stat -c %d /root/.claude` differs from `/root`, (b) `/root/.claude/.ccy-mount-token` equals `$CCY_MOUNT_TOKEN` (passed by env NAME, per BSH-09), (c) a write probe succeeds. Any failure exits 1 before `exec claude`. The old `rm -rf /root/.claude` branch is deleted outright — with a mount there it would destroy live state.
G7 (desktop) — the `cc` wrapper mounts-or-dies before exec'ing claude; the underlying `~/.claude` is empty and `chattr +i`, so even a bypassed invocation gets EPERM rather than writing plaintext.

The design's honest weak seam is G6: if podman silently binds the wrong thing, only the sentinel token catches it. That is why the token is a per-launch random nonce compared inside the container, not a mere `[ -d ]` test — a stale or empty directory cannot forge it.

### Failure modes

- gocryptfs / keyutils / fuse3 not installed on the host — launcher aborts naming `ansible-playbook playbooks/imports/play-claude-crypt.yml`; never installs anything itself (CLAUDE.md 'Missing Dependencies' rule)
- `user_allow_other` missing from /etc/fuse.conf — gocryptfs refuses `-allow_other` and the mount fails; launcher aborts naming the same playbook. NEVER retried without -allow_other, because that silently produces a mount podman cannot read.
- SELinux denies container_t access to fusefs_t (boolean absent, renamed, or ineffective) — the pre-launch probe container fails; launcher aborts with the AVC excerpt from `ausearch` and the playbook to run. Documented pivot: mount inside the container (`--device /dev/fuse --cap-add SYS_ADMIN`, ciphertext on the existing bind, plus `container_use_devices`), kept fully specified but not the default because it grants new capabilities.
- Podman drops the mount (wrong path, propagation, race) so /root/.claude is an ordinary empty dir — entrypoint.sh's device-number + sentinel-token check exits 1 BEFORE claude runs. This is the single most important gate: it is the only thing standing between a mount bug and a session quietly writing 45 MB of plaintext.
- Wrong passphrase — gocryptfs exits 12, bounded re-prompt, then clean abort.
- Unclean shutdown (SIGKILL, OOM, power loss) leaves a torn 4 KiB GCM block — a `.ccy-dirty` marker written at mount and removed at clean unmount triggers `gocryptfs -fsck` on the next launch; exit 26 aborts the launch and prints the `-force-decode` recovery command. Damage is bounded to the tail ≤4 KiB of one file; earlier lines still decrypt.
- Stale mount after a SIGKILL of the launcher — `-idle 30m` self-unmounts; XDG_RUNTIME_DIR teardown at logout is the hard backstop. No plaintext ever reached a block device, so a stale mount is an access-control window, not a residue.
- Concurrent-session unmount race — refcount by `podman ps --filter label=ccy.cryptvol`; zero rows unmounts, a failed query errors loudly and deliberately does NOT unmount.
- `git clean -xfd` deletes the entire ciphertext store (it is gitignored). Documented loudly; the hooks daemon's destructive_git handler already blocks `git clean -f` for the agent, but not for the human.
- Migration interrupted mid-copy — copy-verify-then-remove, with per-file count and byte-count assertions; any failure leaves BOTH trees intact and exits non-zero.
- Filename-length regression (the eCryptfs #80753 class) — acceptance.bash creates a 250-char path in the mount every run; failure blocks the release.
- Desktop `claude` invoked by absolute path, bypassing the `cc` wrapper, while the volume is unmounted — `chattr +i` on the empty mountpoint turns this into EPERM instead of a plaintext write.

### Threats covered

- Exfiltration by copy of the project tree — rsync, tar, Dropbox, Syncthing, rclone, restic, Timeshift. `.gitignore` does nothing for any of these, and `.claude/ccy` sits inside the working tree by design, so today CCY is strictly MORE exposed than ~/.claude. After this change those tools copy ciphertext. This is the strongest justification and it should be the headline one.
- Backups and btrfs snapshots leaving the LUKS volume — same argument, and it covers the desktop store too once phase 2 lands.
- Another non-root local user browsing the tree — including the 744 mode-644 files under file-history/ (7.7 MB of verbatim pre-edit file bodies) that chmod alone would need to be re-applied to forever. Ciphertext content is unreadable regardless of mode drift.
- Code execution as the user BETWEEN sessions (no volume mounted) — the infostealer's `~/.claude` / `.claude` directory sweep finds only ciphertext. This is a real and common window: the machine is on far more hours than a CCY session runs.
- Stolen laptop SUSPENDED with no session live — LUKS keys are in RAM (worse since the Linux 6.9 suspend regression) but the gocryptfs master key is not, because `-idle 30m` has unmounted.
- Accidental sharing — tarring the project dir for a colleague, attaching it to a bug report, pushing a mirror to a non-public host.
- Plaintext residue on disk from crashes — structurally eliminated: the decrypted view exists only as a kernel FUSE translation over tmpfs-mounted mountpoints, so no plaintext block is ever written to the btrfs/LUKS volume at any point in the lifecycle.

### Threats NOT covered

- Compromise WHILE a session is live — malicious npm/pip postinstall, malicious MCP server, prompt-injected agent, or any RCE as the user. The key is loaded and the plaintext view is readable by the same UID that Claude Code uses. This is the most probable real attacker in 2026 (TrapDoor, the SEO-poisoning infostealers, Miasma's SessionStart self-injection) and at-rest encryption is a category error against it. Say this plainly; do not oversell.
- Root on the box, and anyone who can read /proc/<gocryptfs-pid>/mem. gocryptfs is not verified to mlock its master key, so that claim is deliberately not made.
- Stolen laptop POWERED OFF — LUKS FDE already covers it. This design adds exactly nothing there.
- Suspended laptop WITH a session live — the master key is in RAM alongside the LUKS key.
- Secrets entering the transcript in the first place. Encryption is downstream of the leak. The complementary controls — settings.json permission deny-rules on .env/credential reads, and `cleanupPeriodDays: 7` pinned via an Ansible-deployed `/etc/claude-code/managed-settings.json` — are one-line changes that help EVERY scenario including the live-compromise one, and should ship first or alongside. They are not competitors to this design; they are the cheap half of it.
- Server-side retention at Anthropic.
- Metadata: file sizes (±4 KiB), mtimes, directory shape and file counts leak even with EME filename encryption. An observer can tell how much you talked to Claude and when.
- Desktop ~/.claude until phase 2 is deployed — and after phase 2, any invocation of the real `claude` binary by absolute path is only guarded by the `chattr +i` EPERM, not by the wrapper.

### Effort

large

### Risks

- The SELinux boolean name is unverified. `virt_use_fusefs` is podman's documented remedy for FUSE-backed rootless volumes, but Fedora 44's container-selinux may expose it under a different name or not cover container_t. This is THE gating unknown; triage.bash must answer it on the host before a line of the playbook is trusted. Mitigated, not removed, by G0 + G5 failing loudly.
- `user_allow_other` is a host-wide widening: every local user can then traverse any allow_other FUSE mount subject to DAC. Contained by 0700 mountpoints, but it is a real, permanent, system-level side effect of a per-project feature.
- gocryptfs FUSE overhead lands exactly on this workload: many small JSONL appends (read-modify-write of a ≤4 KiB GCM block per append) and the hooks daemon's repeated `load_tail` re-reads, which gocryptfs re-decrypts rather than serving from page cache. acceptance.bash must gate on measured numbers — proposed thresholds: p95 append of a 4 KB line ≤ 2 ms, `load_tail` of a 6 MB transcript ≤ 250 ms, full `-fsck` of the store ≤ 20 s. If those are missed the design is not viable and should be said so, not tuned around.
- Crash durability regresses. Plaintext append-only JSONL degrades gracefully (lose a partial line); a torn ciphertext block loses ≤4 KiB and needs `-force-decode`. Bounded, but strictly worse than today.
- This adds a new hard dependency to the single code path every session traverses. A gocryptfs bug, a Fedora package regression, or a kernel FUSE change breaks ALL CCY sessions, not one project. The `--crypt-status` / `--crypt-fsck` subcommands and a documented `-masterkey` recovery path exist precisely because of this.
- Key loss is unrecoverable without the master key. See openQuestions for the acceptability argument.
- Security theatre risk: 'it's encrypted now' invites more secrets into context while the live-session threat — the likeliest one — is untouched. The docs section must lead with the honest scorecard, not the feature.
- `git clean -xfd` destroys the ciphertext store. So does deleting `.claude/ccy/crypt` by hand while looking for space.
- Blast radius of the entrypoint change: `claude-supervise.py` and any hooks-daemon path that assumes state lives at `/workspace/.claude/ccy` must be audited before the symlink is removed. This is the most likely source of an implementation-day surprise.
- Container version bump (2.25 → 2.26) forces an image rebuild for every user; the existing REQUIRED_CONTAINER_VERSION gate makes that loud, which is correct but disruptive.

### Open questions

- Is the host SELinux Enforcing or Permissive, and which boolean actually grants container_t → fusefs_t on Fedora 44? (`getenforce`, `sestatus`, `getsebool -a | grep -i fuse`, `ausearch -m AVC -ts today`.) The in-container evidence — container_t writing user_home_t with no `:Z` — strongly suggests Permissive, which would mean CCY's CURRENT mount would already be failing under enforcing. That is a finding in its own right and must be settled first.
- Is the project directory actually covered by any backup or sync tool? This is the scenario where the design genuinely wins. If nothing syncs or backs up the tree, the honest recommendation is to do the cheap controls (deny-rules, cleanupPeriodDays, chmod) and NOT build this.
- What are the measured numbers on this btrfs-on-LUKS host for the append + load_tail workload? Nothing in the research is a benchmark; it is all upstream discussion.
- Does `chattr +i` on an empty `~/.claude` still permit fusermount3 to mount over it as a non-root user? If not, fall back to mode 0500, and if that also blocks the mount, the desktop guard is the `cc` wrapper alone plus a documented residual risk.
- Does `claude-supervise.py` (or any hooks-daemon path) hardcode `/workspace/.claude/ccy` as the state directory? Must be grepped before entrypoint.sh:195 is removed.
- Key loss: is losing conversation history acceptable? My argued answer is YES — the encrypted store holds transcripts, history.jsonl, file-history checkpoints, plans/tasks caches and the OAuth credential, and NOTHING there is a source of truth. Source code is in git, CLAUDE/Plan and its JOURNAL are in git, ccy.env and the Dockerfile are tracked, and the OAuth login is re-establishable in 30 seconds. Claude Code's own default `cleanupPeriodDays: 30` would have deleted most of it anyway. The user should confirm they accept that framing before the ceremony is built, because the master-key-in-password-manager step is the whole mitigation.
- Should phase 1 ship CCY-only, or CCY + desktop together? Shipping CCY-only leaves the host `~/.claude/projects` corpus fully exposed and un-measured (no probe has ever seen it), which arguably makes the feature's headline claim misleading. My recommendation: measure the desktop corpus in triage.bash first, and ship both phases or neither.
- Should the cheap controls (permission deny-rules on credential reads, `cleanupPeriodDays: 7` via an Ansible-deployed `/etc/claude-code/managed-settings.json`, `chmod -R go-rwx` on the state tree) land as a prerequisite commit? I think yes — they beat this design on every threat except copy-exfiltration, they cost one playbook task each, and they make the encryption layer's honest scope much easier to state.

## SEALSTORE — tmpfs-live, age-sealed CCY session state

**Claude Code's state dir lives on a container-private tmpfs (never on disk in plaintext); a public-key sealer continuously writes age-encrypted append-segments into the repo, so the plaintext window is RAM-only and the crash window is ≤30s — and only unsealing needs the passphrase, which is why the seal step can never fail to run.**

### Architecture

THE INVERSION. entrypoint.sh:195 currently does `ln -sf /workspace/.claude/ccy /root/.claude`, so every byte lands on the bind-mounted host disk in plaintext. SEALSTORE deletes that path.

PLAINTEXT (RAM only): `podman run --tmpfs /root/.claude:rw,exec,nosuid,nodev,size=${CCY_STATE_TMPFS_SIZE:-2g},mode=0700`. A mount in the container's own mount namespace. Invisible on the host. Needs no CAP_SYS_ADMIN, no /dev/fuse, no `:Z`, no allow_other, no /etc/fuse.conf edit — so SELinux enforcing is a non-issue (crun labels tmpfs container_file_t automatically). The kernel destroys it when the `--rm` container exits, so there is structurally no "plaintext left mounted after a kill" — the failure that sinks every host-side FUSE design.

CIPHERTEXT (disk): `/workspace/.claude/ccy/sealed/`. A SUBDIRECTORY of the existing ccy dir, so already covered by the gitignore layers (.gitignore:29-31; .claude/ccy/.gitignore bare `*`) with no new rules, and the four tracked files (ccy.env, Dockerfile, claude-supervise.py, .gitignore) stay visible. We deliberately do NOT overlay a mount on .claude/ccy — that would delete those tracked files from the working tree and break the ccy.env bootstrap at entrypoint.sh:288.

ASYMMETRIC KEY — the structural choice:
  sealed/recipient.pub — age X25519 PUBLIC key, plaintext, 0644
  sealed/identity.age  — SECRET key wrapped with `age -p` (scrypt), 0600
Sealing needs ONLY the public key. Unsealing needs the passphrase. So the seal path never blocks on a human, holds no secret in RAM, and can run from a watchdog or exit trap unattended. This is what makes "guaranteed to seal" achievable.

STORE LAYOUT (log-structured, flat, SHORT names — sidesteps issue #80753 ENAMETOOLONG entirely; no filename encryption anywhere):
  sealed/recipient.pub, identity.age, RECOVERY.txt (plaintext how-to)
  sealed/manifest.json  — atomic write+rename; per-path: {relpath, gen, kind, sealed_len, seq, sha256, mtime, loaded}
  sealed/seg/<sha256(relpath)[:32]>/<gen>-<seq>.age

SEALER (`ccy-sealstore`, Python 3 stdlib + `age` subprocess, in the image at /usr/local/bin):
- `unseal` — replay manifest → write plaintext into /root/.claude on tmpfs.
- `seal --daemon --interval 30` — every cycle, stat every file under /root/.claude:
  * append-only `.jsonl`: seal bytes [sealed_len, last_newline_offset) as the next segment. Truncating at the last `\n` guarantees a replayed transcript is always line-complete — never a torn JSON record.
  * shrink, or first-4KiB hash change ⇒ file was rewritten: bump `gen`, emit a full snapshot, mark prior segments dead.
  * everything else (settings.json, .claude.json, sessions/*.key, file-history/*, paste-cache, shell-snapshots): full snapshot on content-hash change.
- `compact` — when a path exceeds 64 segments or sealed bytes > 3× live bytes, write a fresh full snapshot from tmpfs (no decryption needed — plaintext is right there), then unlink superseded segments. Also applies `CCY_SEAL_RETENTION_DAYS` (default 30) to the sealed store, so growth is bounded the way cleanupPeriodDays bounds the plaintext one.
- Paths marked `loaded:false` in the manifest (not unsealed this launch) are FROZEN: never compacted, never deleted, never re-sealed. Correctness requirement, not an optimisation.

INCREMENTAL/SELECTIVE UNSEAL: default unseals sessions with mtime inside `CCY_UNSEAL_DAYS` (14) plus always history.jsonl, settings.json, .claude.json, plugins/, sessions/. `ccy --unseal-days N` / `--unseal-all` widen it; `--resume <id>` forces that id. Keeps launch ~1s at 62MB and stops it degrading as the archive grows.

WHY /resume AND THE HOOKS DAEMON STILL WORK: during a session the state tree is ordinary plaintext files on tmpfs. `claude --resume`, `--continue`, up-arrow history, and the daemon's byte-offset reads (`core/transcript_reader.py` load_tail/read_incremental, and the 38 files that depend on it) all see exactly what they see today. This is the decisive advantage over encrypting the live file: nothing downstream changes.

LAUNCH SEQUENCE (host `claude-yolo`):
1. Preflight: `age` on host + in image; swap safety gate; sealed-store integrity.
2. If `sealed/` absent → key ceremony (see keyHandling). Then one-time import of existing plaintext .claude/ccy state, then shred it.
3. Passphrase prompt (after the SSH-key prompt) → unwrap identity into `$XDG_RUNTIME_DIR/ccy-<launchid>/identity` (host tmpfs, 0600, dir 0700).
4. `podman run … --tmpfs /root/.claude:… -v "$RUNDIR:/run/ccy-key" -e CCY_SEALED_STATE=1 …` (identity passed as a bind-mounted FILE — never argv, never `-e`, per BSH-09).
5. entrypoint: assert tmpfs; `ccy-sealstore unseal`; `shred -u /run/ccy-key/identity` (rootless maps container root→host uid 1000, so it owns the file); start `seal --daemon`; exec Claude Code.
6. entrypoint EXIT trap: final `seal --once && compact`, writing `last_seal_epoch`.
7. Host `cleanup()` trap (claude-yolo:1700): `rm -rf "$RUNDIR"`, then read `manifest.json` and compare `last_seal_epoch` to container exit time. Stale ⇒ loud non-zero warning naming the lost window. It cannot recover — the tmpfs is gone — so it reports rather than pretends.

### Key handling

WHERE IT LIVES
- Passphrase: nowhere. Never written, never in a shell variable, never in argv, never in an env var.
- Secret key: `sealed/identity.age`, scrypt-wrapped by `age -p`, mode 0600, in the gitignored store.
- Public key: `sealed/recipient.pub`, plaintext. This is all the sealer ever needs.

PROMPT (host, in the new `lib/sealed-state.bash`, called from claude-yolo right after `discover_and_select_ssh_keys()` at :870). Per CLAUDE/InteractiveScripts.md: we print our own contextual header to stderr, then invoke `age --decrypt -o "$RUNDIR/identity" "$SEALED/identity.age"`, which itself reads the passphrase with no echo directly from /dev/tty. Deliberate: the secret never transits a bash variable at all. Bounded loop, MAX_TRIES=3, friendly re-prompt on a wrong passphrase, and EOF/Ctrl-D (age exits non-zero with no read) is treated as cancelled → "No changes made." exit 1. Never a fallback to plaintext mode.

TRANSPORT TO THE CONTAINER
`$XDG_RUNTIME_DIR` is tmpfs, mode 0700, user-only — so the unwrapped identity exists only in host RAM. `-v "$XDG_RUNTIME_DIR/ccy-<launchid>:/run/ccy-key"` (rw, same shape as the existing `-v "$CONFIG_TEMP:/tmp/claude-config-import:ro"` at claude-yolo:1783). No `-e`, no argv: research is explicit that `-e KEY=…` persists into podman's on-disk container config AND would be inherited by Claude Code itself, whose environment dumps land in the very transcripts we are protecting.

UNLOCK AND WIPE
entrypoint reads the identity, runs `ccy-sealstore unseal`, then `shred -u /run/ccy-key/identity` immediately — so the secret exists in the container for seconds, not for the session. From that point the container holds only `recipient.pub`; a compromised in-container agent (the prompt-injection case) can seal but cannot read the frozen archive. Host `cleanup()` does `rm -rf "$RUNDIR"` as belt-and-braces; it is tmpfs, so nothing hits disk either way.

CEREMONY (first run, `ccy --init-sealed-state`)
`age-keygen` → identity; passphrase typed twice with `read -rs` from /dev/tty, minimum 12 chars, mismatch re-prompts (3 tries); `age -p` wraps the identity; recipient.pub written; then a REQUIRED typed confirmation (not y/N) of "I understand losing this passphrase destroys all session history"; then a one-time import of the existing plaintext tree and its shredding.

RECOVERY RECIPIENTS
`ccy --add-recovery-recipient age1…` appends a second recipient (e.g. a YubiKey/age-plugin, or a paper-backed key held offline). The sealer encrypts to every recipient in `recipient.pub`. `ccy --export-identity` prints the raw identity for paper backup after a passphrase check. Both optional; the default is a single passphrase.

ROTATION
`ccy --rotate-passphrase` re-wraps identity.age only (cheap, no re-encryption). `ccy --rotate-key` requires a full unseal and re-seal of the store under a new keypair; it is a documented offline maintenance command, not a launch-path feature.

### IaC shape

NEW FILES
- `files/var/local/claude-yolo/lib/sealed-state.bash` — host library: preflight, key ceremony, passphrase prompt/unwrap, runtime-dir lifecycle, post-exit seal-freshness verification. Modelled on `lib/ssh-handling.bash`.
- `files/var/local/claude-yolo/ccy-sealstore` — in-container Python 3 executable: `init | import-plaintext | unseal | seal --once|--daemon | compact | verify | stats`. Stdlib only; drives the `age` binary via subprocess with the recipient/identity supplied as file paths (never argv secrets).
- `files/home/.local/bin/claude-sealed` — desktop (non-container) wrapper (see below).
- `files/etc/claude-code/managed-settings.json.j2` — managed-scope settings for desktop Claude Code.
- `playbooks/imports/optional/common/play-claude-sealed-desktop.yml` — NEW play, `scope: general`, deploys the desktop half.
- `scripts/qa-ccy-sealstore.bash` — round-trip gate (seal→kill→replay→byte-compare, frozen-path non-deletion, torn-line truncation, wrong-passphrase refusal). Wired into `scripts/qa-all.bash` as a seventh stage.
- `CLAUDE/Plan/00098-encrypted-claude-transcripts-at-rest/{triage.bash,acceptance.bash}` — triage probes the HOST unknowns (getenforce, `swapon --show`, `podman run --tmpfs` spike, age version, desktop `~/.claude` size census); acceptance is the pass/fail gate.

MODIFIED FILES
- `files/var/local/claude-yolo/claude-yolo` — source the new lib; call the prompt after the SSH prompt; add `--tmpfs` + `-v "$RUNDIR:/run/ccy-key"` + `-e CCY_SEALED_STATE=1` to the run invocation at :2786-2808; extend `cleanup()` at :1700 with runtime-dir wipe + seal-freshness check; add flags `--unseal-days N`, `--unseal-all`, `--init-sealed-state`, `--rotate-passphrase`, `--add-recovery-recipient`, `--export-identity`, `--no-sealed-state` (explicit, loud, records a marker in the store). **CCY_VERSION 3.30.2 → 3.31.0** and **REQUIRED_CONTAINER_VERSION 2.25 → 2.26**.
- `files/var/local/claude-yolo/entrypoint.sh` — replace the :183-195 symlink block with the sealed-mode branch: assert tmpfs both ways, unseal, shred identity, start the sealer + watchdog, EXIT-trap final seal.
- `files/var/local/claude-yolo/Dockerfile` — `apt-get install -y age` (Debian trixie ships it; base is node:lts-slim), `COPY ccy-sealstore /usr/local/bin/`, bump `LABEL claude-yolo-version` 2.25 → **2.26**.
- `playbooks/imports/play-claude-yolo.yml` — add `ccy-sealstore` to the build-context copies (alongside the Dockerfile/entrypoint copies at :86-121), add `sealed-state.bash` to the `Install Shared Helper Libraries` loop at :249, install `age` on the HOST (`package: name=age`, Fedora 44 ships age-1.3.1-4.fc44), and add a deploy-time `assert` that every active swap device is zram or dm-crypt backed.
- `playbooks/playbook-main.yml` — import the new desktop play.
- `docs/ccy.md` — sealed-state section + troubleshooting rows.
- `.claude/ccy/.gitignore` — no change needed; the bare `*` already covers `sealed/`.

DESKTOP HALF (`play-claude-sealed-desktop.yml`, the honest answer to "CCY-only leaves the bigger hole")
1. `~/.local/bin/claude-sealed`: same store shape at `~/.local/state/claude-sealed/`, plaintext at `$XDG_RUNTIME_DIR/claude-state` (tmpfs, 0700), exported via the officially documented `CLAUDE_CONFIG_DIR`. Unseal → run → `systemd-run --user --scope` sealer loop → seal on exit. One-time `--import` of the existing `~/.claude`, then shred + `rmdir`.
2. `/etc/claude-code/managed-settings.json` (managed scope, highest precedence, survives a user editing settings.json): `cleanupPeriodDays: 1`, plus permission deny-rules on `Read(**/.env*)`, `Read(**/*credential*)`, `Read(**/id_*)`, `Read(**/vault-pass.secret)` — defence in depth that stops the secret entering the transcript at all.
3. ANSIBLE MANAGED blockinfile in `~/.bashrc-includes/`: `claude() { command claude-sealed "$@"; }`.
This does NOT fully close the desktop hole: `/usr/local/bin/claude` invoked by absolute path, or a GUI/IDE launcher, bypasses the function and writes plaintext to `$XDG_RUNTIME_DIR`… no — it writes to `~/.claude`, because CLAUDE_CONFIG_DIR is only exported by the wrapper. The managed-settings `cleanupPeriodDays: 1` and the deny-rules are what bound that residual case, and the docs must say so plainly.

### CCY launch UX

Unchanged up to and including the existing SSH-key menu. Then, on stderr:

```
════════════════════════════════════════════════════════════════════
Sealed session state
════════════════════════════════════════════════════════════════════
  Store:    .claude/ccy/sealed  (41 sessions, 18.4 MB sealed)
  Unseal:   sessions active in the last 14 days (12 of 41)
            widen with --unseal-days N, or --unseal-all

Enter passphrase:            <- age prompts; no echo, straight from /dev/tty
```

Wrong passphrase (attempt 1 of 3):

```
  That passphrase did not unlock the store — let's try again.
  (2 attempts left. Ctrl-D cancels without changing anything.)
Enter passphrase:
```

After 3:

```
  Giving up after 3 attempts. No session was started and nothing was
  changed. If you have lost the passphrase, see:
    .claude/ccy/sealed/RECOVERY.txt
```
exit 1.

Ctrl-D at any point: `Cancelled — no session started, no changes made.` exit 1.

Success:

```
  ✓ Unlocked. Unsealed 12 sessions (14.1 MB) to RAM in 0.9s.
  ✓ Sealer running (every 30s). Crash exposure: ≤30s of transcript.
```

First run in a project (ceremony):

```
No sealed store in this project. Creating one.

  Session transcripts, prompt history and edited-file snapshots will be
  encrypted with a key only this passphrase can unwrap.

  IF YOU LOSE THIS PASSPHRASE, ALL SESSION HISTORY IS GONE FOREVER.
  Your git repo, your code and your plans are NOT affected.

New passphrase (min 12 chars):
Confirm passphrase:
Type exactly: I understand losing this passphrase destroys all session history
>
  ✓ Key created.  Found 62 MB of existing PLAINTEXT state — importing…
  ✓ Sealed 68 transcripts. Shredding the plaintext originals…
  ⚠ NOTE: /workspace is btrfs with copy-on-write. `shred` cannot
    guarantee removal of the old blocks; snapshots may still hold them.
    Run `sudo btrfs filesystem defragment` / expire old snapshots if
    that matters to you.
```

At exit, on a clean seal, one line. On a stale seal (container was SIGKILLed):

```
✗ SEAL STALE: the container died without a final seal.
  Last sealed 00:41 before exit — up to 41s of this session's transcript
  was lost. The sealed store is intact and consistent; the tail is gone.
```
Host wrapper exits non-zero.

### Fail-fast story

There is exactly one way plaintext can reach disk: Claude Code writing into a `/root/.claude` that is not tmpfs. Every gate is aimed at that.

1. HOST PREFLIGHT (claude-yolo, before anything else): `age` present on host and in the image; `sealed/` internally consistent (manifest ⟷ segments); swap devices all zram or dm-crypt. Any failure ⇒ exit 1 with the playbook to run. A missing tool is an IaC gap, never installed by hand and never skipped (CLAUDE.md "Missing Dependencies").

2. TWO-WAY MOUNT ASSERTION (entrypoint.sh, first thing, before the symlink logic that no longer exists):
```bash
set -euo pipefail
_fs="$(findmnt -no FSTYPE --target /root/.claude)"
if [ "${CCY_SEALED_STATE:-0}" = "1" ] && [ "$_fs" != "tmpfs" ]; then
    echo "FATAL: sealed state requested but /root/.claude is $_fs, not tmpfs." >&2
    echo "  Refusing to start — this would write session transcripts to disk" >&2
    echo "  in PLAINTEXT. Check the --tmpfs flag in claude-yolo." >&2
    exit 1
fi
if [ "${CCY_SEALED_STATE:-0}" != "1" ] && [ "$_fs" = "tmpfs" ]; then
    echo "FATAL: /root/.claude is tmpfs but sealed mode is off — state would" >&2
    echo "  be silently DISCARDED at exit. Refusing to start." >&2
    exit 1
fi
```
Both directions, because the two silent outcomes (plaintext on disk / total data loss) are equally unacceptable.

3. NO DEGRADED MODE EXISTS. There is no "encryption unavailable, continuing" branch anywhere. `--no-sealed-state` exists but is explicit, prints a full-width red banner, requires typing `yes-write-plaintext`, and stamps `sealed/DISABLED-BY-USER` with a timestamp so the next launch says so out loud.

4. NO `|| true`, NO `2>/dev/null`, NO `failed_when: false`. The sealer uses the sanctioned probe-then-check shape (capture combined output into a variable, then test it) per CLAUDE/PlanTriage.md. Any `age` non-zero exit is fatal to the cycle and escalates through the watchdog.

5. NO SILENT SUCCESS EITHER. `ccy-sealstore seal` re-reads what it wrote and verifies segment count + byte accounting against the manifest before advancing `sealed_len`. If verification fails the offset does not advance, so the next cycle re-seals the same bytes rather than skipping them.

6. EXIT-TIME PROOF. The host `cleanup()` trap reads `last_seal_epoch` and compares against container exit. Stale ⇒ loud message naming the lost window and a non-zero exit. It does not attempt a rescue it cannot perform.

7. QA GATE. `scripts/qa-ccy-sealstore.bash` (in `qa-all.bash`) runs a real round-trip against fixtures: seal → SIGKILL mid-append → replay → assert the replayed file is a prefix of the original AND ends on a newline; assert a frozen (unloaded) path is never deleted by compaction; assert a wrong passphrase exits non-zero and writes nothing. Note for whoever writes it: `grep` in this container is ugrep honouring .gitignore, so any assertion over `.claude/ccy` MUST use `command grep`.

### Failure modes

- Container SIGKILLed / OOM-killed / power loss: tmpfs is destroyed, up to `--seal-interval` seconds (default 30) of transcript is lost. Everything already sealed is intact and line-complete. This is the design's headline cost and it is bounded, reported, and tunable — not silent.
- Podman does not honour `--tmpfs` (old version, unexpected flag rejection): entrypoint's `findmnt -no FSTYPE /root/.claude` returns something other than tmpfs and it exits 1 BEFORE Claude Code starts. Without this assertion the old `rm -rf /root/.claude; ln -sf …` would silently restore plaintext-on-disk — the single most dangerous failure and the reason the check is mandatory in both directions (tmpfs present but CCY_SEALED_STATE unset also exits 1).
- tmpfs fills (size default 2g vs 62 MB today): Claude Code gets ENOSPC. The sealer's watchdog detects the write failure and kills the session loudly rather than letting Claude Code degrade. Tunable via `CCY_STATE_TMPFS_SIZE` in ccy.env.
- Sealer daemon dies mid-session: it writes a heartbeat epoch into manifest.json each cycle; the entrypoint watchdog restarts it up to 3 times, then SIGTERMs Claude Code with a FATAL message. Continuing would mean unsealed data accumulating in RAM with no durability.
- Wrong passphrase: `age --decrypt` fails (AEAD, no ambiguity). Bounded 3-try re-prompt, then exit 1. Never a fallback to unsealed mode.
- Corrupt or truncated segment: age authenticates every chunk, so `unseal` fails loudly and exits 1 rather than replaying partial state. `ccy --verify-sealed` decrypts every segment offline for a scheduled integrity check.
- Manifest torn by a crash mid-write: manifest is the only mutable file and is written temp+fsync+rename, so it is always one of two consistent versions. Segments are immutable and append-only.
- Sealed store deleted by `git clean -xfd` (it is gitignored, so yes, clean would take it): documented in docs/ccy.md; `ccy` refuses to launch if `sealed/manifest.json` is missing while `sealed/seg/` is not, i.e. it detects partial destruction instead of silently starting fresh.
- Swap: tmpfs pages can be paged out in plaintext. Preflight gate rejects launch if any active swap device is neither zram nor dm-crypt-backed. Fedora 44 defaults to zram (RAM-backed) so this normally passes silently.
- Two CCY sessions in one project concurrently: both tmpfs trees are independent, both sealers write to one store. Mitigated with an flock on `sealed/.lock` around each seal cycle plus per-launch `gen` namespacing; a second launch is warned that its unseal snapshot predates the other session's writes.

### Threats covered

- Backup / sync exfiltration of the project tree (rsync, restic, Syncthing, Dropbox, rclone, Timeshift). .gitignore is a git construct and does nothing for these; .claude/ccy sits inside the working tree by design. This is the single strongest win — the copied bytes are age ciphertext.
- Stolen laptop SUSPENDED with LUKS keys resident in RAM (the documented Linux 6.9+ suspend key-wipe regression). LUKS is already unlocked; the sealed store is not, because the passphrase was never stored.
- Another local user on the box. Today 744 of 969 state files are mode 644 under a 755 top dir, including 344 verbatim pre-edit file bodies in file-history/ (7.7 MB). After SEALSTORE those files do not exist on disk at all.
- Offline forensic recovery from an old backup, an old btrfs snapshot, or a decommissioned disk taken after FDE was unlocked once.
- Code execution as the user while NO session is live — the archive is opaque and no key exists anywhere on disk in usable form.
- A future infostealer that enumerates ~/.claude and project-local agent state by name (the TrapDoor / SEO-poisoning class): with no session running it harvests ciphertext and a public key.
- Indefinite accumulation: sealed-store retention (CCY_SEAL_RETENTION_DAYS, default 30) bounds the archive the same way cleanupPeriodDays bounds plaintext, and history.jsonl — which upstream never sweeps — is inside that boundary.
- Accidental `cat`/`grep` of a transcript by the user or by a sub-agent auditing the tree (which is exactly how this recon session wrote secret-shaped strings to disk).

### Threats NOT covered

- Code execution as the user WHILE A SESSION IS LIVE. The tmpfs is readable by the same UID an attacker would already have (rootless podman maps container root → host uid 1000, and the attacker can join the mount namespace via nsenter/podman exec). This is the honest limit of every at-rest scheme with a launch-time key.
- A prompt-injected Claude session. The agent is inside the trust boundary by construction — it reads the decrypted view because that is its job. Only the frozen (not-unsealed) portion of the archive is out of its reach, which is a real but partial benefit.
- A malicious npm/pip/MCP package executing inside CCY. Same as above; it also has recipient.pub, so it can write to the store (it cannot read frozen history).
- Root on the box, live. Reads the tmpfs or the sealer's memory directly.
- Stolen laptop POWERED OFF. Already covered by the existing LUKS FDE on /dev/mapper/luks-…; SEALSTORE adds nothing here and should not be sold as if it does.
- Server-side retention by Anthropic. Entirely out of scope; a reader who thinks local encryption addresses it has been misled.
- Metadata: sealed/manifest.json is plaintext and leaks session UUIDs, relative paths, file sizes, segment counts and timestamps. An observer learns when you worked and roughly how much, but not what about.
- Secrets that never should have entered context in the first place. The managed-settings deny-rules on .env/credential reads are the actual control for that; encryption is a second line, not a substitute.
- Desktop Claude Code launched by absolute path or from a GUI/IDE, bypassing the claude-sealed shell function. Bounded only by managed-settings cleanupPeriodDays: 1 and the read deny-rules.

### Effort

large

### Risks

- UNVERIFIED CORE ASSUMPTION: rootless podman `--tmpfs /root/.claude` on Fedora 44 with SELinux enforcing has NOT been tested on this host. The design is credible (podman/crun handles the mount, labels it container_file_t, needs no caps) but it is the load-bearing fact. This is the first thing triage.bash must prove; if it fails, the whole angle collapses and gocryptfs-in-container is the fallback.
- Data loss is a NEW failure mode this repo does not have today. Plaintext append-only JSONL degrades gracefully; a killed container now costs up to 30s. Tunable down to 5s at a CPU/IO cost, but never zero.
- Key loss destroys all session history: transcripts, history.jsonl, file-history/, auto-memory, plans cache. Accepted deliberately for THIS repo because the durable knowledge — CLAUDE/Plan/**/PLAN.md and JOURNAL/ — is in git and unaffected. That is a defensible trade here; it would not be everywhere.
- tmpfs pages can be swapped in plaintext. Mitigated by a launch gate requiring zram or encrypted swap, not by mlock (you cannot mlock a tmpfs).
- The launch path is CCY's single point of traversal and this adds crypto, a mount assertion and a prompt to it. A bug bricks every session in every project. Mitigated by the QA round-trip gate and by `--no-sealed-state` as a loud, deliberate escape hatch.
- Claude Code's behaviour when its config dir is a tmpfs is untested — flock on daemon.lock, session-env/, sessions/ liveness files. tmpfs supports flock and full-length filenames so it should be strictly safer than the eCryptfs case in issue #80753, but it needs an empirical trial.
- Store growth: at ~45 MB of transcripts today and a log-structured design, compaction correctness (especially the frozen-path rule) is where a subtle bug would silently eat history. Needs its own unit fixtures.
- Unseal latency grows with the unseal window; the default 14 days keeps it ~1s but a user running --unseal-all on a year-old store will wait, and will hold that whole year in RAM.
- Two concurrent CCY sessions in one project is a genuinely awkward case; the flock + gen namespacing bounds corruption but the UX is a warning, not a clean answer.
- btrfs CoW means shredding the existing 62 MB of plaintext at migration is best-effort. Snapshots and unreferenced extents may retain it. Must be stated in the ceremony output, not buried in docs.

### Open questions

- Does `podman run --tmpfs` work rootless on this host under SELinux enforcing, and is the host actually enforcing? `getenforce` could not be read from the container; evidence (container_t writing user_home_t with no :Z) suggests permissive or a local allow rule. HOST triage probe required before anything is written.
- Does Claude Code tolerate its entire config dir on tmpfs — specifically flock on daemon.lock, sessions/ liveness files, and session-env/? Needs an empirical trial, not inference.
- Does Claude Code write any state OUTSIDE CLAUDE_CONFIG_DIR? CLAUDE_CODE_TMPDIR defaults to /tmp, and issue #21602 reports a tmp/attachments dir in the project directory. If so the boundary is incomplete and /tmp must be tmpfs'd too. Must be probed with a live session + lsof/inotify.
- What is the actual size and secret content of the DESKTOP store at ~/.claude/projects/<slug>/? Entirely unmeasured from inside the container, and it is the bigger hole. HOST triage probe.
- Is `age` in Debian trixie (the node:lts-slim base) or does the image need the upstream binary? If the former, one apt line; if the latter, a checksummed download in the Dockerfile.
- What is the right default seal interval? 30s is a guess. Should be benchmarked against real append rates on this host — a 5.7 MB transcript over a long session implies a modest byte rate, so 10s may be nearly free.
- Should the manifest be sealed too? Plaintext manifest enables unattended crash-recovery and compaction without the passphrase, at the cost of leaking session UUIDs, sizes and timings. Current answer: leave it plaintext and document the leak — but the user may prefer the opposite trade.
- Is the desktop half in scope for this plan (00098) or a follow-on? It roughly doubles the work and touches managed-settings, which is a host-wide policy change deserving its own decision gate.
- Does `claude project purge` have a non-interactive flag? If yes it is a cleaner primitive than hand-rolled shredding for the migration step.
- Is swap on this host zram-only? If a disk swapfile exists on the btrfs volume it is inside LUKS, which satisfies the gate — needs confirming rather than assuming.

