# Plan 00066: ftp-camera on untrusted WiFi — retry-loop triage + hotspot IaC gap

**Status**: In Progress
**Created**: 2026-07-29
**Owner**: joseph
**Priority**: High

## Overview

Two `ftp-camera` failures reported from an Airbnb WiFi network, both needing
triage and fix:

1. **`--async-copy` never progresses past the first image.** The camera
   (192.0.2.64 here; real LAN addresses are replaced with RFC 5737
   documentation addresses throughout, per `CLAUDE/ExampleValues.md`)
   connects and authenticates repeatedly against the laptop (192.0.2.12),
   and every transfer vsftpd sees completes cleanly — but the *same* frame
   (`DSC06824`) is re-uploaded roughly six times over eight minutes and no
   second frame ever arrives.

2. **`--hotspot` fails hard**: `ERROR: NetworkManager connection 'Hotspot' not found`. The fallback path for an untrusted network is therefore unavailable
   exactly when it is most needed.

A third, smaller defect appeared in the same session output:
`find: '/srv/ftp-camera/.cache': Permission denied`, printed twice — the
landing-zone advisory and the end-of-session upload summary both walk the
upload tree as the invoking user and hit a subdirectory they cannot read.

Issue 2's **IaC gap** is established by reading the source and is fixed in
this plan — the play requires a profile it never creates. Why the profile is
missing *on this machine* is a separate, still-open question (see the
correction below); the fix does not depend on the answer.

Issue 1's cause is **not confirmed at all**; this plan builds the triage to
establish it and only then applies a fix.

**No triage has been run at the time of writing.** Everything above and below
is derived from reading the repo source and from the terminal output the owner
pasted. Nothing in this plan has touched the host.

## Context & Background

### Confirmed facts (from the reported session output)

| #   | Fact                                                                                                                                                                               | Source                                            |
| --- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------- |
| F1  | Mode was `async-copy`, hotspot off, viewer off                                                                                                                                     | startup banner                                    |
| F2  | Camera reached the FTP control channel repeatedly — ~10 `CONNECT` + `OK LOGIN` pairs in 8 minutes                                                                                  | vsftpd log lines                                  |
| F3  | **Zero `FAIL LOGIN`, zero `FAIL UPLOAD` lines** in the whole session                                                                                                               | the wrapper's awk prints both in red; none appear |
| F4  | Uploads *completed* — `sort` only fires off vsftpd's `OK UPLOAD` line, and it fired 8 times                                                                                        | `async_monitor_loop()`                            |
| F5  | Every one of those 8 sorts was the **same frame**, `DSC06824` (.JPG x4, .ARW x4)                                                                                                   | sort/copy lines                                   |
| F6  | `CONNECT` to `OK LOGIN` latency ranged 2 s to 15 s                                                                                                                                 | timestamps                                        |
| F7  | A late `DSC06824.ARW` arrived with **no readable EXIF date** and was left in the upload root                                                                                       | `skip … (no EXIF date, left in place)`            |
| F8  | Camera sessions **overlap** — e.g. `CONNECT` 22:53:27 while the 22:53:06 session was still live                                                                                    | timestamps + distinct pids                        |
| F9  | `/srv/ftp-camera/.cache` is unreadable by the invoking user                                                                                                                        | `find: … Permission denied`, printed twice        |
| F10 | No NetworkManager profile named `Hotspot` exists **at the time of the failure**. This says nothing about whether one existed previously — see the correction under "Issue 2" below | `--hotspot` fail-fast message                     |

**F3 is the load-bearing fact.** The FTP layer never failed once. "The Airbnb
WiFi is too weak/lossy" does not survive contact with the log: a lossy link
would produce aborted data connections and `FAIL UPLOAD` entries. Something is
causing the camera to *re-send frames that already transferred successfully*.

### Hypotheses (NOT confirmed — do not act on these as fact)

- **H1 (primary): async mode moves the file out of the chroot before the
  camera can verify it, so the camera concludes the upload failed and
  re-sends.** `sort_one_file()` runs `sudo mv` the instant `OK UPLOAD` is
  logged, relocating the file from the chroot root into
  `photos/YYYY/MM/DD/{JPG,RAW}/`. A camera that issues `SIZE`, `LIST`, or
  `MLSD` on the just-stored path immediately afterwards would get "no such
  file". Fits F4, F5, F8 and predicts exactly the observed
  upload-succeeds-then-repeats loop. Also explains F7: overlapping retries
  (F8) `STOR` the same path concurrently, interleaving writes into one
  corrupt file.
- **H2: Sony FTP client-side transfer timeout.** The camera gives up waiting
  for the `226` completion response and re-queues the frame. Would also fit
  F5/F6, but does not explain why vsftpd logged the transfer as OK.
- **H3: `.cache` (F9) is unrelated noise** — cosmetic stderr leakage from
  `warn_if_landing_stale()` and the cleanup summary, not a transfer fault.
- **H4: control-channel latency (F6) is vsftpd reverse-DNS / PAM delay**,
  not radio congestion.

H1 and H2 are cleanly separable by a single decisive probe (see Phase 1).

### Issue 2 (hotspot) — what is actually established, and what is not

**Established by reading the source** (no host data needed):

`ftp-camera --hotspot` requires a NetworkManager profile named by
`HOTSPOT_CONNECTION` (default `Hotspot`) to **already exist**, and fails fast
when it does not. `play-ftp-camera.yml` only ever *tunes* that profile:

```yaml
- name: Apply band=bg, channel=6, channel-width=40MHz to the hotspot profile
  when: ftp_camera_hotspot_connection in ftp_camera_nm_conns.stdout_lines
```

…and when the profile is absent it prints a `debug` message telling the user
to go and create it by hand in GNOME Settings. So the feature depends on a
**manual system change**, which this repo prohibits outright
(`CLAUDE/InfrastructureAsCode.md`). That is a genuine IaC gap regardless of
anything else, and the fix (the play creating the profile) stands on it alone.

**NOT established — a correction.** An earlier revision of this plan went on
to assert that the profile "has therefore never existed on this machine" and
that `--hotspot` "has never been able to work". That does not follow from F10
and is contradicted by the owner, who reports it working on this machine and
certainly on others. F10 says only that the profile is absent **now**.

The reverse is in fact more likely: GNOME's Wi-Fi Hotspot quick-settings
toggle creates a profile called exactly `Hotspot` — the play's own comment
says so and depends on it. Anyone who has used that toggle once would have a
working `--hotspot` until something removed the profile.

That reframes the open question. It is not "why did this never work" but
**"what deleted a profile that used to exist"** — a `nmcli connection delete`,
a GNOME hotspot toggle-off on some versions, a NetworkManager upgrade, or a
`/etc/NetworkManager/system-connections/` clean-up. Triage now probes for
this (see Task 1.5); the answer does not change the fix, but a profile that
silently disappears once can disappear again, and declaring it in Ansible is
only a durable remedy if nothing is actively deleting it.

## Goals

- Establish the grounded cause of the `--async-copy` retry loop with a
  re-runnable plan-local triage script — no guessing.
- Make `--hotspot` work out of the box by creating the NetworkManager AP
  profile in Ansible instead of instructing the user to click through GNOME.
- Remove the `/srv/ftp-camera/.cache` permission noise at its source.

## Non-Goals

- Tuning the Airbnb network, or any change to router/AP configuration.
- Reworking the sort taxonomy, rclone push/copy paths, or the viewer.
- Building any mechanism that automates a disruptive action on the user's
  behalf (see `CLAUDE/AgentNotes.md`).

## Tasks

### Phase 1: Triage — establish the cause of the retry loop

- [x] ✅ **Task 1.1**: Add `triage.bash` — read-only fact gathering into
  `untracked/reports/`: NIC/IP/route state, vsftpd runtime config, upload
  dir ownership/ACL/SELinux, the `.cache` entry (F9), NM profile
  inventory (F10), and a parse of the recent vsftpd log counting
  `OK UPLOAD` per filename (quantifies F5).
- [x] ✅ **Task 1.2**: Add `ftp-camera --debug-ftp` — appends
  `log_ftp_protocol=YES` to the runtime vsftpd config so the log records
  every FTP verb the camera sends. This is the decisive H1-vs-H2 probe:
  it shows whether the camera issues `SIZE`/`LIST`/`MLSD` after `STOR`
  and what reply it gets. Runtime-only; never touches `/etc`.
- [ ] 🔄 **Task 1.3**: Run the A/B on the HOST — one short session in default
  mode (files stay in the chroot root) vs `--async-copy` (files are moved
  away). If default mode progresses past the first frame and async does
  not, H1 is confirmed and H2 is refuted.
- [ ] ⬜ **Task 1.4**: Record the verdict in the journal, promoting the
  surviving hypothesis to a fact with its evidence.
- [ ] 🔄 **Task 1.5**: Establish what happened to the `Hotspot` profile.
  The owner reports it working before, so it is not a case of "never
  existed". Probe the on-disk connection store
  (`/etc/NetworkManager/system-connections/`), timestamps of any
  surviving files, and the NetworkManager journal for an add/delete
  event. If something is actively deleting the profile, declaring it in
  Ansible fixes today's failure but not tomorrow's.

### Phase 2: Fix the retry loop (gated on Phase 1 verdict)

- [ ] ⬜ **Task 2.1**: Implement the remedy the verdict selects. If H1
  confirms, the candidate is to **hard-link** rather than `mv` during
  async sort: the frame stays visible at its original chroot path for the
  camera to verify while also appearing in the sorted tree (same inode,
  no extra space), with the root-level link removed at session end.
- [ ] ⬜ **Task 2.2**: Run QA: `./scripts/qa-all.bash`.
- [ ] ⬜ **Task 2.3**: (HOST) deploy + confirm a multi-frame burst transfers
  once each.

### Phase 3: Hotspot IaC gap (root cause confirmed — no triage gate)

- [x] ✅ **Task 3.1**: Create the NM AP profile in `play-ftp-camera.yml`
  (`nmcli connection add`), idempotently, with `ipv4.method=shared`,
  band `bg`, channel 6, 40 MHz width, autoconnect off.
- [x] ✅ **Task 3.2**: Add `ftp_camera_hotspot_ssid` + vaulted
  `ftp_camera_hotspot_password`, with a length assertion (WPA2 PSK
  minimum 8 characters).
- [x] ✅ **Task 3.3**: Repoint `ftp-camera`'s `--hotspot` failure message at
  the vault command + playbook rather than at GNOME Settings.
- [x] ✅ **Task 3.4**: Document the single-radio consequence — on Intel CNVi
  the AP cannot run concurrently with the WiFi client, so the laptop
  loses internet while the hotspot is up; `--async-copy` still works
  because rclone VFS caches locally and drains once the client link
  returns.
- [x] ✅ **Task 3.5**: Run QA: `./scripts/qa-all.bash`.
- [ ] 🔄 **Task 3.6**: (HOST) vault the PSK, run the play, confirm
  `ftp-camera --hotspot` brings the AP up and the camera associates.

### Phase 4: Landing-zone permission noise

- [ ] ⬜ **Task 4.1**: Using the Task 1.1 facts about `.cache` ownership and
  mode, fix the cause (ownership/ACL in the playbook, or exclude the path
  in the walk). **Not** by redirecting stderr to `/dev/null` — that is an
  error-hiding pattern this repo prohibits.
- [ ] ⬜ **Task 4.2**: Run QA: `./scripts/qa-all.bash`.

## Technical Decisions

### Decision 1: Triage before fixing issue 1, fix issue 2 immediately

**Context**: Both issues were reported together and both need a fix.
**Decision**: They are at different evidence levels and must be treated
differently. Issue 2's cause is provable by reading the playbook and wrapper —
no host data needed, so it is fixed in this plan directly. Issue 1's cause is
*inferred* from a log excerpt; H1 is strong but a strong hypothesis is still a
hypothesis, and shipping a rewrite of the async sort path on an unconfirmed
cause is precisely the failure mode `CLAUDE/AgentNotes.md` records from Plan
00062\.
**Date**: 2026-07-29

### Decision 2: `--debug-ftp` writes to the runtime config, not `/etc`

**Context**: `log_ftp_protocol=YES` is the decisive probe but is far too noisy
to leave on permanently.
**Options considered**: (a) template `files/etc/vsftpd/vsftpd.conf` with a
debug variable — persists, needs a playbook re-run to turn on and off;
(b) a wrapper flag appending to `/run/vsftpd-camera.conf`.
**Decision**: (b). The wrapper already synthesises that runtime config to
inject `pasv_address`, so the mechanism exists; `/run` is tmpfs so the setting
cannot survive a reboot or leak into the deployed state. The wrapper itself is
Ansible-deployed, so this stays inside the IaC boundary.
**Date**: 2026-07-29

## Success Criteria

- [ ] The `--async-copy` retry loop has a **confirmed** cause recorded with
  its evidence, not an asserted one.
- [ ] A multi-frame burst transfers each frame exactly once.
- [ ] `ftp-camera --hotspot` works on a machine that has never opened GNOME
  Settings, straight after `play-ftp-camera.yml`.
- [ ] No `Permission denied` noise in normal `ftp-camera` output.
- [ ] `./scripts/qa-all.bash` passes.

## Risks & Mitigations

| Risk                                                                     | Impact | Probability | Mitigation                                                                         |
| ------------------------------------------------------------------------ | ------ | ----------- | ---------------------------------------------------------------------------------- |
| H1 is wrong and the real cause is camera-side timeout                    | M      | M           | Phase 1 A/B separates them decisively before any fix is written                    |
| Hard-link remedy breaks the `--push`/`--prune` paths (same inode twice)  | M      | M           | Root-level links removed at session end; verify `--push` ships each frame once     |
| Creating an AP profile in Ansible clashes with a user-made GNOME profile | L      | M           | Add only when absent; tune when present — existing behaviour preserved             |
| Hotspot drops the laptop's internet mid-session (single radio)           | M      | H           | Document it (Task 3.4); rclone VFS caches locally and drains later, so no dataloss |

## Delivery & Milestones

- Plan scaffolded; issue 2 IaC gap established from source (no host triage run)
