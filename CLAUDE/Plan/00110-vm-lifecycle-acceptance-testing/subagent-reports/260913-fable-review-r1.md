# Plan 00110 — adversarial review of DESIGN.md, round 1

Reviewer: Fable 5.1 (subagent). Target: `DESIGN.md` at 1,085 lines plus
`JOURNAL/00110-Journal-26-09-13.md`. Every upstream fact below was re-fetched with `curl`
from this container on 2026-09-13; every repo citation was re-read at the stated line.

## What checks out (one line each, then move on)

- §4 signals, all byte-for-byte: `releases/44/COMPOSE_ID` = `Fedora-44-20260422.1`;
  `development/45` = `Fedora-45-20260913.n.0`; rawhide `Fedora-Rawhide-20260913.n.0`;
  `.treeinfo` is 1,427 B with `boot.iso` = `bd285201…d7067e` and `build_timestamp 1776865868`;
  `releases.json` is 135,608 B / 378 entries and its netinst hash equals `.treeinfo`'s
  `boot.iso`; `Fedora-Cloud-Base-Generic-44-1.7.x86_64.qcow2` is 583,729,152 B, HEAD returns
  200 with that `Content-Length`, and its sha256 `28680fe5…f90b7f` matches
  `Fedora-Cloud-44-1.7-x86_64-CHECKSUM`; updates `repomd.xml` revision `1789172543`; Bodhi
  F44 current / F45 pending / F46 pending.
- `releases/44/Workstation/x86_64/os/.treeinfo` is 404 and the `x86_64/` directory holds only
  `iso/`. No Workstation/GNOME respin in `pub/alt/live-respins/` (Budgie, CINN, COSMIC, LXDE,
  LXQT, MATE, SOAS, XFCE, i3 only).
- Repo citations hold: `run.bash:9` v1.19.0, `:877-879`, `:1993-2004`, `:684-686`,
  `:487-515`; `ansible.cfg:42`; `desktop.yml:25-26`; `claude-yolo:1946`; `.claude/ccy/mounts`;
  `untracked/.gitignore`; `CLAUDE/Plan/.gitignore`; `play-toolbox-install.yml:31`;
  `ks.cfg:13-18, 356, 372, 429, 690-691`; `setup-netinstall-boot.bash:793-810`;
  `scripts/qa-deployed-drift.bash` exists; the host-action-bridge article resolves (301 → 200).
- §3.3 (external backing chain, depth 1, base read-only) and §4.4 (`unknown` blocks) are
  correct decisions and correctly argued.

## The vacuously-green extension gate — CONFIRMED, and it is a live repo defect

`helpers/gnome/extension_state.py:58-62` returns `Verdict.SKIP_NO_SESSION` whenever
`session_available` is false. `Classification.is_failure` (`:41-42`) is true only for
`FAIL_VERSION`/`FAIL_ERROR`, so `exit_code` (`:45-46`) is `0`. `helpers/gnome/verify_extension.py:111-113`
then prints `EXT-OK [skip_no_session] …` and returns 0. `_live_state` (`:69-78`) treats any
non-zero `gnome-extensions info` exit as "no session", so a *missing* `gnome-extensions`
binary, a broken D-Bus, or an extension UUID that does not exist at all also lands in the
`EXT-OK` bucket. `play-gnome-shell-extensions.yml:155` documents this as intended
("no-session both exit 0 (EXT-OK …)").

That is a `gnome`-scoped play whose only live-state assertion passes on any box with no
GNOME session, and it is independent of this design. It belongs in a repo issue/plan of its
own: either fail when `provisioning_profile == desktop` and no session is reachable, or emit
a distinct `EXT-SKIP` marker that the play counts and reports, never `EXT-OK`.

---

## BLOCKING

### B1 — The rate-limit story is false: hitting the start limit kills the path unit (§6.3:666-669, §10:1064)

`systemd.path(5)`, verbatim: *"the error condition that the start rate limit is hit is
propagated from the service unit to the path unit and causes the path unit to fail as well,
thus ending the loop."* Once `StartLimitBurst=50` in `StartLimitIntervalSec=60` is reached,
`vmtest-bridge@<slug>.path` enters `failed` and **stops watching**. No oneshot runs, so nobody
writes the `rejected` response §10 promises ("a rejected request still gets a rejected
response naming the limit"). From the sandbox this is indistinguishable from "watcher never
installed", and the only remedy is a human running `systemctl --user reset-failed`.
`TriggerLimitBurst=` (default 200 per 2 s) has the same terminal semantics ("placed into a
failure mode, and will not watch the paths anymore until restarted").

Fix: the rate limit must live *inside* the watcher (count claims in the last N seconds from
the off-mount audit log, write a `rejected` response, exit 0), with the systemd limits set
high enough to be a last-resort loop breaker only, and `lab-status`/docs must state that a
`responses/` file never appearing means the path unit is dead and name the host command.

### B2 — The host writes into a sandbox-controlled tree with no symlink defence (§2.1:144-145, §3.2:260, §6.1:602-609)

Every spool directory — `tmp/ requests/ processing/ responses/ archive/ quarantine/` — is
under the bind-mounted checkout, so the sandbox can `rm -rf responses && ln -s ~/.config responses` (or point `archive/<run_id>/` at `~/.ssh`, `~/.bashrc`, the policy directory…).
The watcher's `mktemp` + `mv -f` and the run's transcript write then land wherever the link
points, **as the host user**. Ownership checks are useless because rootless podman maps
container root onto the host uid. That is an arbitrary-file-truncate/overwrite primitive on
the host — precisely the boundary the bridge exists to hold.

Fix: the watcher `realpath`s each spool directory, refuses (`-L` or realpath outside the
spool root → log off-mount, exit) rather than quarantines, and opens every file it creates
with `O_NOFOLLOW|O_EXCL`. That is not expressible safely in bash `mv -f`; it wants a TDD'd
`helpers/vmtest/spool.py` per `playbooks/CLAUDE.md` "Complex Logic → TDD Helper".

### B3 — Request-body TOCTOU is unspecified (§6.3:650-661)

Steps 3–4 validate; step 7 claims (`mv` to `processing/`); step 9 builds argv. `processing/`
is still on the shared mount. Nothing says the body is read exactly once into memory, that
argv is built from *that* buffer, or that the filename verb must equal the body verb. As
written, "re-read the claimed file" is a legal implementation and the sandbox can swap the
argument after validation. Also: parsing JSON in a bash watcher needs `jq`, which is not in
the T2.1 package list. Specify: one `read()`, validate the buffer, claim, dispatch from the
buffer, never re-open; filename verb ≠ body verb → reject.

### B4 — The 00063 discharge map overclaims, in the repo's own documented defect class (§8:837-847)

1. **§8:839 `server-optional-play-missing`** hits `run.bash:684-686`, which is argument
   validation that aborts *before any playbook runs*. The criterion (00063 Task 2.6 / D7) is
   "a **failed** main or optional playbook makes a headless run exit non-zero" — failure
   propagation, not preflight rejection. A missing name does not exercise the code path it
   vouches for (`AgentNotes.md` row 14). Replacement using only supported inputs:
   `RUN_BASH_PROVISIONING_PROFILE=<unrecognised>` so `play-AA-preflight-sanity.yml:55-58`
   fails play 1 of `playbook-main.yml`; for the optional half, request a
   `hardware-specific/` play whose own hardware assert fails on a VM.
2. **§8:840 "No secret bytes enter the environment or user-data — dischargeable, genuinely
   new."** The default scenario carries no token and no SSH passphrase
   (`RUN_BASH_GITHUB_ACCOUNTS=none`), so the in-guest grep proves nothing about the two
   secrets the criterion is about. It is dischargeable only by the opt-in token scenario, and
   the map must say so.
3. **§8:845-847 arithmetic.** "Four of the eight criteria become machine-provable by the
   default scenarios" — the text names three (end-to-end, failed playbook, secrets), and two
   of those are the ones disputed above.

### B5 — The desktop base has no installer boot medium (§5.3:524-529, §7:769-771, T5.2:1013-1015)

`liveimg` is an Anaconda directive; something has to boot Anaconda. The Workstation Live ISO
boots to a live GNOME session — it does not run a kickstart-driven unattended install. The
repo's own prior art boots the **netinst** kernel/initrd and hands `liveimg` the extracted
squashfs (`setup-netinstall-boot.bash:793-810`, `ks.cfg:429` via a `%pre`-mounted partition).
The design never fetches `boot.iso` (1,217,329,152 B), never uses `--location`, and never says
how `LiveOS/squashfs.img` becomes reachable inside the installer (second cdrom + `%pre`
mount, or an HTTP URL served from the host). The §7 fetch table is therefore incomplete and
Phase 5 as written cannot be executed.

---

## SHOULD FIX

- **S1 — §5.3:549-560 "the real session's environment".** `systemd-run --user` runs under the
  *user manager*, whose environment holds `WAYLAND_DISPLAY`/`DISPLAY`/`DBUS_SESSION_BUS_ADDRESS`
  only because gnome-session imports them. `XDG_SESSION_TYPE`, `XDG_SESSION_ID`, `XDG_SEAT` are
  logind per-session values and a transient user unit is in **no** logind session. Any play
  reading those, and the §5.4 `loginctl show-session` assertion, see something a logged-in
  user does not. Add a Phase-5 probe (`systemd-run --user --pipe env` inside the autologin
  guest), pin the expected set, and list the residual gap in `evidence.divergences`.
- **S2 — `cache=none` + `io=native` (§7:812-813) is slower for this workload.** The cache
  mode applies to the whole backing chain, so it also forbids page-cache sharing of the
  read-only base across parallel runs and repeated boots. For a disposable overlay the
  standard choice is `cache=unsafe` (or `writeback`) — rpm/dnf are fsync-heavy. Keep
  `discard=unmap`; add `-o lazy_refcounts=on` on overlay creation.
- **S3 — Refresh cost is not the floor (§3.3:290-296, §7:789-790).** On btrfs/XFS-reflink
  (Fedora Workstation default; this repo's kickstart is btrfs, `ks.cfg:362-364`)
  `cp --reflink=always` makes "copy the base" O(1). Or boot an overlay rw and
  `qemu-img convert -O qcow2` overlay→new base in one sparse pass. Separately, hashing the
  multi-GB `base.qcow2` "before every run" (§3.2, §10:1055) is a per-run tax; verify
  size+mtime+`qemu-img check` per run and the full sha256 on refresh / `lab-status --deep`.
- **S4 — The largest win is missing: a host-side DNF/flatpak cache.** Every run re-downloads
  the full package set. A persistent caching proxy (or a `dnf` cache dir exported via
  virtiofs, or a pinned local mirror) reachable from the guest at a fixed address, injected
  via the harness and recorded in `evidence.divergences`, would dominate every other item in
  §7. Pinning a mirror `baseurl` also removes metalink flakiness from the verdict.
- **S5 — Response forging (§6.4).** The sandbox can author `responses/*.response.json`
  itself, and `scripts/vmtest-request.bash` trusts it. Not an escape, but "the agent reports
  its own pass" is the repo's defect class. Sign responses with a host-only HMAC key under
  `~/.config/vmtest-bridge/` and have the reader print the off-mount audit-log path; or state
  plainly that the off-mount `service.log` is the verdict of record.
- **S6 — The token scenario's transcript lands in the shared spool.** §3.2:260 puts every
  transcript in `untracked/vmtest-bridge/archive/<run_id>/`; §10:1058 puts a PAT in the guest.
  A host-only scenario's transcript and console log must be written off-mount
  (`~/.local/share/vmtest/runs/`) — this repo has no log scrubber (`CLAUDE/Plan/.gitignore`,
  R4) — or the design has built a PAT→sandbox channel.
- **S7 — 00092 nesting is asserted, not verified (§8:880-890).** Running `ccy --rebuild` and
  `acceptance.bash` inside a CCY container in the guest needs the launcher to start with no
  Claude credential or config import (`claude-yolo:1947` mounts `/tmp/claude-config-import`),
  and step 5 needs a synthetic token delivered through ccy's token store into PID 1's
  environment. Neither is checked; add **U7** and a probe. Also, step 5 needs
  `CCY_CHILD_CLAUDE=1` in the checkout's tracked `ccy.env`, which is commented out at every
  pushed commit — so the guest must edit a tracked file and the tested tree is no longer the
  pinned commit. Say how that is reconciled with §3.4.
- **S8 — 00063 items marked "no" that the VM can do (§8:838).** Tasks 2.2 and 2.4 leave
  "delete-after-use and ssh-agent teardown HOST-verified in Phase 3", and the `sudo -k -n true`
  NOPASSWD probe cannot run as `nobody`. The VM can assert secret files unlinked after use,
  no `ssh-agent` left, and `RUN_BASH_*_FILE` values absent from `/proc/*/environ`. Also
  00063's Non-Goals and Task 1.6 still say `none` is deferred, though Plan 00082 shipped it
  (`run.bash:502-515`, commit `b7a5800`) — T7.2 should correct that stale text.
- **S9 — Rule 03 of §6.4 admits an all-skipped pass.** `passed + skipped == total` is
  satisfied by `passed=0, skipped=total`. Require `passed >= 1` and a per-scenario
  `max_skipped` in the manifest, or row 13 recurs at the verdict layer.
- **S10 — Duration words in a plan-directory document (§7:765-778).** "tens of minutes", "a
  few minutes" etc. are time estimates; the `R-PLAN-TIME-ESTIMATE` hook will deny them when §9
  is lifted into `PLAN.md` and the table is referenced. Keep the mechanism/why columns; drop
  the duration column until measured values exist.
- **S11 — §0:35 / T2.1:956-959 versions come from mdapi**, which §4.2 itself says reports
  updates-testing NEVRAs. Right now `qemu-kvm 10.2.2-1.fc44` is `repo: updates-testing`,
  `guestfs-tools`/`edk2-ovmf`/`swtpm-tools`/`lorax`/`xorriso` are `updates`. Existence is
  proven; the version list is not what a default install gets. Drop the versions.
- **S12 — U3 and U6 were settleable from documentation and should not be UNVERIFIED.**
  `%f` is documented in `systemd.unit(5)` as "the unescaped instance name with `/`
  prepended", the exact inverse of `systemd-escape --path`, and `[Path]` directives resolve
  specifiers (systemd `path.c` uses `unit_path_printf`); so `PathModified=%f/untracked/…` is
  correct by construction — keep the probe as confirmation, not as the source of truth.
  `--cloud-init` suboptions are in the upstream virt-install man page (`user-data=`,
  `meta-data=`, `network-config=`, `root-ssh-key=`, `clouduser-ssh-key=`,
  `root-password-file=`, `disable=on`). U1, U2, U4 are honestly host-only; U5 is honest —
  pagure raw paths 404 for me too and comps ships only as `.zst`/`.zck`.

---

## NITS

- N1 — `AgentNotes.md` table has 20 rows (0, 0b, 1–9, 9b, 10–17), not "18 recorded
  instances" (§6.4:680).
- N2 — `play-AA-preflight-sanity.yml:52-53` → the Cloud Base sentence is at `:50-51`.
- N3 — `--boot loader=… edk2-ovmf` (§7:823) → the documented form is `--boot uefi`.
- N4 — `kvm` group membership needs a re-login; T2.2 must fail loud rather than continue in a
  session that lacks the group.
- N5 — `refresh-base` on the bridge contradicts T6.3's "an unannounced disk-churning rebuild
  is a surprise"; default `MODE_refresh-base=deny`.
- N6 — `quarantine/ never deleted` on the shared mount is a trivial disk-fill; put it under
  retention.
- N7 — `--cpu host-passthrough` is not mentioned; trivial win under KVM.
- N8 — Journal 11:52 handoff says "Nothing has been committed by this session" — fine, but
  the design's `PLAN.md` still needs the Phase 0–7 lift before any Phase-1 code lands (Plan
  Commit Rule).

---

## Addendum — the 12:10 revision (DESIGN.md now 1,252 lines)

The document changed mid-review (journal 12:10 entries; `git diff` +247/−79). Section numbers
above refer to the revised file where they still apply; the old §4.4 is now §4.5. Every
finding above survives the revision untouched — B1–B5 and S1–S12 are all in unchanged text
(the bridge, the desktop build, §8's 00063 rows 3–4, §7's cache mode). New facts checked:
all four `Fedora-*-44-1.7-x86_64-CHECKSUM` files return 200; the Server install tree exists
(`releases/44/Server/x86_64/os/.treeinfo` 200, `variant = Server`, its own `boot.iso`
`ae20c06b…` — different from Everything's, which matters for `server-full`'s identity row);
updates `repomd.xml` `<revision>` `1789172543` is a plain epoch timestamp of the compose.

### B6 — The revision made `unknown` degrade to `current` (§4.4:200-205, §4.5:245-250) — BLOCKING

Draft 1 said `unknown` = "*any* signal could not be fetched or parsed". The revision says
`unknown` = "**no** freshness signal could be read at all", and "a partial outage still
resolves through the TTL backstops". Trace the partial case: `artefact_identity` unreadable,
`package_revision` readable. `reinstall` needs identity to *differ*; unreadable cannot
differ; nothing else fires until `installed_at` is 90 days old. So the policy returns
`current` (or `refresh`) for a base whose media identity it could not check, for up to
TTL-R. That is exactly the fail-open §4.5 says it forbids, and T1.2's "`unknown` can never
collapse into `current`" test will pass against it because the collapse is spelled
"backstop". Fix: an unreadable `artefact_identity` is `unknown` (it is one HTTP GET to
`dl.fedoraproject.org`, which is also where `COMPOSE_ID` comes from, so "identity unreadable
but revision readable" is not a realistic outage shape anyway); only an unreadable
`package_revision` may fall to TTL-U, and then the response must carry
`freshness.degraded: true` and a `divergences` entry, not a bare `current`.

### B7 — The two-step refresh gate is unsound against the very mirror lag the revision added handling for (§4.4:215-222, 227-238) — BLOCKING

The probe reads the revision from `dl.fedoraproject.org` (§4.1 URLs). The guest's
`dnf -y upgrade` resolves through the **metalink redirector** to whichever mirror it is
handed — today `mirror.cov.ukservers.com` / `fedora.mirrorservice.org` (checked via
`mirrors.fedoraproject.org/metalink?repo=updates-released-f44`). If that mirror lags the
canonical host, the transaction "changed no packages", the gate says *checked and
unnecessary*, and `base.json` records the **probe's** newer revision as `last_upgraded`. The
base is now stale and provably marked current, and it stays that way until the revision
advances *again*. The "backwards revision" handling does not catch this because the
regression is between probe and guest, not between two probes.

Fix: (a) the guest must upgrade against the same canonical host the probe read
(`--setopt=updates.baseurl=https://dl.fedoraproject.org/pub/fedora/linux/updates/<v>/Everything/x86_64/`,
metalink disabled for that transaction); (b) record as `last_upgraded_revision` the revision
the **guest actually saw** (its cached `repomd.xml`), never the probe's; (c) if guest-seen \<
probe-seen, the refresh is *incomplete*, not *unnecessary* — record it as such and re-run.
Note also that the "mirror lag" paragraph as written protects a path that does not exist:
the probe never uses the redirector, so a backwards probe revision can only mean the
canonical host itself changed, which is `unknown` territory, not "retry".

### S13 — The refresh boot is redundant: the run already does the upgrade (§4.4:215-222)

`playbooks/imports/play-AB-dnf-upgrade.yml:72-75` (`scope: general`, play 2 of
`playbook-main.yml`) runs `dnf name: "*" state: latest` **in every run of every profile**.
So the "boot rw + `dnf -y upgrade` to see whether anything changes" step pays a boot and a
transaction that the scenario is about to pay again on the overlay. Cheaper and equally
sound: let the run be the probe — the guest acceptance script reports the play-AB
transaction's changed-package count and the guest-seen revision in `evidence`, and `vmtest`
schedules the re-flatten *after* the run (or lazily before the next one) only when that count
was non-zero. Zero extra boots; the revision-primary policy stops "thrashing" by
construction. This also answers the team lead's affordability question: as written, the first
run per base after each upstream regeneration (near-daily, three bases) pays an extra full
boot+DNF cycle before it starts.

### S14 — The `server-fast` / `server-full` split is not yet bound in the data (§3.5, §6.4:816-822, §8)

The separation lives in scenario ids and prose. The response's `evidence.base` still carries
`profile: "server"` and `name: "server-44"`; §3.5 says `evidence.base.profile` distinguishes
the bases, but both are profile `server`. Bind it: `base.kind: fast|full`, `base.name: server-fast-44|server-full-44`, and the manifest's per-scenario `base:` field is what
`vmtest` resolves — a `server-full-provision` request whose only available base is
`server-fast` must be `verdict: error, stage: base`, never a silent substitution. Also
§3.5's "Cadence" column mixes two axes (how often the scenario runs vs. how often the base is
rebuilt); `server-full` is *built* once per release and *refreshed* like the others — say so,
or a reader concludes it runs only on reinstall.

### S15 — `server-full`'s identity row cites the wrong tree (§4.3:167)

`server-full` is "Anaconda from the Server/Everything tree" and its identity is
"`.treeinfo` `[checksums]` + `Fedora-Server-44-1.7-x86_64-CHECKSUM`". The Server tree's
`.treeinfo` has different `boot.iso`/`install.img` hashes from Everything's; the design must
name **one** tree per base and hash that tree's `.treeinfo`, or the identity is ambiguous and
a rebuild from the other tree matches nothing.

### N9 — "detectable from a directory listing before anything is downloaded" (§4.3:179-182)

The listing is Apache HTML, not a contract; `releases.json`'s `link` already carries
`44-1.7` in a parseable form and is the source the design says it uses. Drop the listing
idea rather than add an HTML scraper to `upstream.py`.

### N10 — Bodhi `state` leaving `current` triggers `reinstall` of identical media (§4.4:204)

Nothing upstream changes when F44 becomes `archived`; a rebuild proves nothing. That
transition should produce a loud `lab-status` warning and a `divergences` entry, not a
90-minute rebuild.

---

**Verdict: needs another round.** B1–B3 change the shape of the bridge implementation (host
side must be symlink-safe, TOCTOU-safe and self-rate-limiting, not systemd-rate-limited);
B4 changes the discharge map's text; B5 fills a hole in Phase 5 that would stop the desktop
base from ever being built as written. The 12:10 revision introduced two new defects in the
freshness policy — B6 (partial outage now resolves to `current`) and B7 (the refresh gate
trusts a lagging guest mirror) — and the separate refresh boot it added is redundant with
`play-AB-dnf-upgrade.yml` (S13). The backing-chain decision (§3.3) and the
artefact-identity/revision authority rule (§4.3) are sound and can proceed unchanged.
