# Plan 00070 — documentation drift audit

Read-only audit of `CLAUDE.md`, `CLAUDE/*.md`, `.claude/rules/*.md`, `docs/`, `README.md` and the
nested `*/CLAUDE.md` files. Plan bodies, `.claude/hooks-daemon/`, `roles/vendor/` and `untracked/`
were out of scope.

**Nothing here is fixed. This document records findings only.**

## Verdict

The defect is documentation **drift** — docs describing a past state of the code — not sloppiness.
The code and the newer docs are disciplined; the older docs were never re-read after the changes
that invalidated them.

**Worst instance: four live documents teach an Ansible pattern the project's own style guide names
as broken.** A contributor copy-pasting the repo's own "how to write a playbook" template
reintroduces a bug the project already found, diagnosed and banned.

**Runner-up:** two documents that each describe themselves as complete independently omit the same
core playbook, and `CLAUDE.md`'s own topic index omits two of its own current topic files.

## Verification status

Findings came from five parallel read-only passes. **I re-verified 11 of the 17 confirmed findings
myself against the tree before recording them**; the rest are marked. A finding I did not personally
re-check is not thereby wrong — it is unverified by me, which is a different claim.

| Marker | Meaning                                                            |
| ------ | ------------------------------------------------------------------ |
| ✔      | I ran the check myself and the tree disagrees with the doc         |
| ○      | Reported by a review pass with a citation; **I did not re-verify** |

---

## Confirmed findings

### 1 ✔ Four live docs teach a `root_dir` pattern the style guide bans by name

`docs/configuration.md:187`, `docs/development.md:129`, `docs/development.md:293` and
`docs/playbooks.md:1016` all show:

```yaml
root_dir: "{{ inventory_dir }}/../../"
```

`CLAUDE/AnsibleStyle.md:20` says the opposite, explicitly: *"Do NOT use `{{ inventory_dir }}/../../`
— `inventory_dir` is a host-scoped magic var that is UNDEFINED during the early `vars_files`
evaluation pass, so any `vars_files: - "{{ root_dir }}/..."` entry silently skips."* Every real
playbook uses the correct form (`playbooks/imports/play-basic-configs.yml:7`).

`docs/playbooks.md:1016` is the worst of the four — it is the "Creating Custom Playbooks" template
new contributors are told to copy.

**Fix**: replace all four with `root_dir: "{{ lookup('ansible.builtin.config', 'CONFIG_FILE') | dirname }}"`.

### 2 ✔ Two "complete" docs both omit the same core playbook

`playbooks/playbook-main.yml:10` imports `play-mask-intel-lpmd.yml`. It appears in **neither**
`docs/architecture.md`'s numbered "all run by default" list **nor** `docs/playbooks.md`'s Core
Playbooks section — and `docs/playbooks.md:3` calls itself a "Complete catalog". Measured: zero
occurrences of `mask-intel-lpmd` across both files.

Two independent authoritative docs with the same gap is systemic drift, not a typo.

**Fix**: add it to both.

### 3 ✔ `CLAUDE.md`'s own topic index omits two of its own topic files

`CLAUDE/` holds 15 `.md` files; `CLAUDE.md` references 13 of them. `CLAUDE/PlanJournalling.md` and
`CLAUDE/PlanScriptStandards.md` exist, are actively referenced from `CLAUDE/PlanWorkflow.md`, and
have **no row** in the index table. Measured: zero occurrences of either name in `CLAUDE.md`.

Combined with finding 7, the two omitted files are the only two that are *not* eagerly inlined into
every session — which is the correct behaviour, reached by accident.

**Fix**: add two rows.

### 4 ○ The NordVPN playbook's own on-screen instructions cannot work

`play-nordvpn-openvpn.yml:114` appends plaintext `nordvpn_username` / `nordvpn_password` lines into
`localhost.yml`, then in the same run (lines 125-134) tells the operator to run
`./vault.bash set nordvpn_username`. `vault.bash:171` refuses when the variable already exists —
which it now does, because line 114 just wrote it. The working command is `./vault.bash replace`.

`docs/nordvpn-installation.md:39-42` documents a **third**, different workflow again
(`ansible-vault encrypt_string` + manual paste).

**Fix**: change the playbook's message to `replace`; align the doc with whichever workflow is kept.

### 5 ✔ `helpers/CLAUDE.md` documents a test command that silently runs zero tests

`helpers/CLAUDE.md:76` says to run `python3 -m unittest discover -s tests`. I ran it:

```
Ran 0 tests in 0.000s
OK
```

Exit 0, "OK", nothing executed. `scripts/qa-helper-tests.bash:5-6` explains why — the helper
packages are namespace packages with no `__init__.py`, so `discover` cannot import the start dir and
collects nothing.

This is the exact false-green failure the project made policy about: *"A gate reporting `0 files` is
a FAILURE, not a pass"* (`CLAUDE/QA.md:38`). A doc is teaching the pattern the repo banned.

**Fix**: replace with `./scripts/qa-helper-tests.bash`.

### 6 ✔ `extensions/CLAUDE.md` mandates the command `CLAUDE/QA.md` forbids

`extensions/CLAUDE.md` says to run `npm run lint` three times, including under "Critical Safety
Rules — ALWAYS RUN ESLINT". `CLAUDE/QA.md:71` says verbatim: *"Run ESLint via the binary directly
(NOT `npm run lint` — blocked by hooks)"*.

`extensions/CLAUDE.md:3` cites `QA.md` as its authority and then contradicts it.

**Fix**: use the eslint-binary form, or add `llm:lint` scripts and update `QA.md`.

### 7 ✔ `CLAUDE.md` violates the doc-organisation policy stated inside `CLAUDE.md`

`CLAUDE.md` uses `@CLAUDE/<file>.md` import syntax 22 times. The `markdown_organization` guidance in
the same file says: *"Link docs with plain markdown links (zero token cost until followed); **avoid
`@`-imports** (they re-inline eagerly rather than defer)."*

Not theoretical: the review pass measured ~108,900 bytes of topic files eagerly inlined at session
start for a task that needed none of them. The two newest topic docs (finding 3) correctly use plain
deferred links from `PlanWorkflow.md` — the project already knows the right pattern; the older table
was never migrated.

**Fix**: convert the `@CLAUDE/*.md` references to plain markdown links.

### 8 ✔ "Six stages" is documented; seven gates run

`CLAUDE/QA.md:15` and the CI job name both say six. `scripts/qa-all.bash` runs six jq-merged stages
(lines 30, 39, 48, 58, 67, 76) **plus** `qa-nokill-containerwatch.bash` at line 90 — a real gate that
can fail the run. Its separate structure is deliberate (it would corrupt the JSON merge); its absence
from the docs is not.

**Fix**: document the no-kill safety gate in `QA.md`.

### 9 ✔ The "Complete catalog" omits four real optional playbooks

Measured — on disk, zero mentions in `docs/playbooks.md`:

| Playbook                                                     | In catalog |
| ------------------------------------------------------------ | ---------- |
| `optional/common/play-cloudflare-dns.yml`                    | 0          |
| `optional/common/play-container-watch.yml`                   | 0          |
| `optional/common/play-disk-reclaim.yml`                      | 0          |
| `optional/experimental/play-virtualbox-windows-vm-setup.yml` | 0          |

**Fix**: add catalog entries.

### 10 ○ `docs/playbooks.md` mischaracterises two playbooks

- `playbooks.md:98-104` says `play-network-wait-tuning.yml` caps the wait-online timeout to 5s. The
  play now **masks the unit outright** (Plan 00053); the 5s drop-in survives only as a dormant
  fallback. The doc describes superseded behaviour.
- `playbooks.md:272` says `play-lxc-install-config.yml` installs "LXC and LXD packages". No LXD
  package appears in the file. The mislabel probably traces to a task named *"Enable LXD Copr
  Repository"* for the `ganto/lxc4` repo — which is an LXC repo, so this may be a **source** naming
  bug rather than only a doc slip.

**Fix**: rewrite both; check whether the LXD task name is itself wrong.

### 11 ✔ `docs/fast-file-manager.md` documents a feature that does not exist

The doc mentions "tracker" 11 times — a `fast_file_manager_disable_tracker` variable,
`tracker-*.service` masking, and a re-enable troubleshooting section. The playbook mentions it **zero**
times; its `vars:` block defines only `disable_thumbnails` and `apply_gsk_fix`.

Meanwhile the play's real behaviour — `remember-recent-files=false` plus deleting
`recently-used.xbel`, fixing a FUSE stat-storm — is undocumented.

**Fix**: delete the tracker sections; document what the play actually does.

### 12 ○ The Whisper model table is stale

`docs/features/speech-to-text.md` lists tiny/base/small/medium/large-v3.
`extensions/speech-to-text@fedora-desktop/extension.js:74-83` also offers `auto`, `large-v2`,
`large-v3-turbo` and further English-only variants.

**Fix**: regenerate the table from `_whisperModels`.

### 13 ✔ `README.md`'s security-guidelines link is broken

`README.md:245` links to `CLAUDE.md#-public-repository-warning`. No heading in `CLAUDE.md` matches —
`CLAUDE.md:28` is `### Public Repository — Never Commit Secrets`. The matching heading text now lives
at `CLAUDE/SecurityRules.md:3` (`## Public Repository Warning`), and even that would not match the
link's leading hyphen. Stale since the modular-`CLAUDE.md` restructure moved the content.

**Fix**: point at `CLAUDE/SecurityRules.md#public-repository-warning`.

### 14 ○ Neither architecture nor development doc mentions dual desktop/server provisioning

Zero hits for "server", "headless", "provisioning_profile" or "scope:" in either file — despite this
being the repo's defining architectural fact (`CLAUDE/AnsibleStyle.md`'s Provisioning Profile
Self-Guard; every play declares a `scope`) and despite `README.md`'s own tagline naming it.

**Fix**: add a `provisioning_profile` / `scope` subsection to `architecture.md`.

### 15 ✔ The architecture directory tree omits real, populated directories

`files/` contains `etc/ home/ opt/ usr/ var/`. `docs/architecture.md` presents only `etc/`, `home/`,
`var/` as if exhaustive, and omits `helpers/` from the top-level structure entirely — despite
`helpers/` being CI-tested and having its own `CLAUDE.md`. Measured: zero mentions of `files/opt`,
`files/usr` or `helpers/` in the file.

**Fix**: add the missing directories.

### 16 ○ `docs/architecture.md:175` calls the vault password file "encrypted"

Reported as plaintext. **I did not verify this and deliberately did not open the file** — it is a
credential, and reading it is not something an audit needs. The claim is sound from
semantics alone: `ansible.cfg`'s `vault_password_file` names the file holding the *key* used to
decrypt vault values, so by definition it is not itself vault-encrypted.

**Fix**: reword to "Vault password file (plaintext, gitignored)".

### 17 ✔ Two `docs/README.md` links target non-existent anchors

- `docs/README.md:337` → `installation.md#version-mismatch`; the real heading is
  `installation.md:271` `### Version Mismatch Error` (slug `version-mismatch-error`).
- `docs/README.md:317` → `installation.md#post-installation`; no such heading exists (closest:
  `installation.md:242` `## Verifying Installation`).

Both 404 on GitHub.

**Fix**: correct both links.

---

## Suspected — not confirmed

Recorded so they are not lost, explicitly **not** established.

| #   | Suspicion                                                                                                             | Why it is not confirmed                                       |
| --- | --------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------- |
| S1  | `docs/development.md:246` hardcodes `/workspace/extensions`, a CCY-container-only path, in a doc that clones anywhere | `CLAUDE/QA.md` uses the same wording — may be deliberate      |
| S2  | `docs/features/claude-devtools.md`'s `podman run` reproduction omits `--init`, `--replace`, `--name`                  | plausibly a readability simplification; intent unknown        |
| S3  | `docs/headless-server-install.md`'s example vars omit `RUN_BASH_USER_LOGIN` / `RUN_BASH_USER_NAME`                    | the doc calls its example non-exhaustive and defers elsewhere |
| S4  | Whisper model **size** figures may disagree with `extension.js` (~500MB vs ~466MB)                                    | not enumerated past `large-v3-turbo`                          |
| S5  | `playbooks/dev/play-collect-diagnostics.yml` absent from the catalog                                                  | self-identifies as dev-only; exclusion may be intended        |
| S6  | `docs/features/README.md` is not linked from `docs/README.md`                                                         | no written rule requires it                                   |

---

## Checked and clean

The coverage ledger — what was verified and found accurate, so the audit's reach is known:

- `vars/container-defaults.yml` matches `CLAUDE/ContainerEngines.md` exactly.
- `group_vars/desktop.yml`'s `provisioning_profile` computation matches `CLAUDE/AnsibleStyle.md`.
- `roles/` hygiene claims in `CLAUDE/AgentNotes.md` — confirmed via `git ls-files`.
- `.github/workflows/qa.yml`'s clean-checkout steps (vault placeholder, `ansible-galaxy` role+
  collections, gitleaks OSS binary not the paid Action) match `CLAUDE/AgentNotes.md`.
- Branching: `F42`/`F43`/`F44` exist, remote `HEAD branch: F44`, `vars/fedora-version.yml` is 44.
- `ansible.cfg` claims across several docs (inventory path, `transport=local`, `sudo_flags=-HE`,
  vault settings, fact-cache path) verified byte-for-byte.
- Core/optional classification vs the real import graph — correct apart from finding 2.
- `docs/containerization.md`, `docs/ddev.md`, `docs/configuration.md` variable names and vault
  workflow: accurate.
- `docs/github-multi-account.md`, `docs/github-ssh-over-443.md`, `docs/headless-provisioning.md`,
  `docs/post-upgrade.md`: extensively verified, no confirmed defects.
- `docs/nordvpn-installation.md`'s `nord` CLI documentation matches `files/home/.local/bin/nord`
  exactly — only the vault instructions are wrong (finding 4).
- `docs/kitty.md`, `docs/UnifiSetupGuide.md`, `docs/ccy-debug-mounts.md`: accurate.
- Speech-to-text DBus signal names and `wsi` CLI flags match (only the model table is stale).
- Internal `#anchor` links across `architecture.md`, `development.md`, `playbooks.md`,
  `configuration.md`, `containerization.md`, `ddev.md`, `installation.md` resolve — except
  finding 17's two.
- `.ansible/roles/lts.vault-scripts/**/CLAUDE.md` is untracked (`.ansible/` is gitignored) —
  correctly out of scope.

## What this audit did not cover

- Plan bodies under `CLAUDE/Plan/NNNNN-*/`, vendored trees, and `untracked/` — excluded by scope.
- Whether each **fix** is correct. Every "Fix" line above is a proposal, not a verified remedy.
- The six suspected items, by definition.
