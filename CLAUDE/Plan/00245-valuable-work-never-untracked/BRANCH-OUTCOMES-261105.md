# The FINISHED branches of the triage: what became of each (2026-11-05)

The triage ([a](RESEARCH-branch-triage-261104-a.md), [b](RESEARCH-branch-triage-261104-b.md),
[c](RESEARCH-branch-triage-261104-c.md)) marked 20 branches FINISHED. This is what was done
with each. Branches merged into F44 are deleted from the remote once F44 holds them.

## Merged into F44 directly

- `fix/qa-changed-gate-args`: qa-changed runs each gate with its row's arguments (efb74f482).
- `fix/ccy-teams-launch-start-window` (PR #132): the launch-start check owns its clock (bdc37c565).
- `feat/matrix-clients-element-install`: Element's install and keyring grant are one task file
  that both Element plays import (11ca29312); its host run is queued.

## Merged through `docs/finished-reports-261105`

- 13 report branches, one report each: `agent-a772edc8fcc891172-8468e47f`,
  `agent-aae326b6898d21a0b-62df0496` (minus a stray 5-byte file),
  `agent-af6d5f14f2f32c8fb-06029089`, `research/bus-stack` (its journal entry appended to F44's
  redacted journal), `research/v2-scope`, `review/lxc-checkout-typed`,
  `review/lxc-checkout-typed-final`, `review/teams-member-roles`, and the five
  `wip-rescue/agent-*` review reports.
- `design/seat-broker`: the Plan 00197 design and its review. The plan is Not Started, and v2's
  seat pool may supersede it; the design is kept as the record either way.
- `worker/433-unowned-decisions`: the correction of decisions attributed to the owner beyond the
  record. Its five-line attribution note on `Completed/002-nordvpn-openvpn-manager/PLAN.md` was
  left out. The pre-commit secret scanner reads an existing Ansible `--vault-id <label>@<source>`
  argument in that file as an email address, and it judges every file a commit touches. That is
  a scanner false positive (a Defence Before Fix item, not yet started). The note goes in once
  the scanner is fixed.

## Not merged

- `skip-docker-env`: **SUPERSEDED**, not FINISHED. Headless installs are fully supported, and
  rootful Docker is no longer in `playbook-main.yml`: it is the optional
  `playbooks/imports/optional/common/play-docker.yml`, and a headless install chooses its optional
  plays with `RUN_BASH_OPTIONAL_PLAYBOOKS`, so a Podman-only box never gets Docker unless asked.
  The branch patches `playbooks/imports/play-docker.yml`, a path that no longer exists. It joins
  the deletable set.
- `feat/release-version` (Plan 00153): built and green, but waits for the owner's GitHub objects
  (release environment, signing secret, deploy key, tag ruleset, the setting that lets Actions
  open pull requests). The DC worker opens it as a draft pull request.
