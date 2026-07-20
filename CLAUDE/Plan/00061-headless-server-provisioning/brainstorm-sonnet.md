# Brainstorm (Sonnet): Headless Server Provisioning via Play-Level Scope Tags

## Grounding — what I actually found in the repo

- `playbooks/playbook-main.yml` unconditionally imports 31 core plays, all `- hosts: desktop`, all resolved against a single-host inventory: `environment/localhost/hosts.yml` defines exactly one group (`desktop`) containing exactly one host (`localhost`, `ansible_connection: local`). There is **no second host, no server group, no SSH transport** anywhere in this repo. This one fact eliminates an entire design axis before it starts (see §1).
- Of the 31 core plays, only a handful declare `tags:` today, and every existing usage is **task-level**, not play-level (`play-basic-configs.yml:82` `tags: packages`, `play-python.yml:106-108` `pyenv`/`pyenv_install_versions`, `play-mask-intel-lpmd.yml:49-51` `systemd`/`power`, `play-git-configure-and-tools.yml:121` `tags: alias`). **No play in the repo currently has a play-level `tags:` key** (a sibling of `hosts:`/`name:`/`become:`). This is genuinely greenfield — no legacy convention to reconcile.
- Almost every play is **already single-concern** in practice. I read all 31 core plays' task lists. Exactly **one** genuinely mixes GNOME-only content into an otherwise general play: `playbooks/imports/play-vpn.yml` installs `NetworkManager-openvpn-gnome` (a GNOME NetworkManager-applet integration package) alongside `wireguard-tools` and `NetworkManager-openvpn` (both pure CLI, equally useful headless). Everything else classifies cleanly. This matters a lot for the "mixed-concern play" design axis — it is not the common case, it is the *rare* case, and the design should not be complicated to accommodate a common case that doesn't exist.
- `scripts/qa-ansible.bash` already establishes the exact QA-gate pattern to imitate: pure `grep -rn` structural checks (no YAML parser) over `playbooks/ tasks/ vars/ environment/ roles/`, JSON emitted to `${QA_JSON_OUT}`, merged into `qa-all.bash`'s single `jq -s` call by **positional array index** (`.[0]`..`.[5]` today, currently `bash, python, patterns, ansible, ansible_syntax, js`). `qa-all.bash` even has a hand-written comment warning that a new stage must not be added carelessly because it would "corrupt the positional `.[0]..[5]` JSON merge" (that's why the no-kill gate was deliberately kept *outside* the jq merge). This is the exact seam I plug into.
- `scripts/qa-ansible-syntax.bash` already has the playbook-discovery logic I need for free: `MAIN_PLAYBOOK` + everything under `playbooks/imports/**` (recursively, including `optional/**`) whose file matches `^\s*-\s+hosts:`. I reuse that discovery, not reinvent it.

## 1. Selection mechanism — play-level `tags:` + `--skip-tags`, not a second playbook or host group

**Rejected: separate `playbook-server.yml` entry point.** Every future play addition would need a decision about which of two playbook files imports it (or both) — this is the literal "duplication" the requester explicitly ruled out. It also invites drift the moment someone updates `playbook-main.yml` and forgets the sibling file exists.

**Rejected: inventory host groups (`desktop` / `server`).** This looks idiomatic-Ansible on paper, but this repo has **one host** (`localhost`, local transport). A `server` group would have to point at the *same* `localhost` under a second name, and `- hosts: desktop` on every play would *still* need to change to something group-selectable (`- hosts: desktop:server` or a var-driven `- hosts: "{{ target_group }}"`) for the group to actually filter anything — group membership alone does not skip *tasks within* a play that already matches. It solves nothing that tags don't solve more simply, and it fights the grain of a repo that has always been "one box, local connection."

**Rejected: a `profile` variable gating `import_playbook: ... when:`.** Ansible does propagate a `when:` on `import_playbook` to every play it imports, so `- import_playbook: imports/play-firefox.yml \n  when: provisioning_profile != 'server'` genuinely works. I looked hard at this because it has one real advantage tags don't: the *zero-flag* command (`ansible-playbook playbooks/playbook-main.yml`) could stay desktop-behaviour forever without an operator ever having to pass a flag. But it has a fatal maintenance cost: **the scope declaration would live in `playbook-main.yml`'s import line, not in the play file itself.** Adding a new GNOME play would require touching *two* places — the new play file, and the right `when:` on its import line — which is exactly the "duplicated bookkeeping" the requester wants to avoid. Tags, declared inside the play file, need zero changes to `playbook-main.yml` beyond the import line that already has to exist regardless of scope.

**Recommendation: a play-level `tags:` block on every play, selected with `--skip-tags` at invocation.** Ansible tags declared as a sibling of `hosts:`/`name:`/`become:` on the play object propagate to *every task in the play* automatically — no per-task tagging needed, no role/include gymnastics, this repo's plays are flat task lists so inheritance is total. Concretely:

```yaml
#!/usr/bin/env ansible-playbook
---
- hosts: desktop
  name: Communication Tools
  become: false
  tags:
    - scope-gnome
  vars:
    root_dir: "{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"
  tasks:
    ...
```

Two canonical commands, both explicit, both documented in `docs/`:

```bash
# Desktop (current default, unchanged result)
ansible-playbook playbooks/playbook-main.yml --skip-tags scope-server

# Headless server
ansible-playbook playbooks/playbook-main.yml --skip-tags scope-gnome
```

**Why `--skip-tags` and not `--tags` allow-lists on both sides:** an allow-list (`--tags scope-general,scope-server`) has to be updated the day a *new sub-bucket* is invented under the taxonomy; a skip-list of exactly one bucket never does — every future `scope-general`/`scope-server` play is included automatically just by not being `scope-gnome`. This is the lowest-maintenance selection rule available: the command line itself never needs editing as the play set grows, only the new play's own `tags:` line does.

**Why the desktop command explicitly skips `scope-server` even though that bucket is empty today:** this plan's own non-goals rule out adding real server-hardening plays (fail2ban, unattended-upgrades, cockpit) *in this plan* — so `scope-server` starts empty and `--skip-tags scope-server` is a no-op on day one. But the taxonomy exists precisely so a *future* plan can add such a play safely. If the desktop invocation is left as the bare zero-flag command today, the first `scope-server`-only play added later will silently run on desktops too — a real regression, just a deferred one. Baking the symmetric `--skip-tags` into both canonical commands **now**, while it costs nothing, is the cheap insurance against that: the "zero regression" guarantee holds not just today but structurally, forever, without anyone having to remember to update a doc when `scope-server` gets its first member.

**Honest cost of this recommendation vs. the profile-var alternative:** the bare `ansible-playbook playbooks/playbook-main.yml` (no flags) is no longer *the* documented desktop command — `--skip-tags scope-server` is. That's a one-time doc/muscle-memory change. I judge that cheaper than the two-edit-site cost of the `when:` alternative, but it's a real trade-off, not a free lunch, and worth a decision-gate sentence either way.

## 2. Where scope lives — the play's own `tags:` block, always the 2-space-indented one directly under `hosts:`/`name:`/`become:`

Scope is a property of the *play*, not of a task inside it (mixed plays are the rare exception, handled in §3) and not of the *import site* (rejected in §1). So it lives exactly where play-level Ansible tags already syntactically live — no sidecar manifest, no separate `SCOPE.yml`, no metadata comment convention for a grep to half-parse. One YAML key, one place, matching an Ansible feature that already exists and that operators already invoke identically (`--tags`/`--skip-tags`) for every other purpose in this ecosystem.

Rejected alternatives and why:

- **A separate manifest file** (`playbooks/scope-manifest.yml` mapping play → scope) — now every new play is a *two-file* change (the play + the manifest entry), which is worse than today, not better. Also a second source of truth that can drift from the play it describes.
- **A naming convention** (`play-gnome-firefox.yml` vs `play-server-fail2ban.yml`) — unenforceable by a QA gate without also parsing the taxonomy into the regex, brittle to renames, and does not compose with the *existing* `play-AA-`/`play-ZZ-` ordering-prefix convention already in use.
- **A play-level `vars:` entry** (`scope: gnome`) instead of a tag — works, but throws away the free `--skip-tags` selection mechanism Ansible already gives you for tags, forcing a bespoke `when: scope != 'server'` on every play instead. Tags are strictly less code for the same declaration.

## 3. Mixed-concern plays — split the offending *task*, tag it, leave the play's own scope alone

I found exactly one real instance of this in the current 31 core plays: `playbooks/imports/play-vpn.yml`. It installs three packages in one `dnf` task; one of the three (`NetworkManager-openvpn-gnome`) is GNOME-applet integration, the other two are pure CLI and equally useful on a headless box managing its own WireGuard/OpenVPN tunnels. Today:

```yaml
- name: Install VPN Packages
  ansible.builtin.dnf:
    name:
      - wireguard-tools
      - NetworkManager-openvpn
      - NetworkManager-openvpn-gnome
    state: present
```

The play itself is overwhelmingly `scope-general` (VPN client tooling + firewalld rule, both server-relevant), so tagging the *whole play* `scope-gnome` would wrongly exclude it from headless runs, and tagging it `scope-general` would wrongly install a GNOME applet package on a server with no GNOME to integrate with. The fix is a **task-level split**, keeping the play-level tag as the play's dominant scope and adding a second, narrower tag only on the one task that needs it:

```yaml
- hosts: desktop
  name: VPN Tools
  become: true
  tags:
    - scope-general
  vars: ...
  tasks:
    - name: Install VPN Packages (CLI, headless-safe)
      ansible.builtin.dnf:
        name:
          - wireguard-tools
          - NetworkManager-openvpn
        state: present

    - name: Install NetworkManager GNOME Applet Integration
      ansible.builtin.dnf:
        name:
          - NetworkManager-openvpn-gnome
        state: present
      tags:
        - scope-gnome
```

Running with `--skip-tags scope-gnome` on a server now correctly drops just the applet package and keeps the rest. This is additive-only (no `when:`, no new variable, no behaviour change for the desktop path — `NetworkManager-openvpn-gnome` still installs there because `scope-gnome` is not skipped on desktop). The play's own **play-level scope stays `scope-general`** — the QA gate (§4) only demands exactly one scope tag *at the play level*; the extra fine-grained task tag is additive vetting, not a second play-level declaration, so it never trips the "exactly one" rule.

**This is deliberately the exception path, not the primary design.** Given only one real instance exists today across 31 plays, I do not think the design should be built "mixed-play-first" (e.g. by requiring every play to enumerate task-level scope tags even when uniform) — that would tax the 30 clean plays to accommodate the 1 messy one. Classify at the play level by default; reach for a task-level override only when a play genuinely earns it, and let the QA gate's "exactly one play-level scope" rule remain the invariant regardless.

## 4. The QA gate — a grep-based fourth check inside `qa-ansible.bash`, not a new stage

`qa-ansible.bash` already has three greppy checks (fail-fast patterns, shebang/exec hygiene, self-ref vars) sharing one JSON blob and one `$ERRORS` counter, and it is already wired into `qa-all.bash`'s merge. Adding scope enforcement as **Check 4 in the same script** means `qa-all.bash` needs **zero changes** — no new `TMP_SCOPE` temp file, no new positional index in the `jq -s` array (avoiding exactly the footgun `qa-all.bash`'s own comment warns about), no new "missing tool" branch. That is the lowest-friction integration point available, and it's why I did not propose a `qa-scope.bash` standalone sibling despite the existing one-check-per-file pattern elsewhere — a whole new stage script is heavier than the check warrants, and every existing sibling script conspicuously does **one** thing while `qa-ansible.bash` already deliberately bundles multiple *structural* playbook-hygiene checks together. Scope-tag hygiene is exactly that kind of check.

**Design of the check itself**, layered directly under the existing "Check 2: playbook hygiene" block in `qa-ansible.bash` (same discovery loop, same `$yml_file` var, same "is this actually a playbook" `grep -qE '^[[:space:]]*-[[:space:]]+hosts:'` gate — so it automatically covers `playbooks/imports/**` including `optional/**`, exactly like the shebang/exec check already does):

```bash
# ---------------------------------------------------------------------------
# Check 4: play-level scope declaration (Plan 00061)
# ---------------------------------------------------------------------------
# Every playbook must declare EXACTLY ONE of scope-gnome / scope-general /
# scope-server as a PLAY-LEVEL tag (2-space indent, sibling of hosts:/name:/
# become: — never a task-level tag, which sits at 6+ spaces under `tasks:`).
# A task-level scope-gnome override (see play-vpn.yml, mixed-concern plays)
# does NOT count toward this check — it is deliberately invisible to a
# 2-space-anchored grep.
SCOPE_TAGS='scope-gnome|scope-general|scope-server'
SCOPE_VIOLATIONS=()

while IFS= read -r -d '' yml_file; do
    grep -qE '^[[:space:]]*-[[:space:]]+hosts:' "$yml_file" || continue
    # Exclude archived plays — dead weight not meant to run, not meant to be
    # classified against a taxonomy invented after they were shelved.
    [[ "$yml_file" == */optional/archived/* ]] && continue

    rel_file="${yml_file#"$REPO_ROOT"/}"
    # Play-level tags block: the FIRST "^  tags:" line (exactly 2 spaces) —
    # collect every list item until indentation drops back to 2 spaces or less.
    play_tags=$(awk '
        /^  tags:$/ { in_block=1; next }
        in_block && /^    - / { print; next }
        in_block { exit }
    ' "$yml_file")

    hit_count=$(printf '%s\n' "$play_tags" | grep -cE "($SCOPE_TAGS)")

    if [[ $hit_count -eq 0 ]]; then
        echo "  ERROR (scope): $rel_file — no play-level scope tag (need one of scope-gnome|scope-general|scope-server)"
        SCOPE_VIOLATIONS+=("$rel_file (missing scope tag)")
        ERRORS=$((ERRORS + 1))
    elif [[ $hit_count -gt 1 ]]; then
        echo "  ERROR (scope): $rel_file — multiple scope tags declared (exactly one required)"
        SCOPE_VIOLATIONS+=("$rel_file (multiple scope tags)")
        ERRORS=$((ERRORS + 1))
    fi
done < <(find "$REPO_ROOT/playbooks/" -type f \( -name '*.yml' -o -name '*.yaml' \) -print0)
```

...followed by the same `SCOPE_VIOLATIONS` → `jq --arg v ... '. + [$v]'` JSON-array-building idiom the script already uses twice for `FF_VIOLATIONS`/`HYGIENE_VIOLATIONS`, folded into the existing `checks.ansible.scope` key of the one JSON blob `qa-ansible.bash` already emits. No `qa-all.bash` edits, no new required-tool failure mode (pure grep/awk, same as the rest of the file), fails the whole run per this project's #1 rule the moment one play is unscoped.

**Grep/awk vs. a real YAML parser:** I deliberately kept this awk-based rather than reaching for a small Python+PyYAML helper, for two repo-specific reasons: (1) `helpers/CLAUDE.md` is explicit that helpers under `helpers/` are **stdlib-only, no PyYAML**, and this repo has no PyYAML dependency anywhere to lean on; (2) `qa-ansible.bash`'s three existing checks are *all* grep-based specifically because Ansible's own 2.19 parser problems (documented at length in `CLAUDE/AgentNotes.md`) already taught this repo that "correctly handling every possible YAML shape" is not the bar for these structural QA checks — the bar is "match the one shape this repo's `AnsibleStyle.md` actually prescribes" (2-space play-level indent, list-form tags). If a future play ever writes `tags: scope-general` as an inline scalar instead of a list, the awk block above won't see it — that's a real, named limitation (documented inline in the check's own comment), not a silent gap, and the fix is "write tags as a list, like every existing example in `AnsibleStyle.md` already does," not "add a YAML parser."

## 5. Server-baseline classification of the actual 31 core plays

| Play                               | Scope                                                     | Why                                                                                                                                                                                                                                                                                                                                             |
| ---------------------------------- | --------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `play-AA-preflight-sanity.yml`     | `scope-general`                                           | Sanity checks apply to any box                                                                                                                                                                                                                                                                                                                  |
| `play-AB-dnf-upgrade.yml`          | `scope-general`                                           | Package upgrades apply everywhere                                                                                                                                                                                                                                                                                                               |
| `play-basic-configs.yml`           | `scope-general`                                           | Read in full — vim colours, passwordless sudo, PS1 prompt, SSH helper scripts, `yq`, GRUB menu, `fwupd`. No GUI package anywhere. Genuinely general despite the "basic" catch-all name                                                                                                                                                          |
| `play-prevent-ssh-suspend.yml`     | `scope-general`                                           | SSH-session suspend prevention is *more* relevant headless than on a desktop                                                                                                                                                                                                                                                                    |
| `play-network-wait-tuning.yml`     | `scope-general`                                           | NetworkManager boot-timing tuning                                                                                                                                                                                                                                                                                                               |
| `play-mask-intel-lpmd.yml`         | `scope-general`                                           | Any Intel-CPU host, GUI-independent (self-probes and no-ops on AMD)                                                                                                                                                                                                                                                                             |
| `play-systemd-user-tweaks.yml`     | `scope-general`                                           | systemd user-service behaviour                                                                                                                                                                                                                                                                                                                  |
| `play-nvm-install.yml`             | `scope-general`                                           | Node.js toolchain, equally a server dev/runtime dependency                                                                                                                                                                                                                                                                                      |
| `play-git-configure-and-tools.yml` | `scope-general`                                           | git config + `gh` CLI                                                                                                                                                                                                                                                                                                                           |
| `play-git-hooks-security.yml`      | `scope-general`                                           | git hook install                                                                                                                                                                                                                                                                                                                                |
| `play-firefox.yml`                 | `scope-gnome`                                             | GUI browser                                                                                                                                                                                                                                                                                                                                     |
| `play-github-cli-multi.yml`        | `scope-general`                                           | multi-account `gh`/`git` auth                                                                                                                                                                                                                                                                                                                   |
| `play-ms-fonts.yml`                | `scope-gnome`                                             | Fonts only matter with a GUI renderer                                                                                                                                                                                                                                                                                                           |
| `play-rpm-fusion.yml`              | `scope-gnome` **(ambiguous — flagged)**                   | The release-package-enablement half is genuinely general (unlocks free/nonfree repos for anything later), but the *only tasks in the file* are multimedia codecs + `intel-media-driver` (hardware video decode) — pure desktop/playback concern. Classify by what the play actually *does* today, not by what the repo name implies it could do |
| `play-browsers.yml`                | `scope-gnome`                                             | GUI browsers                                                                                                                                                                                                                                                                                                                                    |
| `play-toolbox-install.yml`         | `scope-gnome`                                             | JetBrains Toolbox is a GUI IDE launcher                                                                                                                                                                                                                                                                                                         |
| `play-docker.yml`                  | `scope-general`                                           | Rootful Docker compat engine — DDEV-class server workloads need this too                                                                                                                                                                                                                                                                        |
| `play-lxc-install-config.yml`      | `scope-general`                                           | System containers are at least as relevant on a server                                                                                                                                                                                                                                                                                          |
| `play-podman.yml`                  | `scope-general`                                           | Rootless container engine                                                                                                                                                                                                                                                                                                                       |
| `play-python.yml`                  | `scope-general`                                           | Python/pyenv/pipx dev toolchain                                                                                                                                                                                                                                                                                                                 |
| `play-claude-yolo.yml`             | `scope-general`                                           | Claude container tooling                                                                                                                                                                                                                                                                                                                        |
| `play-claude-code.yml`             | `scope-general`                                           | Claude Code CLI                                                                                                                                                                                                                                                                                                                                 |
| `play-comms.yml`                   | `scope-gnome`                                             | Slack via Flatpak — GUI-only                                                                                                                                                                                                                                                                                                                    |
| `play-gnome-shell.yml`             | `scope-gnome`                                             | GNOME (note: its `name:` field says "Gnome Shell Extensions", duplicating `play-gnome-shell-extensions.yml`'s name — pre-existing repo quirk, not this plan's problem, flagged for the record only)                                                                                                                                             |
| `play-gnome-shell-extensions.yml`  | `scope-gnome`                                             | GNOME                                                                                                                                                                                                                                                                                                                                           |
| `play-markless.yml`                | `scope-general`                                           | Terminal-based markdown viewer — no GUI dependency                                                                                                                                                                                                                                                                                              |
| `play-terminal-emulators.yml`      | `scope-gnome` **(ambiguous — flagged)**                   | Despite "terminal" in the name, these are GUI terminal *emulator applications* (need a display to run at all) — not CLI tooling                                                                                                                                                                                                                 |
| `play-vscode.yml`                  | `scope-gnome`                                             | GUI IDE                                                                                                                                                                                                                                                                                                                                         |
| `play-vpn.yml`                     | `scope-general` (+ one task-level `scope-gnome` override) | See §3 — mixed-concern exemplar                                                                                                                                                                                                                                                                                                                 |
| `play-gsettings.yml`               | `scope-gnome`                                             | GNOME settings                                                                                                                                                                                                                                                                                                                                  |
| `play-ZZ-repo-cleanup.yml`         | `scope-general`                                           | Repo/COPR hygiene applies everywhere                                                                                                                                                                                                                                                                                                            |

**Flagged for a human decision at the decision gate:** `play-rpm-fusion.yml` and `play-terminal-emulators.yml` — my classification is defensible but not unarguable; both are exactly the kind of "read the play, not the filename" judgement call this taxonomy forces into the open, which is itself a point in favour of the design (today this ambiguity is silently unresolved; the QA gate forces someone to pick).

**`scope-server` bucket: intentionally empty in this first pass.** The plan's own non-goals rule out server-hardening content now — the taxonomy needs the bucket to exist so a later plan has somewhere to put `fail2ban`/`unattended-upgrades`/`cockpit` without inventing a fourth category, but nothing in the current 31 core plays legitimately belongs there yet. An empty-but-valid bucket is not a design smell here; it's the taxonomy correctly anticipating work explicitly deferred elsewhere.

## 6. First slice — smallest thing that proves the whole design end-to-end

1. Add `tags: [scope-general]` or `tags: [scope-gnome]` to **all 31 core plays** per the table above (mechanical, ~2-line diff per file, zero task-body changes except the one `play-vpn.yml` split from §3).
2. Add the Check 4 block to `scripts/qa-ansible.bash` (§4) — run `./scripts/qa-all.bash`, confirm it **fails** before step 1 lands (missing scope on all 31) and **passes** after.
3. Run both canonical commands with `--check` (dry-run, safe in the CCY container per `CLAUDE/ContainerRules.md`'s "no playbook *execution*" rule — `--check` never mutates) and diff the task lists:
   ```bash
   ansible-playbook playbooks/playbook-main.yml --skip-tags scope-server --check --list-tasks
   ansible-playbook playbooks/playbook-main.yml --skip-tags scope-gnome  --check --list-tasks
   ```
   Confirm the first list is **identical** to today's unscoped `--list-tasks` output (zero regression, machine-verifiable, no human inspection required) and the second visibly drops every GNOME play.
4. Document both commands in `docs/` (wherever `playbook-main.yml` usage is currently documented) as the two supported entry points.
5. Stop there. Do **not** yet build `scope-server` content, a real headless VM test harness, or hardware-specific-dir classification — those are legitimately Phase 3+/future-plan scope per this plan's own non-goals.

This slice is deliberately narrow: it proves the mechanism (tags), the enforcement (QA gate), and the zero-regression claim (machine-diffed `--list-tasks`) without touching a single line of task *behaviour* other than the one necessary `play-vpn.yml` split.

## 7. Honest pros/cons and failure modes

**Pros:**

- Zero new files, zero new inventory concepts, zero new CLI tooling — every mechanism used (`tags:`, `--skip-tags`, grep-based QA) already exists natively in Ansible or in this repo's own QA idiom.
- Adding a future play is genuinely a one-line addition (`tags: [scope-general]`) with no second edit site anywhere, satisfying the requester's core constraint directly.
- The QA gate slots into an existing script with an existing JSON shape — `qa-all.bash` needs no changes, sidestepping the exact positional-merge footgun its own comments warn about.
- `--list-tasks --check` gives a machine-checkable zero-regression proof, not just "I eyeballed it."
- Mixed-concern plays get a real, demonstrated (not hypothetical) worked example (`play-vpn.yml`) rather than a speculative pattern for a problem that mostly doesn't exist yet.

**Cons / failure modes:**

- **The desktop command is no longer flag-free.** `--skip-tags scope-server` must be remembered/documented forever, even though it does nothing today. If that documentation lapses and someone runs the bare `ansible-playbook playbooks/playbook-main.yml`, a future `scope-server`-only play *would* run on desktop — the insurance in §1 only pays off if the two-command convention is actually followed, not just designed correctly. Mitigation: put both canonical commands in a checked-in doc/README next to the plan's first-slice work, not just in this brainstorm.
- **Play-level tag inheritance is total-but-implicit.** A future contributor adding a task to an existing `scope-general` play that happens to need a GUI (without realising it) will silently ship that GUI dependency into headless runs — the QA gate checks *that a scope tag exists*, not *that the tag is still accurate* for every task added since. This is a review-discipline gap, not a tooling gap; no static check can fully close it without literally executing the play headless in CI (out of scope here).
- **The awk-based scope-block parser is format-sensitive** (§4) — it assumes 2-space play indent and list-form tags, matching `AnsibleStyle.md` today but not proof against someone hand-writing `tags: scope-general` as an inline scalar. A malformed-but-technically-valid YAML play could silently read as "no scope tag" (a hard QA failure, fails safe) rather than "wrong scope" (fails loud) — i.e. the failure mode is over-strict, not under-strict, which is the correct direction to err on for a fail-fast repo.
- **`--skip-tags` cosmetically still prints a `PLAY [...]` banner with zero tasks executed** for every skipped play — mildly noisy console output on a server run (31 desktop plays skip through with an empty banner each), not a functional problem, just an honest wart. `--list-tasks`/`-v` output stays clean either way.
- **The `play-rpm-fusion.yml` / `play-terminal-emulators.yml` classification calls are genuinely arguable** (§5) — if the decision gate disagrees with my read, that's a one-line tag change, not a design failure, but it's worth being explicit that "exactly one scope, mandatory" does not mean "obviously correct scope" — the taxonomy forces a decision, it doesn't make the decision for you.

## Recommendation

Play-level `tags: [scope-gnome|scope-general|scope-server]` on every playbook, selected via `--skip-tags` at invocation (`--skip-tags scope-server` for desktop, `--skip-tags scope-gnome` for server), enforced by a fourth grep/awk check folded into the existing `scripts/qa-ansible.bash` (no `qa-all.bash` changes needed). Mixed-concern plays split at the task level with an additive, narrower tag (one real instance today: `play-vpn.yml`), never by duplicating the play. This is the option that adds the least new surface area to a repo that already leans on Ansible-native tags and grep-based QA everywhere else — the design is, deliberately, almost invisible next to what's already here.
