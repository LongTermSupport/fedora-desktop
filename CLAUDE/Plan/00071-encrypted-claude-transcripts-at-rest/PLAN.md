# Plan 00071: encrypted claude transcripts at rest

**Status**: In Progress
**Created**: 2026-08-13
**Owner**: joseph
**Priority**: Medium

## Overview

Claude Code persists every conversation as plaintext JSONL at a predictable path, and
Anthropic documents this openly: transcripts are **not encrypted at rest**, and "OS file
permissions are the only protection". The request that opened this plan was to encrypt that
state, with the key supplied at CCY launch alongside the existing SSH-key prompt.

Research (5 parallel agents, 3 independent designs, 2 adversarial judges — see
[`research/`](research/)) confirmed the premise but **inverted the design**. Three findings do
most of the work:

1. **The host is already LUKS-encrypted.** At-rest encryption buys nothing against a stolen
   powered-off machine. A key supplied at launch is loaded precisely when the most probable
   2026 attacker — an infostealer or malicious npm/MCP package running as this user — is
   active, and that attacker reads the plaintext exactly as Claude Code does.
2. **The measured hole is beside the target.** `projects/` is already mode 700 — the one
   directory everyone proposes to encrypt is the one already protected. Meanwhile **879 of
   ~978 state files are group/other-readable**, including 7.7 MB of verbatim pre-edit file
   bodies in `file-history/`, because nothing sets a umask.
3. **Encrypting the live file would break this repo's own guardrails.** The hooks daemon reads
   transcripts by byte offset (`core/transcript_reader.py`), which the Stop handlers depend on.

So the plan ships **blast-radius reduction first and unconditionally**, and puts live-state
encryption behind a host-run triage gate — where the deciding question is whether anything
actually backs up or syncs the project tree, since that is the one scenario where encryption
beats the LUKS already present.

Two stores are in scope, not one. CCY does **not** bind-mount the host `~/.claude`;
`entrypoint.sh:195` symlinks `/root/.claude` to `/workspace/.claude/ccy`, so container state
lands *inside the project working tree* — which makes CCY strictly **more** exposed to
copy-based exfiltration than the desktop store, because `.gitignore` does nothing for rsync,
restic, borg or Syncthing.

## Goals

- Close the measured, currently-open exposure in Claude Code's on-disk state (permissions,
  retention, credential reads, backup inclusion).
- Reduce the window in which secrets can land in a transcript at all.
- Establish, by host triage, whether live-state encryption is justified here — and build it
  only if it is.
- Keep every guardrail this repo already depends on working: byte-offset transcript reads,
  `/resume`, the hooks daemon.

## Non-Goals

- Encrypting the whole home directory or replacing the existing LUKS full-disk encryption.
- Patching Claude Code, or depending on unreleased upstream behaviour. Anthropic closed the
  encryption-at-rest request (#50014) as *not planned*.
- Redaction hooks as a primary defence — every published implementation documents the same
  `@file` inlining bypass and disclaims protection.
- Host-side gocryptfs. It requires `user_allow_other` in `/etc/fuse.conf`, a permanent
  host-wide widening imposed to protect one directory.
- Extending the sealer to live transcript files. This is a **standing** non-goal: it would
  break the daemon's byte-offset reads.

## Threat Model Verdict

Recorded here because it is the plan's central decision, not an appendix. Full reasoning in
[`research/synthesis.md`](research/synthesis.md).

| Scenario                                                      | At-rest encryption of this dir              |
| ------------------------------------------------------------- | ------------------------------------------- |
| Copy-exfiltration of the project tree (rsync/restic/Dropbox)  | ✅ genuinely defeated — the strongest case  |
| Code execution as the user *between* sessions                 | ✅ defeated                                 |
| Stolen laptop, suspended, no session live                     | ✅ defeated                                 |
| Another local non-root user                                   | ⚠️ defeated far more cheaply by `umask 077` |
| RCE / malicious package / prompt-injected agent, session live | ❌ category error — key is loaded           |
| Stolen laptop, powered off                                    | ❌ zero delta over existing LUKS            |
| Secrets entering the transcript at all                        | ❌ encryption is downstream of the leak     |

**Two premises in the original framing are wrong and must not be repeated.** Transcripts do
not persist indefinitely — `cleanupPeriodDays` defaults to 30 and is demonstrably working here
(oldest transcript 29 days old), though `history.jsonl` genuinely is never swept. And the
archive is not currently a goldmine: **zero live credentials** were found across 60 historical
transcripts. What was found is **27 `$ANSIBLE_VAULT` blobs across 7 files**, which proves the
capture mechanism end to end. The risk is **prospective, with a proven mechanism** — not a
realised breach. The plan rests on that framing.

## Options Considered

| Option                                   | Verdict           | Why                                                                                |
| ---------------------------------------- | ----------------- | ---------------------------------------------------------------------------------- |
| Do nothing                               | ❌ reject         | Leaves 879 world-readable files and an unpinned retention sweep                    |
| `CLAUDE_CODE_SKIP_PROMPT_HISTORY=1`      | ⚠️ opt-in only    | True zero-plaintext, but kills `/resume` and starves the daemon                    |
| Redaction hooks                          | ❌ reject         | Universal `@file` bypass; writes are async                                         |
| fscrypt                                  | ❌ impossible     | btrfs has no merged fscrypt support                                                |
| LUKS loopback image                      | ❌ reject         | Needs CAP_SYS_ADMIN — sudo/polkit prompt per launch                                |
| eCryptfs / EncFS / CryFS                 | ❌ reject         | Unmaintained / audited-insecure / unaudited; eCryptfs silently breaks CC           |
| **A** — host gocryptfs (CryptView)       | ❌ reject         | Host-wide `user_allow_other` downgrade; 3 unverified SELinux/podman premises       |
| **B** — tmpfs + sealed store (SEALSTORE) | ⚠️ shape kept     | Shape adopted; its bespoke segment/manifest engine rejected as a data-loss surface |
| **C** — blast-radius reduction           | ✅ **ship first** | Fixes the only measured hole; no mount, no cap, no FUSE, cannot break the daemon   |

## Tasks

### Phase 0: Ground truth (HOST triage — blocks every design decision)

- [ ] ⬜ **Task 0.1**: Write plan-local `triage.bash` — read-only, re-runnable, `probe()` helper,
  logs to `<plan>/logs/` resolved from `BASH_SOURCE[0]`, `--help` before any environment
  resolution, **`command grep` only**
- [ ] ⬜ **Task 0.2**: Probe SELinux mode and container booleans (`getenforce`, `getsebool -a`,
  `ausearch -m AVC`)
- [ ] ⬜ **Task 0.3**: Probe backup/sync coverage of the project tree — **this finding largely
  decides whether Phase 2 is worth building**
- [ ] ⬜ **Task 0.4**: Census the desktop `~/.claude/projects/` store — never measured, and it has
  no container boundary
- [ ] ⬜ **Task 0.5**: Probe rootless `podman run --tmpfs /root/.claude` and assert `findmnt`
  reports tmpfs
- [ ] ⬜ **Task 0.6**: Probe host swap backing — plaintext tmpfs pages must not swap to an
  unencrypted device
- [ ] ⬜ **Task 0.7**: Find every consumer that hardcodes `.claude/ccy` (blocks any
  `entrypoint.sh:195` change)
- [ ] ⬜ **Task 0.8**: Probe whether Claude Code writes sensitive data outside its config dir
- [ ] ⬜ **Task 0.9**: Record Phase 0 results in `JOURNAL/`, separating F-numbered facts from
  H-numbered hypotheses with their refuting observation

### Phase 1: Blast-radius reduction (ship unconditionally)

- [ ] ⬜ **Task 1.1**: `umask 077` in `entrypoint.sh` before state creation, and in the desktop
  `cc` wrapper
- [ ] ⬜ **Task 1.2**: New `playbooks/imports/play-claude-state-hygiene.yml` (`scope: general`)
  with a recursive `mode: "u=rwX,go="` reconciliation over both stores
- [ ] ⬜ **Task 1.3**: Deploy `/etc/claude-code/managed-settings.json` — pins `cleanupPeriodDays`
  and adds `permissions.deny` rules for `.env*`, `*.pem`, `id_*`, `vault-pass.secret`
- [ ] ⬜ **Task 1.4**: Bake the same managed settings into the CCY image; `entrypoint.sh` asserts
  it exists and parses, exiting 1 if not
- [ ] ⬜ **Task 1.5**: Write the credential-read-blocker handler test **first**, then the handler
- [ ] ⬜ **Task 1.6**: Write `CACHEDIR.TAG` into both stores — honoured by borg, restic, and
  `rsync --exclude-caches`
- [ ] ⬜ **Task 1.7**: Generate the age keypair with `creates:` **plus** a `stat` + non-empty
  `assert` whose `fail_msg` names the file to delete; seal a `canary.age`
- [ ] ⬜ **Task 1.8**: Write `ccy-seal` — seals archived sessions with `age -R` (public key, no
  prompt), `--unseal`/`--dry-run`/`--report`, `flock`-guarded, never touches live sessions
- [ ] ⬜ **Task 1.9**: Write `lib/state-hygiene.bash` with an `assert_scanner_can_see()` canary
  self-test — a blind scanner reports "clean", which is indistinguishable from safety
- [ ] ⬜ **Task 1.10**: Wire the launch banner in after `discover_and_select_ssh_keys`; add
  `--unseal`, `--seal-now`, `--state-report`. Posture repair and scan are **advisory**
- [ ] ⬜ **Task 1.11**: Install the new lib and script via the existing playbook loops; import the
  new play from `playbook-main.yml`
- [ ] ⬜ **Task 1.12**: Add a systemd **user** timer for the desktop store (precedent:
  `container-watch.timer`)
- [ ] ⬜ **Task 1.13**: Bump `CCY_VERSION` → 3.31.0 and `REQUIRED_CONTAINER_VERSION` → 2.26 +
  Dockerfile label
- [ ] ⬜ **Task 1.14**: Write `acceptance.bash` — 0 other-readable files, scanner canary detected,
  sealed session round-trips byte-identically, wrong passphrase changes nothing,
  managed-settings parses with both keys
- [ ] ⬜ **Task 1.15**: Run `./scripts/qa-all.bash` and fix all findings
- [ ] ⬜ **Task 1.16**: Write `docs/claude-state-hygiene.md`, index it, add `docs/ccy.md`
  troubleshooting rows — leading with the honest scorecard, not the feature
- [ ] ⬜ **Task 1.17**: (HOST) run `deploy.bash` → `triage.bash` → `acceptance.bash`

### Phase 2: Decision gate — live-state encryption

- [ ] ⬜ **Task 2.1**: Record the gate decision — proceed only if Task 0.3 confirms the tree is
  backed up/synced, **or** the user explicitly accepts the between-sessions scenario as
  sufficient justification
- [ ] ⬜ **Task 2.2**: If proceeding, confirm Task 0.5 cleared `--tmpfs`; if not, evaluate
  gocryptfs *inside* the container and record the reasoning. Host-side gocryptfs stays rejected
- [ ] ⬜ **Task 2.3**: Empirically prove Claude Code tolerates the candidate filesystem —
  `flock` on `daemon.lock`, `sessions/` liveness, and a 250-char path — **before** any code

### Phase 3: Live-state encryption (only if Phase 2 clears)

- [ ] ⬜ **Task 3.1**: Replace `entrypoint.sh:183-195` (including the `rm -rf /root/.claude`
  branch, which would destroy live state under a mount) with a **two-way** `findmnt` assertion
- [ ] ⬜ **Task 3.2**: Add the `--tmpfs` mount; pass the identity as a bind-mounted file, never
  via `-e` or argv
- [ ] ⬜ **Task 3.3**: Restore-at-start and interval-seal using **Phase 1's `ccy-seal`** — no
  bespoke store engine
- [ ] ⬜ **Task 3.4**: Shred the identity inside the container after restore, leaving only the
  public key where untrusted agent code runs
- [ ] ⬜ **Task 3.5**: Exit-time seal-freshness check — a stale seal reports the lost window
  loudly and exits non-zero
- [ ] ⬜ **Task 3.6**: Launch passphrase prompt — `read -rs` from `/dev/tty`, stderr prompts,
  `MAX_TRIES=3`, EOF cancels cleanly, **no fallback to unencrypted mode ever**
- [ ] ⬜ **Task 3.7**: Extend `acceptance.bash` — kill-mid-append round-trip, 250-char path,
  measured append and `load_tail` performance gates
- [ ] ⬜ **Task 3.8**: Version bumps again; QA; document the ceremony, key backup, recovery, and
  what key loss costs

## Success Criteria

- [ ] Threat-model verdict recorded and reflected in user-facing docs — no claim that a live
  session is encrypted when it is not
- [ ] Zero group/other-readable files in either store after deploy, verified by `acceptance.bash`
- [ ] Retention pinned in managed scope so a malformed settings file cannot silently degrade it
  to keep-forever
- [ ] Scanner canary self-test passes — no "clean" result is trusted from a blind scanner
- [ ] Hooks daemon byte-offset transcript reads and `/resume` still work
- [ ] QA passes (`./scripts/qa-all.bash`)

## Risks & Mitigations

| Risk                                                           | Impact | Probability    | Mitigation                                                                                                |
| -------------------------------------------------------------- | ------ | -------------- | --------------------------------------------------------------------------------------------------------- |
| Scanner blindness makes every audit report "clean"             | H      | H              | Canary self-test before any clean result is trusted; `command grep` mandated                              |
| Security theatre — user believes the live session is encrypted | H      | H              | Launch banner prints `PLAINTEXT until sealed`; docs lead with the scorecard                               |
| A new pre-launch gate bricks the path every session traverses  | H      | M              | Phase 1 posture repair and scan are advisory; guards name the exact playbook                              |
| Live-file encryption breaks the daemon's byte-offset reads     | H      | H if attempted | Structurally excluded — Phase 1 never touches the live file                                               |
| Age passphrase lost → sealed archives unreadable               | M      | L              | Nothing sealed is a source of truth; optional second offline recipient — **decide before the first seal** |
| `--tmpfs` silently does not apply → plaintext on disk          | H      | M              | Two-way `findmnt` assertion; the old `rm -rf` branch deleted outright                                     |
| `permissions.deny` blocks legitimate reads                     | L      | M              | Tight patterns; deny message names how to proceed                                                         |
| `git clean -xfd` destroys the gitignored sealed store          | M      | L              | Documented loudly; `ccy` refuses to launch on partial destruction                                         |
| Sealer later extended to live files by a contributor           | H      | M              | Standing non-goal above + a comment in `ccy-seal` citing the reason                                       |

## Open Questions For The User

Blocking Phase 2; Phase 1 proceeds regardless.

1. **Does anything back up or sync the project tree?** If nothing does, the headline
   justification for Phase 2 largely evaporates. Task 0.3 answers it.
2. **Escrow or not?** Losing the age passphrase loses the sealed archives. Recommendation:
   accept it (code, plans and journals are in git). The alternative is a second offline
   recipient — one line, but **retrofitting requires re-sealing everything**.
3. **Retention floor**, and whether the seal keeps N sessions or N days.
4. **CCY-only or CCY + desktop?** Phase 1 covers both cheaply. Recommendation: measure in
   Task 0.4, then decide.
5. **Is a `ccy --ephemeral` flag wanted** for genuinely sensitive sessions? The only true
   zero-plaintext answer, at the cost of `/resume` and the daemon's Stop handlers.
6. **How aggressive should the credential deny-rules be?**

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00071-Journal-YY-MM-DD.md. -->

- Plan opened; research workflow `wf_487ae6c4-73e` dispatched — d5985e6
- Research complete (11 agents, 0 errors); recommendation recorded; full artefacts in
  [`research/`](research/)
