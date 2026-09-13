# Plan 00110 — certification pass on the frozen DESIGN.md (2,071 lines)

Reviewer: Fable 5.1 (subagent). Every claim below was checked against the frozen file and
the repo at the cited line. Verdict is at the end; nothing under SHOULD FIX or NITS blocks.

**Certified object** — verified with `git hash-object` before review, working tree clean for
the path:

```
blob   : c854880ae89e26cbc62380c9e14fef476cbbdf28
commit : 8c0759a  "Plan 00110: freeze the VM acceptance design at 2,071 lines"
lines  : 2071
path   : CLAUDE/Plan/00110-vm-lifecycle-acceptance-testing/DESIGN.md
```

Every `:line` reference below is into that blob. The author-supplied locations for the
fixed items (B8 `:1313-1333`; B9.1 `:1080`; B9.2 `:1061-1067`; B9.3 `:1084-1088`;
`TriggerLimitBurst=0` `:1163`; `play-nvidia.yml` `:1526`; §5.3a `:866`) are the passages
this report judges for correctness.

## Verdict up front

**Implementable — build it.** No finding in this pass is architecturally blocking. There is
one correctness gap in the LUKS route that must be resolved by the U8 probe *before Phase 5*
(not before Phase 1), one wedge that is narrowed but not closed, and a handful of stale
sentences left behind by four rounds of surgery. All are listed with the exact line so they
can be taken during implementation without another design round.

---

## 1. B8 — host-side verification: SOUND

§6.6 rule 11 (`:1296-1333`) now says what is true: the key is host-only, so only
`vmtest verify <run-id>` on the host can verify; the container reader reports
`signature: present, not verifiable from inside the sandbox` and cannot say `forged`. That is
the honest shape — the unverifiable check did not "move", it was removed from the place it
could never run and kept where it can.

Can the sandbox spoof or suppress the *host* verdict's delivery? No: the verdict of record is
the off-mount `service.log` (`:1324-1326`), which the sandbox cannot reach, and the request
nonce inside the signed payload defeats replay. What the sandbox *can* do is present a forged
response to its own reader and exit 0 on it — §6.6 says so plainly. The residual is the human
actually running `vmtest verify`; both tools print the command. Sound.

- NIT — `vmtest verify` needs a host-held copy of what it verifies. Rule 10 puts the
  transcript sha256 in the audit log; the **verdict and `checks` counters** should be logged
  there too, so `verify` compares against something the sandbox never touched rather than
  re-reading the spool copy. One sentence in §6.6.

## 2. B9 — the component-wise walk: SOUND, two implementation details

The walk at `:1061-1063` (`O_PATH|O_DIRECTORY|O_NOFOLLOW` per component from the `%f`
checkout root, then only `*at()` calls against pinned fds) does give `RESOLVE_NO_SYMLINKS`
semantics for this use: the components are hardcoded literals (no `..`), the parent of the
checkout is outside the mount, the sandbox cannot create mount points on the host, and a
pinned fd is immune to later renames/replacement (a sandbox that deletes and recreates the
directory leaves the host writing into an unlinked inode, which fails `ENOENT` and refuses —
fail-closed). `diagnostics/` is now in the table (`:1080`), and the run scope and heartbeat
timer pin for their lifetime (`:1084-1090`). `os.open(path, flags, dir_fd=fd)` with `O_PATH`
exists in the stdlib on Linux, so the "no `openat2`, no `ctypes`" claim holds.

- SHOULD FIX (implementation-level) — **the symlink case does not surface as `ELOOP`.**
  With `O_PATH|O_NOFOLLOW`, `open(2)` on a symlink *succeeds* and returns an fd to the link
  itself; it is the `O_DIRECTORY` flag that then fails the lookup with **`ENOTDIR`**, not
  `ELOOP`. `:1063` and `:1076` say "refuse on `ELOOP`". A `spool.py` that catches only
  `ELOOP` will let `ENOTDIR` propagate as an uncaught exception — still non-zero, still
  fail-closed, but T4.2's "symlinked `responses/`" test written to expect `ELOOP` will not
  see what it expects. Refuse on `ELOOP` **or** `ENOTDIR`, and `fstat` the result to assert
  `S_ISDIR` — belt and braces, and it makes the test unambiguous.
- SHOULD FIX — **request files must be regular and bounded.** The read-once step (`:1117`)
  opens a sandbox-authored file. A FIFO in `requests/` blocks the oneshot until
  `RuntimeMaxSec`; a multi-GB file is read into memory. `openat(..., O_RDONLY|O_NOFOLLOW)`,
  `fstat` → `S_ISREG`, size cap (a request is a few hundred bytes), else quarantine.
- NIT — §6.4 step 1 (`:1115`) and §10 (`:2012`) still name "`realpath` containment" as a
  defence; §6.3 correctly makes the pinned walk the only control and `realpath` advisory.
  Drop the word from both so nobody implements the racy check instead of the walk.

## 3. §5.3a LUKS — the inversion is CORRECT; the chosen route has one unaddressed obstacle

The relocation is right: `ks.cfg:359-360` and `:377-378` pass `--passphrase=` inline on both
branches, so the install is unattended-capable and the prompt is at every boot. No real
passphrase value appears anywhere in the file — the only occurrences are the `"…"` placeholder
at `:876` and prose. U8 selecting the route and gating Phase 5 is the right control.

- SHOULD FIX (before Phase 5, inside U8) — **Plymouth will swallow the prompt on the serial
  console.** `ks.cfg:444` sets `bootloader --append="rhgb quiet"`. With `rhgb`, the initramfs
  runs Plymouth, and `systemd-ask-password-console.path` is conditioned on Plymouth *not*
  running (`ConditionPathExists=!/run/plymouth/pid`), so the LUKS prompt goes to the
  Plymouth agent on the virtio-gpu display and **nothing appears on `ttyS0`**. Two
  consequences: route 1 cannot drive the passphrase unless the VM's cmdline drops `rhgb` /
  adds `plymouth.enable=0` (a divergence to record as `plymouth-disabled-by-harness`), and —
  worse — the "wedged at the LUKS passphrase prompt" matcher (`:899-905`) never sees the
  prompt text, so the failure it promises to name degrades to the bare timeout it promises
  never to emit. U8's first sub-question (`:100-103`) should name Plymouth as the specific
  reason the prompt may not reach serial, and T5.1b should carry the cmdline change.
- SHOULD FIX (wording) — `:891` and `:2049` say route 1 "keeps the shipped kickstart
  byte-identical". It does not and cannot: §2.1 `:175` and T5.1 `:1895-1899` make
  `ks-vm-desktop.cfg` a **separate file** with a per-run passphrase, autologin and a VM
  header, so it is a *template rendered per run*, not a tracked static file. What route 1
  keeps identical is the **partition stanza** (LUKS2 + btrfs subvolumes). Say that; the
  current sentence contradicts the phase task and will confuse the implementer about whether
  T5.1 is a tracked file or a render.

## 4. `play-nvidia.yml` as the failing scenario — VERIFIED at source

`play-nvidia.yml:12` is `scope: general`; `:8` is `hosts: desktop` (the inventory group every
box is in, so it runs on the server profile); the first assertion is `mok_password is defined` at `:203-206`. The guest cannot have it: `environment/localhost/host_vars/localhost.yml`
is **gitignored** (only `localhost.yml.dist` is tracked, `git ls-files`), and
`run.bash:501-516` writes a fresh file with no vault values on the `none` path. Nothing
before `:203` can skip the play: the only `when:` guards (`:66-175`) are `nvidia_install_*`
defaults and there is no scope-guard `end_play` (that guard exists only in the `gnome`-scoped
plays). If an earlier task fails first (RPM Fusion fetch, `akmod` build), the play still
*runs and fails*, which is the criterion. The falsifiability proof is alive.

- NIT — the design cites `:203-211`; the assert block runs to `:215`. Cosmetic.

## 5. `TriggerLimitBurst=0` + drain-all — NARROWED, not closed

Disabling the trigger limit removes the pre-activation kill (`:1163-1170`), and draining every
pending request per activation is correct and cheaper. But `StartLimitBurst` is still "high"
and **finite** (`:1162`, T4.4 `:1871`). A sandbox `touch` loop makes the path unit
re-trigger the moment each oneshot exits; if a drain finds nothing new it exits in
milliseconds, so activations come at machine speed and any finite burst is reached in
seconds — then the path unit fails, exactly as before. §6.5 makes it visible, so this is a
self-inflicted, visible DoS rather than a silent one, but §6.4 says "there is no loop to
break" (`:1170`), which is not true.

- SHOULD FIX — set `StartLimitIntervalSec=0` (disables start limiting per
  `systemd.unit(5)`) **and** end each activation with a short fixed sleep (a debounce of a
  second or two), so activation rate is bounded by the watcher and no systemd limit remains
  to trip. Record the residual in §6.7 (`:1337-1340`): a hostile event loop costs the host a
  little CPU and the agent its own bridge latency; it cannot wedge the unit.

## 6. Open decision 7 — the reasoning HOLDS, one sentence is wrong

`entrypoint.sh:362-363` accepts `1 | 0 | ""`, so a launcher line of the existing form
`-e "CCY_CHILD_CLAUDE=${CCY_CHILD_CLAUDE:-}"` is safe when unset (empty string → off). The
`-e` list at `claude-yolo:3048-3061` indeed lacks the name and already passes
`CCY_CLAUDE_WRAPPER` and `CCY_NO_SUPERVISOR` by that pattern. 00092's Non-Goal is a launcher
*flag* with its own UX; an env passthrough is a different thing. Recommendation is sound.

- NIT — `:1650-1651` says "`ccy.env` remains the declaration, the env is the override". The
  precedence is the reverse: `ccy.env` is sourced *after* the env arrives and uses a plain
  `export CCY_CHILD_CLAUDE=1`, so a project that sets it in `ccy.env` wins over the env. For
  this plan's use (the tracked line is commented out) that is irrelevant, but the sentence
  should not go to the owner inverted. Say "the env supplies the value when `ccy.env` does
  not set one".

## 7. Whole-document coherence — cross-section rot found

Each is a sentence that survived from an earlier revision and now contradicts a later fix.
None changes the architecture; all should be corrected so the implementer reads one design.

| Where                     | Stale text                                                                                  | Contradicts                                                                                          |
| ------------------------- | ------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------- |
| §10 `:2014`               | "an unsigned or non-verifying response is reported as `forged`, distinctly"                 | §6.6 rule 11 `:1332-1333`: only host `vmtest verify` can conclude `forged`                           |
| T4.7 `:1878-1880`         | "exits 0 only on `finished` + `pass` + **verifying signature**"                             | §6.6 `:1328-1333`: the container reader does not verify                                              |
| T4.8 `:1883`              | "forged response" in the *bridge* selftest                                                  | must be a host-side `vmtest verify` test, or the selftest asserts something the container cannot see |
| §10 `:2003`               | "separated at … `evidence.base.profile` (§3.5)"                                             | §3.5 `:421-423`: `profile` cannot distinguish the bases; it is `base.kind`/`base.name`               |
| §10 `:2011`, T4.4 `:1871` | "the systemd limits are high"                                                               | §6.4 `:1163`: `TriggerLimitBurst=0`, not high                                                        |
| §10 `:2007`               | "the bridge selftest (T4.6)"                                                                | the selftest is T4.8; T4.6 is `verdict.py`                                                           |
| §3.5 `:447-449`           | "a `refresh` on it may simply be a rebuild rather than a boot-and-upgrade cycle"            | §4.4a: there is no boot-and-upgrade cycle for any base                                               |
| §5.3a `:891`, §11 `:2049` | "the shipped kickstart stays byte-identical"                                                | §2.1 `:175`, T5.1 `:1895-1899`: a separate, per-run-rendered kickstart                               |
| §4.4 `:666` vs §4.4a      | `refresh` with the revision *unreadable* — but §4.4a's completeness test needs `probe_seen` | define: with no probe value, record guest-seen, stamp `degraded`, never `incomplete`                 |
| §6.7 `:1337-1340`         | known limits omit the human `reset-failed` remedy and the hostile-loop residual             | §6.4/§6.5 introduced both                                                                            |

Counts re-tallied and correct: §8 00063 has nine rows and the prose sums to nine
(`:1545-1551`); §6.6 `:1210` says 20 `AgentNotes` rows, matching the table; §0's U-list is
U1, U2, U4, U5, U7, U8 with U3/U6 deliberately retired (`:108-110`), and T0.3 `:1710` lists
the same six. §4.4's matrix (`:660-666`) is total over its two inputs and T1.2 (`:1733-1747`)
enumerates the full product space rather than hand-picked cases — that is the right fix for
B6 and it is the strongest test design in the document.

## What is verified good and should not be touched again

§3.3 backing chain; §4.3 authority rule with the per-tree hashes; §4.4 total matrix and
`degraded` stamp; §4.4a run-as-probe with guest-seen revision (B7 closed — the 45-mirror,
two-day measurement is the right evidence); §5.3 two-artefact boot medium; §6.3 D1–D3;
§6.5 liveness; §6.6 rules 01–11; §7's cache/reflink/`cache=unsafe` set; §8's corrected
00063 map; §12.

---

**Verdict: implementable — build it.** Take the two B9 implementation details, the Plymouth
item in U8, the `StartLimitIntervalSec=0` + debounce change, and the ten stale sentences
during the phases that own them. None warrants another design round.
