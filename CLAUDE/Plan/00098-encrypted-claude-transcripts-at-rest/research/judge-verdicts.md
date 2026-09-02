# Judge verdicts — Plan 00098

Two adversarial judges scored the three designs independently.

## Judge 1 — security lens

# Security judgment: encryption-at-rest for CCY transcripts

## Cross-cutting findings that reframe all three scores

**1. The dominant 2026 threat is untouched by every proposal.** Infostealers targeting `~/.claude` by name, malicious npm/MCP postinstalls, and prompt-injected agents all execute *as uid 1000, while a session is live, with the key loaded*. A launch-time key is present exactly when the attacker runs. All three proposals admit this; none should get credit for it. The user's stated goal — "an attacker browsing the filesystem sees only opaque blobs" — is achievable only when no session is running, which on this host (6 concurrent CCY sessions observed) is a minority of wall-clock hours.

**2. `shred` on btrfs is a no-op, so day-one migration claims are false for all three.** CoW + compression + snapshots mean the existing 62 MB of plaintext survives the "migrate and shred" step in extents nobody can address. Proposal B is the only one that says so. This means the *historical* goldmine — the thing that motivated the request — is not removed by any encryption design. Only retention expiry plus time removes it. That is a significant point in favour of the retention-first framing.

**3. Removal beats protection.** `cleanupPeriodDays: 30 → 7` deletes 77% of the corpus; `→ 1` deletes 97%. It is one line, pinned in managed scope so a malformed user settings file cannot silently degrade the sweep to "keep forever." No key, no mount, no new failure mode on the launch path. Every proposal that treats this as an afterthought is optimising the wrong variable.

**4. The one measured, currently-open hole is a permissions bug, not a crypto gap.** 879 of 978 state files are other-readable, including 344 verbatim pre-edit file bodies at 0644 under a 0755 directory. `projects/` — the directory the user wants encrypted — is already 0700. The leak is beside the target.

---

## CryptView (host gocryptfs → bind-mounted plaintext view) — **4/10**

It does deliver genuine continuous encryption of the persisted representation, and the G4 canary (write random bytes, `command grep` the ciphertext tree, abort if findable) is the only *proof* of encryption in any of the three — real verification rather than assumed. But it buys that by permanently widening a host-wide policy (`user_allow_other` in `/etc/fuse.conf`) so that a per-project feature weakens FUSE access control for every user and every mount on the box, forever. The plaintext view lives in the **host** mount namespace at a deterministic, enumerable path (`$XDG_RUNTIME_DIR/ccy-plain/sha256(realpath $PWD)[:16]`) — an infostealer globs `/run/user/1000/ccy-plain/*` and is done, no key needed. Worse, the refcounted unmount plus `-idle 30m` means that with 6 concurrent sessions the mount is effectively up all day, so the claimed "code execution between sessions finds only ciphertext" win largely evaporates on this user's actual usage pattern. It also stacks three unverified load-bearing facts (SELinux boolean name, enforcing mode, podman/FUSE interaction) onto the single code path every session traverses.

**Biggest flaw:** it converts a per-project confidentiality feature into a permanent host-wide FUSE authorisation downgrade, and in exchange produces a plaintext directory at a predictable host path that stays mounted most of the day.

---

## SEALSTORE (tmpfs live + age public-key sealing) — **7/10**

The asymmetric split is the best structural idea in the set: sealing needs only the public key, so the seal path never blocks on a human and can run unattended from an exit trap or watchdog; and after the identity is shredded seconds into startup, a compromised in-container agent can *write* to the archive but cannot *read* the frozen portion. That is a real capability boundary neither other design achieves. Plaintext never touches a block device at all, which structurally eliminates crash residue, backup capture, and stale-mount exposure — the failures that sink CryptView. The swap gate (reject launch unless swap is zram or dm-crypt) is a detail almost everyone misses and it matters, because tmpfs pages are swappable in cleartext.

Two things pull it down hard. The 14-day default unseal window means a live-session attacker gets a fortnight of history in RAM, not one session — the headline property is undercut by its own default. And `manifest.json` is plaintext and unauthenticated, so an in-container attacker with `recipient.pub` can flip `loaded`/`gen` fields and drive compaction into unlinking live segments: **integrity destruction of the archive by a party who cannot read it.** Add the bespoke log-structured segment store — `sealed_len` offsets, generation bumps, frozen-path rules — whose failure mode is silent history loss, and this is the riskiest code in the comparison.

**Biggest flaw:** the plaintext, unsigned manifest is a write-side integrity hole reachable by the exact attacker the asymmetric design was built to exclude — plus a 14-day default that hands that attacker most of the archive anyway.

---

## Blast-Radius Reduction (ccy-state-hygiene) — **8/10**

This is the only proposal that fixes something measurably broken today rather than something hypothetically breakable, and it is honest that it does not encrypt the live transcript. `assert_scanner_can_see()` — planting a canary and refusing to trust a "clean" result until the scanner has proved it can detect — is the single best security idea across all three, and directly answers the ugrep failure that already produced a false all-clear during research. Public-key archive sealing gives it B's key model at a fraction of B's complexity, with no prompt on the common path and no usable key material on the box. Retention pinning, managed-scope deny rules, and the umask/mode fix each cost one task and help scenarios that no encryption design touches.

Two real problems. It **overclaims on backup coverage**: `CACHEDIR.TAG` is honoured by restic, borg, and `tar --exclude-caches`, but *not* by Dropbox, Syncthing, or `rsync` without an explicit flag — and sync-tool exfiltration is one of only two scenarios where any of this beats the existing LUKS FDE. And the launch-blocking credential scan is a footgun: a false positive bricks the path every session traverses, and refusing to launch does not unwrite the secret that is already on disk.

**Biggest flaw:** it does not deliver what the user literally asked for — the live session and the N most recent sessions remain plaintext at well-known paths, so "an attacker browsing the filesystem sees only opaque blobs" stays false.

---

## Ranking on real risk reduction

| | Cold archive | Present-day open hole | Live window | Complexity cost | Net |
|---|---|---|---|---|---|
| CryptView | good | incidental | none (+ host-wide downgrade) | large | **4** |
| SEALSTORE | good (read), weak (integrity) | incidental | best available | largest | **7** |
| Hygiene | good | **only one that targets it** | none, and says so | medium | **8** |

**False comfort call: CryptView.** It is the design most likely to make the user believe the problem is solved while a decrypted directory sits at an enumerable host path all day and the box's FUSE policy is permanently weaker. Not pure theatre — the backup/sync win is real — but it has the worst comfort-to-risk-reduction ratio, and it is the only one whose *side effects* reduce security elsewhere.

---

## What I would ship

**None as proposed. Ship Hygiene with three amendments, then B's tmpfs — and only B's tmpfs — as a gated phase 2.**

**Phase 0 (prerequisite, ship first, no crypto):**
- `umask 077` in `entrypoint.sh` and the `cc` wrapper + one-time mode reconciliation. Closes the only measured open hole.
- `/etc/claude-code/managed-settings.json` pinning `cleanupPeriodDays: 7` (argue for 3) and the credential deny-rules. Removal beats protection; managed scope survives a malformed user settings file.
- Desktop parity in the same play. A CCY-only fix leaves the store with no container boundary untouched and unmeasured.

**Phase 1 — Hygiene's L3 archive sealing, amended:**
1. **Make the pre-launch credential scan report-only.** Keep `assert_scanner_can_see()` — it is excellent — but a scanner false positive must never block the launch path, and blocking does not unwrite the secret.
2. **Drop the CACHEDIR.TAG-solves-backups claim.** Have `triage.bash` establish what actually covers the tree (restic/borg/rclone/Syncthing/Dropbox/Timeshift), then write the tool-specific exclusion. If nothing syncs or backs up the project dir, say so and reconsider whether phase 2 is worth building at all — that finding largely decides the question.
3. **Seal at session end, not on an N-most-recent heuristic.** One session's plaintext window, not "the last 7 days." Same public-key model, no unseal-on-launch, no 14-day RAM exposure.

**Phase 2 (gated on the triage finding above) — take exactly one thing from SEALSTORE:** `--tmpfs /root/.claude` plus the two-way `findmnt` assertion in `entrypoint.sh`. Roughly fifteen lines. Combined with phase 1's seal-on-exit you get the property the user actually wants — no plaintext block ever written to disk — without the log-structured segment store, without the plaintext manifest, without the compaction state machine, and without the 14-day unseal default. Keep B's swap gate; it is correct and cheap.

**Reject outright:** the host-side FUSE mount and the `user_allow_other` edit. A system-wide authorisation downgrade is not an acceptable price for a per-project confidentiality feature, and the resulting plaintext path is enumerable by the attacker who matters.

**State plainly in the docs and the launch banner:** this protects the cold archive, backups, and other local users. It does not protect a live session against code executing as you, which is the likeliest attacker in 2026. Anyone who reads "encrypted" as "safe to paste secrets into context" has been misled, and that misreading is a net-negative outcome worse than shipping nothing.
The judgment is delivered above as my final response. No further work is required — the analysis is complete: three scores with justifications, the single biggest flaw named for each, and a ship recommendation (hybrid: Hygiene amended, plus SEALSTORE's tmpfs gated on a triage finding, rejecting CryptView's host-wide FUSE downgrade).

Regarding the prompt-capture suggestion in the hook output: that is a repo convention for the calling session to action if it judges the prompt reusable, not something I should run from this subagent — `npm run llm:prompts` is also a write operation against project state that the parent agent owns.

## Judge 2 — operability / IaC lens

I'll evaluate these against the operability/IaC lens. Let me ground a few load-bearing claims in the actual repo before scoring.
Verified against the repo before scoring: `transcript_reader.py` byte-offset reads are real (`path.open("rb")` + `seek` at :239-240 and :409-427); the posture claim is real (881 of 982 files under `.claude/ccy` are other-readable, `.claude/ccy` 755, `file-history/` 755, `projects/` already 700); `seboolean` precedent is real (`play-ftp-camera.yml:204`); systemd user timer precedent is real (`container-watch.timer`); `entrypoint.sh:189-195` is the `rm -rf` + `ln -sf` block all three must displace.

---

# OPERABILITY / IaC VERDICT

## CryptView (host gocryptfs → bind-mounted view) — 6/10

**Deployability.** The playbook is honest IaC: `package`, `lineinfile` on `/etc/fuse.conf`, `seboolean`, `copy` — all idempotent, all declarative, and the `seboolean` citation is accurate rather than invented. It is also the only design that leans on a *distro-maintained, independently-audited* crypto primitive rather than one it writes itself, which is the single most important maintainability property on this table. And it is uniquely immune to Claude Code layout churn: it hands the app a POSIX filesystem and stops caring. New directories, longer paths, a future SQLite store — all just work.

**Where it breaks.** The mount lives in the *host* mount namespace and outlives the container, so SIGKILL/OOM/terminal-close leaves a decrypted view readable indefinitely; `-idle 30m` is a timer, not a guarantee. Refcounting six concurrent sessions via `podman ps --filter label` is fragile in both directions — a premature unmount pulls the config dir out from under a live session (behaviour explicitly unknown), and a failed query leaks the mount forever. `$XDG_RUNTIME_DIR` is assumed to exist and won't in every invocation context. Worst: `user_allow_other` in `/etc/fuse.conf` is a permanent, host-wide widening deployed for a per-project feature, which sits badly against this repo's least-privilege principle.

**Biggest flaw:** the core mechanism's viability is unverified on the target OS. FUSE gets `fusefs_t`, cannot be relabelled, and the boolean that would grant `container_t` access is named-but-unconfirmed — while nobody has established whether the host is even enforcing. A large design whose load-bearing mechanism is a coin flip pending a host probe cannot be scored above the middle, however good the pre-launch probe container is at failing loudly.

## SEALSTORE (tmpfs live + age-sealed log-structured store) — 4/10

**Deployability.** On the pure Ansible surface this is the cleanest of the three by a distance: install `age`, copy two files, bump the image. No `fuse.conf`, no seboolean, no `/dev/fuse`, no capabilities, no new host system state, and SELinux is genuinely a non-issue. Its two-way `findmnt` assertion — refusing to start if tmpfs is expected and absent *and* if tmpfs is present when it shouldn't be — is the best single fail-fast gate anyone wrote here, because it closes both silent outcomes rather than the obvious one.

**Where it breaks.** What is actually being deployed is not a playbook; it is `ccy-sealstore` — a bespoke incremental encrypted store with generations, sequence numbers, compaction, GC, frozen-path invariants and manifest atomicity. That is a database engine, maintained forever, inside an Ansible repo whose Python is stdlib-only helpers and whose own QA doc states plainly that QA does not catch logic errors. A compaction bug that eats a "frozen" path is silent permanent history loss and no gate in this repo can see it. Layout churn is the second wound: the sealer carries per-kind write-pattern knowledge (append-only JSONL vs snapshot-on-hash-change), so a rewritten-in-place file or a new SQLite store yields *wrong archives*, not crashes — discovered only on resume. And the concurrency answer for six sessions in one project is, by its own admission, a warning.

**Biggest flaw:** it introduces guaranteed data loss (≤30s, by design, where today there is none) into the single code path every session traverses, and pays for it by swapping a distro-owned dependency for a self-owned storage engine. That is the exact inverse of this repo's ethos — declare state, let the distro own the mechanism.

## Blast-Radius Reduction (ccy-state-hygiene) — 8/10

**Deployability.** This is what the repo is built to express. Every layer is a declarative task; the `age-keygen` step correctly cites the repo's own documented `ansible.builtin.password` gotcha and gates on *non-emptiness* rather than existence, which is grounded repo knowledge rather than generic advice. `umask 077` is forward-compatible in a way `chmod` never is — every directory Claude Code invents next year is covered automatically, with no code change. Critically, C is the only design whose shape does not change based on the unresolved SELinux question.

**Blast radius.** The live path is untouched: no mount, no tmpfs, no interception. If every new component fails, CCY still launches and state still persists exactly as today. The failure mode is "the control didn't apply," not "the session broke" — categorically different from A and B, which both interpose novel machinery between Claude Code and its config dir. Forgotten passphrase loses only the archive, not recent or live sessions. The hooks daemon and `/resume` are structurally unaffected because the byte-offset reads I verified never meet ciphertext. And `assert_scanner_can_see()` is the best idea in any of the three: this repo has *already been burned* by a scan returning all-zeros because `grep` is a ugrep wrapper honouring `.gitignore`, and proving the scanner detects a planted canary before trusting a clean result is exactly the right lesson from that incident.

**Biggest flaw:** it gates the launch path — the one path every session traverses — on a regex credential scan over ~45 MB of prose, plus seven other hard gates. A false positive bricks work; `CCY_STATE_SCAN=off` exists, and a control everyone disables after the third false positive is worse than no control. The scan and the posture-repair gates should be loud-banner advisory; only "the control is not installed" should be a hard refusal.

---

# WHAT I WOULD SHIP

**Neither A nor B as proposed. Ship C now, then A as a gated phase 2.**

The reasoning is operability, not security theory:

1. **C is strictly beneficial and cannot break anything.** 881 of 982 state files are other-readable *today*, including 344 verbatim pre-edit file bodies at mode 644 — and `projects/`, the directory the user actually wants encrypted, is already 700. The measured hole is beside the one being discussed. A one-line `umask 077` in `entrypoint.sh` plus a recursive mode reconciliation closes it permanently and forward-compatibly. No design here should ship without that, and it needs none of them.

2. **A and B both put new machinery in front of `entrypoint.sh:195`.** That single line is CCY's entire persistence mechanism. Replacing it doubles as the highest-blast-radius edit in the repo, and both designs' worst case is not a leak but total session loss (A: dropped bind mount → writes into a `--rm` layer; B: sealer produced garbage → tmpfs dies with the container). Do that once, deliberately, after the unknowns are answered — not while three of them are open.

3. **The gating triage must run on the HOST first, and it decides whether A is worth building at all.** Three questions: (a) `getenforce`/`sestatus`/`getsebool -a | grep fuse` — if enforcing and no boolean grants `container_t → fusefs_t`, A is dead as designed; (b) is the project tree covered by *any* backup or sync tool (restic/borg/rclone/Syncthing/Dropbox)? Copy-exfil is the strongest scenario where encryption beats the existing LUKS FDE, and if nothing copies the tree, A's headline benefit largely evaporates; (c) benchmark `load_tail` on a 6 MB transcript over gocryptfs, because the hooks daemon does that read on every Stop hook and gocryptfs re-decrypts rather than serving page cache. Put all three probes in `Plan 00098`'s `triage.bash` — do not answer them in chat.

4. **Take B's mount assertion into A verbatim.** The two-way `findmnt` check is better than A's sentinel-token gate alone and costs nothing. Take C's `assert_scanner_can_see()` regardless of which encryption design wins. Discard B's store engine entirely.

5. **Amend C before shipping:** demote the credential scan and posture-repair checks to loud-banner advisory; keep hard refusal only for "managed-settings absent / `age` absent / recipients unusable." Add an flock around the archive-seal pass so six concurrent launches cannot half-seal the same file.

The honest framing for the user: on the operability lens, the live transcript cannot be encrypted without either owning a storage engine (B) or betting the launch path on FUSE + userns + SELinux agreeing (A) — and *neither* defends against the attacker who is actually most likely, because a launch-time key is loaded precisely when that attacker runs. C buys most of the real risk reduction for a fraction of the blast radius, and leaves the door open to A once the host has been probed.
The verdict is delivered above. Summary of what was returned to the calling script:

**Scores (operability/IaC lens):** CryptView 6, SEALSTORE 4, Blast-Radius Reduction 8.

**Recommendation:** ship C now (amended: demote the credential scan and posture-repair checks to advisory, add an flock around the seal pass), then A as a phase-2 gated on a host-run `triage.bash` answering three questions — SELinux enforcing status plus the `container_t → fusefs_t` boolean, whether any backup/sync tool actually covers the project tree, and a `load_tail` benchmark over gocryptfs. Discard B's bespoke store engine but port its two-way `findmnt` mount assertion into A.

Four repo facts were verified rather than taken from the digest: the daemon's byte-offset transcript reads (`transcript_reader.py:239-240`, `:409-427`), the permission posture (881 of 982 files other-readable; `projects/` already 700 while `file-history/` is 755), the `seboolean` precedent at `play-ftp-camera.yml:204`, and the systemd user timer precedent (`container-watch.timer`). The first two are load-bearing: the byte-offset reads are what disqualify live-file encryption, and the posture measurement shows the open hole is beside the directory everyone was proposing to encrypt.
