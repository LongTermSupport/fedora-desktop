# Brainstorm (Fable): Headless / Server Provisioning

**Recommendation in one sentence**: classify every core play with a `# scope: gnome|general|server` comment attached to its `import_playbook:` line in
`playbooks/playbook-main.yml`, gate `scope-gnome`/`scope-server` imports with a
plain `when: provisioning_profile == ...` clause on a single new extra-var
(default `desktop`, so an unflagged run is byte-identical to today), and add a
`scripts/qa-ansible-scope.bash` that greps that one file and fails if any
import line lacks a valid, self-consistent scope declaration.

This rejects the two options the plan floats first (`--tags`/`--skip-tags`
and a separate `playbook-server.yml`) for concrete, repo-grounded reasons
below — read section 1 before objecting that tags were the "obvious" choice.

---

## 1. Selection mechanism

**Recommendation: a single extra-var, `provisioning_profile`, read at the
import site with `when:`.**

```yaml
# playbooks/playbook-main.yml
- import_playbook: imports/play-gnome-shell.yml  # scope: gnome
  when: (provisioning_profile | default('desktop')) != 'server'

- import_playbook: imports/play-podman.yml  # scope: general

- import_playbook: imports/play-git-configure-and-tools.yml  # scope: general
```

Commands:

```bash
# Desktop (today's exact behaviour — no flag needed):
ansible-playbook playbooks/playbook-main.yml

# Headless server:
ansible-playbook playbooks/playbook-main.yml -e provisioning_profile=server
```

**Why not `--tags`/`--skip-tags`** (the option the plan lists first): Ansible
tag inheritance is **additive, not subtractive**. A `tags:` key on a play's
`- hosts:` block is appended to *every* task's own tags — there is no way to
tag a single task inside a `scope-general` play so that it gets *excluded*
when the play-level `scope-general` tag is selected; the play-level tag is
still present on that task regardless of what extra tag you also give it. I
found exactly one real mixed play in this repo
(`playbooks/imports/play-basic-configs.yml` — see §3), so this isn't
theoretical: tags cannot express it cleanly, `when:` can (conditions **AND**,
so a task-level `when:` on top of a play-level `when:` composes correctly).
Tags stay exactly as they are today for their existing purpose (partial
reruns — `packages`, `pyenv`, `grub`, `kitty`, …); scope selection is a
different axis and doesn't need to fight that mechanism.

**Why not a separate `playbook-server.yml`**: it duplicates the ~31-line
import list into a second file that must be hand-kept in sync forever — the
single highest-maintenance-burden option on the table, and it directly
violates the plan's own constraint ("one play stays one play; no wholesale
duplication"). Reject outright.

**Why not inventory host groups** (`desktop` / `server` groups): this repo
provisions **one local machine** (`transport=local`, `ansible_connection: local`, single `hosts: localhost` entry in
`environment/localhost/hosts.yml`). It isn't fleet management across
distinct hosts — it's "this box, in one of two modes." A `server` inventory
group would have to point at the *same* `localhost` entry as `desktop`,
selected via `--limit`, which adds an inventory-layer indirection to model
something a plain variable already models more directly. Host groups are the
right tool when hosts differ; here the *host* is fixed and the *mode* varies.
A variable is the correct abstraction for "mode."

`when:` on `import_playbook:` is standard, documented Ansible behaviour — the
condition is propagated onto every task inside the imported play (not
evaluated once for the whole playbook), which is mechanically a little
verbose in `--list-tasks` output but functionally exactly what's needed and
costs nothing at runtime for ~30 plays. Verify this is still true on this
repo's `ansible-core` version as the very first first-slice step (§6) — it's
a load-bearing assumption.

---

## 2. Where scope lives

**Recommendation: a trailing `# scope: <value>` comment on the
`import_playbook:` line in `playbooks/playbook-main.yml` — not inside the
individual play files.**

Rejected alternative: play-level `tags:`/a `scope:` var declared *inside*
each of the 31 play files. That scatters the classification across 31 files
a QA gate has to open individually, and (per §1) tags can't express the
exclusion case anyway. Centralizing in the one file that already **is** the
authoritative enumeration of "what runs in a bulk provisioning run" means:

- Adding a new core play = one new line in one file, already true today.
  Classifying it = append one more comment token to that same line. Zero new
  files, zero new mechanism to learn.
- The QA gate greps **one file**, not 31 — the check is `grep -c` simple,
  matching the existing `qa-ansible.bash` style exactly (see §4).
- A reviewer can `git diff playbooks/playbook-main.yml` and see the entire
  scope taxonomy state in one screen.

The comment is inert to Ansible (never parsed by the engine), so it can't
itself cause a runtime bug — only a QA-gate false-negative if malformed,
which fails *closed* (missing/invalid comment = QA failure), matching the
project's fail-fast rule.

**Naming**: three tokens — `gnome`, `general`, `server` (I'd drop the
`scope-` prefix inside the comment; the *concept* is called the scope
taxonomy, the *values* don't need to repeat the word). If the QA gate should
also be greppable by prefix, `gnome`/`general`/`server` are already unambiguous
tokens — no collision risk with anything else appearing after `# scope:`.

---

## 3. Mixed-concern plays

**Recommendation: split the play into two single-scope files. Do not do
in-play task-level scope exceptions.**

I found the concrete case: `playbooks/imports/play-basic-configs.yml` is
almost entirely `general` (PS1 colour, sudoers, vim colours, bashrc includes,
SSH helper scripts, YQ, DNF parallel, GRUB auto-hide, fwupd) but contains one
task that is desktop-hardware-only:

```yaml
    - name: Deploy USB audio fix script
      ...
```

A headless server has no USB audio hardware to fix. Two ways to exclude it:
(a) a task-level `when:` guard *inside* the file, or (b) extract it into its
own file. **I recommend (b)** — extract to a new
`playbooks/imports/play-usb-audio-fix.yml` (`scope: gnome`), imported as its
own line:

```yaml
- import_playbook: imports/play-basic-configs.yml  # scope: general
- import_playbook: imports/play-usb-audio-fix.yml  # scope: gnome
  when: (provisioning_profile | default('desktop')) != 'server'
```

Why (b) over (a): the QA gate in §4 only ever reads
`playbooks/playbook-main.yml`. A task-level `when:` buried inside a play file
is **invisible** to that gate — nothing enforces it stays correct, and
"exactly one scope per play, enforced" (the plan's explicit requirement)
silently degrades to "exactly one scope per play, unless someone hides an
exception inside the file." Splitting keeps the invariant airtight: **one
file = one play = one scope, always**, which is also already the de facto
convention today (every existing play file has exactly one `- hosts:` block
— confirmed by grepping the whole `imports/` tree). This plan just makes that
convention load-bearing instead of incidental.

This is a one-time, bounded refactor cost (I found exactly one file that
needs it in the current 31-play core set — see §5's ambiguous list for
others that are borderline but don't actually need splitting).

---

## 4. The QA gate

New script, mirroring `scripts/qa-ansible.bash`'s shape exactly (grep-based,
`set -euo pipefail`, JSON to `${QA_JSON_OUT:-/tmp/qa-ansible-scope.json}`,
terse stdout, same failure-array JSON shape as its siblings):

`scripts/qa-ansible-scope.bash`:

1. Extract every `- import_playbook: imports/….yml` line from
   `playbooks/playbook-main.yml` (one `grep -nE` for the pattern — same
   technique `qa-ansible-syntax.bash` already uses to discover playbooks).
2. For each line, extract the trailing comment via
   `grep -oP '#\s*scope:\s*\K\S+'`.
   - No match → **ERROR: missing scope declaration**.
   - Match not in `{gnome,general,server}` → **ERROR: invalid scope value**.
3. Cross-check the `when:` line immediately following each import (if any)
   against the declared scope — this is the drift-proofing the plan asks
   for, catching "someone changed the comment but not the guard" or
   vice versa:
   - `scope: gnome` → the next line **must** be a `when:` containing both
     `provisioning_profile` and `server` (i.e. it skips on server). Missing
     → **ERROR: gnome play has no server-skip guard**.
   - `scope: server` → same shape but asserting equality instead of
     inequality (skip on desktop). Missing → **ERROR: server-only play runs
     unconditionally on desktop**.
   - `scope: general` → **must not** have a `provisioning_profile`-related
     `when:` at all (a `general` play that's secretly gated is a
     misclassification, not a `general` play). Presence → **ERROR**.
4. (Optional second pass, cheap) count `- hosts:` occurrences in each
   imported play file and flag >1 as a hygiene warning — enforces the "one
   file = one play" convention §3 depends on. This can piggyback on the file
   list `qa-ansible.bash`'s Check 2 already walks.

`imports/optional/**` handling: **explicitly out of scope for this gate.**
Optional plays are never part of a bulk provisioning run — a user invokes
`ansible-playbook playbooks/imports/optional/common/play-ddev.yml` by exact
path, deliberately, regardless of profile. "Scope" as a *selection* concept
has no meaning for a playbook that's always manually, individually invoked.
Forcing scope annotations onto all ~40 optional plays for a gate that will
never use them to select anything is pure busywork with no drift risk to
guard against (there's nothing to drift *from* — they're not in an
enumeration). If a later plan wants profile-aware optional plays, that's new
scope, not this gate's job today (YAGNI).

Wiring into `qa-all.bash`: add as a **7th positional stage**, following the
exact pattern of the existing 6 (`TMP_ANSIBLE_SCOPE=$(mktemp)`, run the
script with `QA_JSON_OUT="$TMP_ANSIBLE_SCOPE"`, add to the `jq -s` merge's
positional file list `"$TMP_BASH" ... "$TMP_JS" "$TMP_ANSIBLE_SCOPE"`, add
`"ansible_scope": .[6]` to the `checks` object literal). Not a candidate for
the separate "L0 hard gate" pattern used by
`qa-nokill-containerwatch.bash` — that pattern exists for one specific
non-negotiable safety invariant outside the normal check taxonomy; this gate
is a normal Ansible hygiene check and belongs in the merged JSON like its
`qa-ansible.bash`/`qa-ansible-syntax.bash` siblings.

---

## 5. Server baseline — classifying the real 31 core plays

Going through `playbooks/playbook-main.yml` top to bottom:

| Play                         | Scope     | Note                                                                                                                                                                                                                                                                                                                   |
| ---------------------------- | --------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| play-AA-preflight-sanity     | general   |                                                                                                                                                                                                                                                                                                                        |
| play-AB-dnf-upgrade          | general   |                                                                                                                                                                                                                                                                                                                        |
| play-basic-configs           | general   | *after* extracting USB audio fix (§3)                                                                                                                                                                                                                                                                                  |
| play-prevent-ssh-suspend     | general   | name suggests "desktop," but it's driven by active SSH sessions — if anything **more** relevant on a headless SSH-managed box                                                                                                                                                                                          |
| play-network-wait-tuning     | general   | boot-time network readiness matters for headless even more (no local console to notice a stalled boot)                                                                                                                                                                                                                 |
| play-mask-intel-lpmd         | general   | hardware/CPU power mgmt, not GUI-coupled                                                                                                                                                                                                                                                                               |
| play-systemd-user-tweaks     | general   | ambiguous — flagging: "user" systemd services assume a lingering user session; confirm this behaves sanely with no GUI login session before shipping                                                                                                                                                                   |
| play-nvm-install             | general   |                                                                                                                                                                                                                                                                                                                        |
| play-git-configure-and-tools | general   |                                                                                                                                                                                                                                                                                                                        |
| play-git-hooks-security      | general   |                                                                                                                                                                                                                                                                                                                        |
| play-firefox                 | **gnome** |                                                                                                                                                                                                                                                                                                                        |
| play-github-cli-multi        | general   |                                                                                                                                                                                                                                                                                                                        |
| play-ms-fonts                | **gnome** | fonts only matter for rendering                                                                                                                                                                                                                                                                                        |
| play-rpm-fusion              | general   | **ambiguous** — just a repo enablement; kept general since non-GUI packages (ffmpeg, codecs) can need it too, but it's a judgement call, not a hard fact                                                                                                                                                               |
| play-browsers                | **gnome** | Chrome/Brave/Vivaldi                                                                                                                                                                                                                                                                                                   |
| play-toolbox-install         | **gnome** | JetBrains Toolbox, GUI IDE launcher                                                                                                                                                                                                                                                                                    |
| play-docker                  | general   | rootful Docker, DDEV-focused but standalone-useful headless                                                                                                                                                                                                                                                            |
| play-lxc-install-config      | general   | system containers, valuable on a server                                                                                                                                                                                                                                                                                |
| play-podman                  | general   |                                                                                                                                                                                                                                                                                                                        |
| play-python                  | general   | **ambiguous** — pulls `SDL2-devel`/`portaudio-devel`/`portmidi-devel` (game/audio dev headers) alongside plain CPython build deps; harmless to install headless but worth a follow-up to see if that sub-list should itself be split out as `scope: gnome` inside a future refactor (not urgent — YAGNI for this plan) |
| play-claude-yolo             | general   |                                                                                                                                                                                                                                                                                                                        |
| play-claude-code             | general   |                                                                                                                                                                                                                                                                                                                        |
| play-comms                   | **gnome** | Slack via Flatpak                                                                                                                                                                                                                                                                                                      |
| play-gnome-shell             | **gnome** |                                                                                                                                                                                                                                                                                                                        |
| play-gnome-shell-extensions  | **gnome** |                                                                                                                                                                                                                                                                                                                        |
| play-markless                | general   | terminal-based markdown viewer — explicitly headless-friendly by design                                                                                                                                                                                                                                                |
| play-terminal-emulators      | **gnome** | alacritty/kitty/ghostty/foot are GUI terminal emulators                                                                                                                                                                                                                                                                |
| play-vscode                  | **gnome** |                                                                                                                                                                                                                                                                                                                        |
| play-vpn                     | general   | VPN + firewall — servers need VPN too, arguably more so                                                                                                                                                                                                                                                                |
| play-gsettings               | **gnome** | literally GNOME settings                                                                                                                                                                                                                                                                                               |
| play-ZZ-repo-cleanup         | general   |                                                                                                                                                                                                                                                                                                                        |

**Tally**: 10 `gnome`, 21 `general`, **0 `server`-only**. That's expected and
fine — the plan's own non-goals explicitly exclude adding new server-specific
workloads (hardening, k8s, web/db roles) in this pass. The `server` bucket
exists in the taxonomy for future use, not because this pass needs to
populate it. Don't invent a placeholder `scope: server` play just to prove
the enum has three live members — an empty-but-valid bucket is a fine state,
and the QA gate already accepts zero occurrences of a valid enum value.

---

## 6. First slice

Smallest change that proves every part of the design at once, on the real
repo:

1. Add `# scope: <value>` comments to all 31 `import_playbook:` lines in
   `playbooks/playbook-main.yml` per the table in §5 (no `when:` needed yet
   except on the `gnome` ones).
2. Add `when: (provisioning_profile | default('desktop')) != 'server'` to
   the 10 `gnome` imports.
3. Extract `play-usb-audio-fix.yml` out of `play-basic-configs.yml` (§3),
   proving the mixed-play handling end-to-end, not just in theory.
4. Write `scripts/qa-ansible-scope.bash`; wire into `qa-all.bash` as stage 7.
5. `./scripts/qa-all.bash` — must pass.
6. `ansible-playbook --syntax-check playbooks/playbook-main.yml` (safe in the
   CCY container) with **both** `provisioning_profile` unset and
   `-e provisioning_profile=server`, confirming the file still parses in
   both modes and — critically — confirming the load-bearing assumption from
   §1 that `when:` on `import_playbook:` behaves as documented on this
   repo's pinned `ansible-core` version.
7. **(HOST, not container)** `ansible-playbook playbooks/playbook-main.yml --check` unflagged (desktop — must diff-match a pre-change `--check` run,
   proving zero regression) and again with `-e provisioning_profile=server --check` (confirm the 10 `gnome` plays report skipped, everything else
   still attempts to run).

That's the whole design proven on real files, not a toy example — the
remaining work (Phase 3 in `PLAN.md`) is repeating step 1–2's classification
labor across whichever imports weren't covered by this slice (all of them
are, actually — this slice **is** the full classification, just not yet the
full task list like QA-gate edge-case tests).

---

## 7. Honest pros/cons and failure modes

**Pros**

- Single file (`playbook-main.yml`) is both the enumeration and the
  classification — can't drift apart because they're the same artifact.
- `when:` + extra-var is a pattern this repo already uses everywhere
  (`when: ansible_distribution == 'Fedora'`, `AnsibleStyle.md`) — no new
  Ansible concept for contributors to learn.
- Default behaviour with no flag is *exactly* today's desktop run — zero
  regression is true by construction (the var literally doesn't exist unless
  passed), not by promise.
- QA gate is pure grep over one file — fast, trivial to review, matches
  `qa-ansible.bash`'s existing idiom precisely, easy for a future maintainer
  to extend.
- Mixed-play handling (split into single-scope files) keeps "exactly one
  scope, enforced" airtight — no QA-invisible in-file exceptions possible.

**Cons / failure modes**

- `when:` on `import_playbook:` expands the condition onto every task in the
  imported play — harmless at runtime, but `--list-tasks`/`-vvv` output gets
  noisier (many individual "skipping" lines instead of one). Cosmetic only.
- The comment grammar (`# scope: gnome`, exact spacing) is convention, not
  enforced by Ansible — a typo (`# Scope:`, `#scope:`) fails the QA gate's
  regex. This fails **closed** (gate reports it as a missing declaration,
  blocking QA) which is the correct fail-fast direction, but the exact
  grammar needs one line in `CLAUDE/AnsibleStyle.md` so contributors don't
  have to reverse-engineer it from the gate script.
- The `when:`-matches-`scope:` cross-check (§4.3) is a grep heuristic, not a
  real YAML/Ansible parse — an unusual but valid formatting (e.g. the
  condition split across multiple lines, or phrased with `not in` instead of
  `!=`) could dodge it. Mitigated by prescribing one canonical phrasing in
  `AnsibleStyle.md`, same tradeoff `qa-ansible.bash`'s existing patterns
  already accept.
- A play file that exists under `imports/` but is **never added** to
  `playbook-main.yml` is invisible to this gate (it only reads lines that
  exist). This is a pre-existing gap — such a file is already dead code
  today, orthogonal to this plan — not introduced by this design, but worth
  naming so nobody expects the gate to catch it.
- `imports/optional/**` is deliberately excluded from enforcement (§4) —
  this is a scoping decision, but it does mean the taxonomy doesn't (yet)
  answer "can I run this specific optional play headless?" for any of the
  ~40 optional plays. Explicitly future work, not a gap in *this* design.
- One-time refactor cost to split `play-basic-configs.yml`; bounded (found
  exactly one file needing it), but any *future* mixed play will need the
  same treatment — this is ongoing maintenance, just cheap and mechanical
  per occurrence (extract, add one import line, add one comment).
