# Plan 00110 — adversarial review of DESIGN.md, round 2 (the 1,739-line revision)

Reviewer: Fable 5.1 (subagent). Scope: whether the fixes for r1's findings are sound, plus
the new material (§6.3, §6.5, T4.9, T5.2, T6.2b, U7, §12, open decision 7). Every claim
below was re-checked against the file on disk and the repo at the cited line.

## Verified sound — one line each

- **B1 fix.** Rate limit moved into the watcher (§6.4 step 8, off-mount log, exit 0);
  systemd limits demoted; the `systemd.path(5)` quotes are accurate. §6.5's heartbeat is a
  `.timer`, not path-triggered (T4.5), and the reader distinguishes *stale heartbeat* ("daemon
  not running") from *fresh heartbeat with `path_unit: failed`* ("wedged, here is the
  command"). T4.9 asserts the second state and that it is neither timeout nor `fail`. Sound.
- **B4 fix.** `run.bash:2632-2634` forwards `RUN_BASH_PROVISIONING_PROFILE` verbatim as
  `-e provisioning_profile=…` with no validation (the only other mentions, `:777` and `:831`,
  are help text), so `play-AA-preflight-sanity.yml:55-58` fails play 1; `:2678-2683` then
  `hl_abort`s headless with the exit code. Propagation for optional plays is `:698-701`, as
  now cited. The secrets row is correctly downgraded; the count now matches the nine rows.
- **B5 fix.** §5.3 names both artefacts with roles and cites the prior art's own header;
  T5.2 owns the boot medium and the squashfs-delivery decision. The LUKS observation is
  correct: `ks.cfg:359-360` and `:377-378` both write `--encrypted --luks-version=luks2 --passphrase=` on every branch, so the shipped kickstart cannot install unattended.
- **U3/U6 quotes** match the upstream text I fetched in r1. **S15** fixed with measured
  per-tree hashes (§4.3:566-582). **§12** is accurate and correctly broader than §5.2.
- `cache=unsafe`, `lazy_refcounts`, reflink, no per-run sha256, `--boot uefi`,
  `--cpu host-passthrough`, `MODE_refresh-base=deny`, `passed >= 1` + `max_skipped`,
  quarantine retention: all taken correctly.

---

## BLOCKING

### B6, B7, S13, S14 from the r1 addendum are NOT in this revision

The brief says they were taken. The file says otherwise — grep for `degraded`, `guest-seen`,
`play-AB`, `base.kind`, `server-fast-44` returns nothing, and the governing text is unchanged:

- **B6 — `unknown` still degrades to `current` on a partial outage.** §4.4:595 still defines
  `unknown` as "**no** freshness signal could be read at all" and §4.5:637-639 still says "a
  partial outage still resolves through the TTL backstops". With `artefact_identity`
  unreadable and the revision readable, `reinstall` cannot fire (identity cannot "differ"),
  so the base is `current` for up to TTL-R. T1.2's "`unknown` never collapses into `current`"
  test passes against this because the collapse is spelled "backstop".
- **B7 — the refresh gate still trusts the guest's mirror.** §4.4:605-612 still records
  "the observed revision" (the probe's, from `dl.fedoraproject.org`) as `last_upgraded` after
  a `dnf -y upgrade` that resolved via metalink to whichever mirror the guest was handed. A
  lagging guest mirror → "changed no packages" → stale base marked current until the
  revision moves again. The §4.4:617-628 mirror paragraph still guards probe-vs-probe, which
  is not the hole.
- **S13 — the extra refresh boot is still there.** T6.2:1638-1640 and §4.4:607-609 keep
  "boot rw + `dnf -y upgrade`, then re-flatten only if the transaction changed packages",
  while `play-AB-dnf-upgrade.yml:72-75` (`scope: general`, play 2 of `playbook-main.yml`)
  already runs `dnf name: "*" state: latest` in every scenario. The run *is* the probe.
- **S14 — the fast/full split is still unbound in the data.** §3.5:400 still says
  `evidence.base.profile` distinguishes the bases; §6.6:1023 still shows
  `profile: "server", name: "server-44"` for both. Nothing stops `server-full-provision`
  silently running on the `server-fast` base.

These four are the freshness-policy and evidence-binding fixes; without them the policy has
a fail-open path and the split is prose.

### B8 — The HMAC fix cannot work as specified (§6.6 rule 11, §6.4 step 12, T4.6, T4.7, §10)

An HMAC is verified with the **same** secret that signed it. §6.6:1110-1113 puts the key in
`~/.config/vmtest-bridge/<slug>/response.key`, "host-only, never on the mount", and then has
`scripts/vmtest-request.bash` — which runs **inside the container** — treat "an absent or
non-verifying signature as `forged`". The container has no key, so it can verify nothing;
if it is given the key (mount, image, env), the sandbox can forge at will. One of the two
must be true, and either way rule 11 as written is not implementable. Fix: make the
signature a **host-side** property — `vmtest verify <response>` on the host, and the
heartbeat/audit log as the verdict of record (which §6.6:1096-1098 already says) — and have
`vmtest-request.bash` print *"signature not verifiable from inside the sandbox"* rather
than claim a `forged`/verified distinction it cannot make. An asymmetric signature does not
rescue it: the public key would live on the mount, where the sandbox can swap it.

### B9 — The spool defence (§6.3) is incomplete in three places

1. **`diagnostics/` is missing from the defence table** (§6.3:883-890) yet §6.5:1010 has a
   **timer** writing `diagnostics/bridge-heartbeat.json` there on a fixed interval —
   unconditionally, with no request needed. That is the most attractive symlink target in
   the spool: `ln -s ~/.bashrc diagnostics/bridge-heartbeat.json` and wait. Add the row and
   make the heartbeat writer go through `spool.py`.
2. **`O_NOFOLLOW` only governs the final path component.** §6.3:885 and §6.4 step 1 say
   "openat a pinned directory fd for the spool root; reject if `O_NOFOLLOW` … fails". A single
   `os.open("<checkout>/untracked/vmtest-bridge", O_NOFOLLOW)` still follows a symlink at
   `untracked/`. The walk must be **component-wise** from a trusted root (`%f`, which systemd
   hands the unit): `openat(dirfd, component, O_PATH|O_DIRECTORY|O_NOFOLLOW)` per step, or
   `openat2` with `RESOLVE_NO_SYMLINKS|RESOLVE_BENEATH` (via `ctypes`; not in the stdlib).
   `realpath` containment is advisory and racy — say so, and do not list it as a control.
3. **Who holds the fds during a run is unspecified.** The watcher is a oneshot; the
   transcript, `console.log`, `heartbeat_at` refreshes and the final response are written by
   the **scope** over tens of minutes. Either the scope inherits per-directory fds pinned at
   claim time and uses only `*at()` calls against them for the run's lifetime, or every one of
   those writes re-resolves a sandbox-controlled path. T4.2's attack list should include
   "directory replaced *after* claim, before the final response".

---

## SHOULD FIX

- **S16 — `server-optional-playbook-fails` will skip, not fail, on a server run**
  (§8:1291-1293). Every hardware-specific play with a hardware assert is `scope: gnome`
  (`play-ipu6-webcam.yml`, `play-displaylink.yml`, `play-musiccast.yml`) and the scope guard
  `meta: end_play`s on the server profile (`play-musiccast.yml:46-47`). `play-nvidia.yml` is
  `general` but its first assert is *"MOK password is in vault"* (`:203-207`) — a vault
  failure, not a hardware one. So on a server scenario the play ends cleanly and the run
  exits 0; the "runs and fails" claim is row 14 again. Make it a **desktop** scenario using
  `play-ipu6-webcam.yml:63-67` (`intel_ipu6` not loaded → assert fails), or use `nvidia` on
  server and label the failing cause honestly.
- **S17 — The new "secret files unlinked; no `ssh-agent`" row (§8:1272) is vacuous on the
  default path**, by the same reasoning §8:1298-1304 just applied to the secrets row. With
  `RUN_BASH_GITHUB_ACCOUNTS=none` there is no token file and no passphrase file, and
  `hl_ssh_agent_start` never runs, so "no `ssh-agent` survives" is true of a run that never
  started one. Only the vault-password-file unlink is real on the default path; the rest is
  `server-github-token` only. Narrow the row.
- **S18 — A sandbox `touch` loop can still wedge the bridge.** `TriggerLimit` is "enforced
  before the service activation is enqueued" (§6.4:970-971 quotes it), so a loop of
  filesystem events in `requests/` reaches any finite `TriggerLimitBurst` before the watcher's
  step-8 limit ever runs. §6.5 makes that visible; it does not prevent it. Set
  `TriggerLimitBurst=0`, keep `StartLimit` as the only breaker, and have each oneshot
  activation **drain every pending request** (unspecified today; also cheaper), so event
  bursts collapse into one run. State the residual "a hostile loop needs a human `reset-failed`"
  in §6.7.
- **S19 — §3.5:393 "the repo's own installer shape" overstates** now that §5.3 drops LUKS,
  drops the interactive `%pre`, and delivers the squashfs by cdrom instead of the `FDINST`
  partition. Say "a derived, non-interactive kickstart sharing the shipped partition layout
  minus LUKS", add `luks-dropped` and `kickstart-derived` to `evidence.divergences`, and add a
  QA check that the shared stanza (`part /boot/efi`, `part /boot`, the btrfs subvolumes) in
  `ks.cfg` and `ks-vm-desktop.cfg` still agree — two kickstarts will drift silently otherwise.
- **S20 — Heartbeat contract gaps (§6.5).** The staleness threshold is not related to the
  timer interval (state "stale = older than 3× interval"), and the heartbeat file sits in the
  sandbox-writable spool, so it is forgeable; harmless (self-deception only) but say so, and
  do not describe it as something the reader can trust more than a response.
- **S21 — The pinned `baseurl` (§7:1154-1157) recreates B7.** `dl.fedoraproject.org` is the
  master Fedora asks not to be used for bulk pulls; any other single mirror can lag, which is
  exactly the hole B7 names. Recommended shape: metadata (`repomd.xml`) from the canonical
  host, RPMs from the host cache, mirror only on a miss, and record the **guest-seen**
  revision in `base.json`.
- **S22 — virtiofs under `qemu:///session`** needs unprivileged `virtiofsd`; add it to the
  U2 probe rather than assuming the cache mount works rootless.
- **T6.2b judgement: sound, not theatre — with three conditions.** A cached run genuinely
  stops proving mirror/metalink availability and package presence on a live mirror (it does
  *not* mask repo-definition errors, because metadata is still fetched). To keep it from
  being a scenario nobody runs: (1) it is a distinct scenario id with its own `planned`;
  (2) "periodically" is made concrete — the T6.3 nightly report flags *cold run overdue* after
  N days or after any base refresh; (3) every cached run's `evidence` names the last cold-pass
  `run_id`, so a green cached run shows what it is leaning on.

## NITS

- N11 — §6.3:883 header says "at every use"; the table rows for `archive/<run_id>/` say
  "refuse if the run-id dir already exists" but the run id is host-generated, so a collision
  can only mean the sandbox pre-created it — say that is the attack being refused.
- N12 — §0:101-103 explains why U3/U6 numbers are not reused. Good; keep.
- N13 — §6.6 rule 03 says `max_skipped` is per scenario in the manifest; §3.5's retention
  note and T6.4 mention failed-build retention — neither says where failed *runs* of
  `server-fast` go. One sentence.

---

## Open decision 7 — judgement

Option **2** (push a branch with the flag on, pin that commit) is the only one that leaves
§3.4 untouched, and the recommendation is sound with two riders: the branch must be on the
**public** remote (the guest clones over HTTPS), and `evidence.repo.branch` will not be `F44`,
so add a `tested-on-test-branch` divergence naming the base commit it was cut from. Option
**1** does break the pinned-commit guarantee — the tested tree is `commit + diff` — and is
acceptable only as the recorded fallback it is presented as. Option **3** is closer than the
design says: `entrypoint.sh:362` reads `${CCY_CHILD_CLAUDE:-}` *after* sourcing `ccy.env`, so
an environment value already works inside the container; what is missing is the launcher
forwarding it (`claude-yolo:3048-3061` enumerates the `-e` list and `CCY_CHILD_CLAUDE` is not
in it), and 00092's PLAN.md lists "a host launcher flag" as an explicit **Non-Goal**. So
option 3 is a one-line launcher change that contradicts a recorded decision of another plan
— the design should say that rather than "not this plan's to make".

---

## Verdict

**One more round, and a narrow one.** Nothing new is architecturally wrong; the problems are
that four accepted findings never reached the file (B6, B7, S13, S14 — a diff of the four
governing paragraphs would show it), one fix is not implementable as written (B8 — the HMAC
verifier has no key), and the security fix has three coverage gaps (B9). Those six are the
blocking list; everything under SHOULD FIX can be taken during implementation without
reopening the design. The bridge liveness fix (B1), the failure-propagation scenarios (B4),
the desktop boot medium (B5), the settled-from-docs section and §12 are correct and should
not be touched again. Once B6–B9 land, stop looping and build Phase 1.
