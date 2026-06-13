# Repo-Development Playbooks

**These plays are for developing THIS repo, not for configuring the host.**

## Scope

Plays under `playbooks/dev/` are tools that help us work on the repo itself:
gathering diagnostic snapshots, auditing host state against the playbooks,
extracting evidence for issue triage, and similar dev-time chores.

They are **never** imported by `playbooks/playbook-main.yml` and they
**never** belong in the standard converge run.

## How `dev/` differs from `imports/`

| Concern            | `playbooks/imports/`                                 | `playbooks/dev/`                                         |
| ------------------ | ---------------------------------------------------- | -------------------------------------------------------- |
| Purpose            | Configure / provision the host                       | Help us develop and audit the repo                       |
| When run           | As part of `playbook-main.yml` or as opt-in features | Ad-hoc, by a developer or agent investigating something  |
| Mutates host state | Yes (packages, files, services)                      | No — read-only data gathering / reporting                |
| Idempotent         | Required                                             | Snapshot-style (each run produces a new timestamped dir) |
| Output location    | The host's filesystem (`/etc`, `/usr/local`, ...)    | `untracked/` only — gitignored, never reaches the host   |
| CCY container safe | No — must run on HOST                                | No — must run on HOST (probes need real systemd / DBus)  |

## Rules for plays added here

1. **Read-only by default.** A dev play must not install packages, enable
   services, modify `/etc`, or otherwise alter the host. If a play needs
   to install a tool to do its job, that install belongs in `imports/`
   first (per the Infrastructure-as-Code rule); the dev play just uses it.
2. **Output lives under `untracked/`.** Anything a dev play writes goes
   into `untracked/<topic>/` so it stays out of git. Use a timestamped
   subdir if a single run can be re-run usefully.
3. **No import from `playbook-main.yml`.** A dev play is invoked directly
   by path. It must not be listed in `playbooks/playbook-main.yml` or any
   `imports/` import chain.
4. **Standard playbook hygiene still applies.** Shebang, executable bit,
   `hosts: desktop`, `root_dir` via the config lookup pattern, and QA
   gates — see `CLAUDE/AnsibleStyle.md` and `playbooks/CLAUDE.md`.
5. **Run on the HOST.** Dev plays talk to the live system (journalctl,
   dmesg, dbus, etc.) and must not run inside a CCY container.

## Where the heavy lifting lives

Dev plays should stay thin — orchestration only. Any non-trivial logic
(loops, command catalogues, output formatting) goes in a regular bash or
python script under `scripts/`, and the play calls it via
`ansible.builtin.command`. That keeps `playbooks/` purely about Ansible
and `scripts/` as the single home for executable tooling.

## Current plays

- `play-collect-diagnostics.yml` → `scripts/collect-diagnostics.bash`.
  Captures a full system snapshot (journals, dmesg, systemd state,
  hardware, packages, GNOME session, ...) into
  `untracked/diagnostics/<timestamp>/`, with a `README.md` and a
  `_manifest.tsv` for agent-driven analysis.
