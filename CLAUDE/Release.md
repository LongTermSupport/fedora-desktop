# Releases (agent rules)

Release tags are `<fedora-major>.<minor>.<patch>` on `F<major>`, signed and annotated. They are made
only by the release workflow (`.github/workflows/release.yml`), after the owner approves its
protected environment, or by the owner with `scripts/release.bash` (a major's first release, and the
fallback); see [docs/releases.md](../docs/releases.md).

- **Never tag, and never run `scripts/release.bash`, unless the owner explicitly asks.** A release is
  the owner's call: the unattended self-update deploys the newest tag (its default channel;
  `self_update_channel: branch` opts into the tip). The approval of the release environment is the
  owner's and no one else's.
- **A pull request bound for `F<major>` carries a conventional title** (`feat: ...`, `fix(scope): ...`,
  `docs: ...`): the release workflow reads the title of each merged pull request, and `pr-title.yml`
  refuses one that is no header. `feat` makes a minor release and `fix` a patch; any other type
  releases nothing. A breaking marker (`feat!:`, or a `BREAKING CHANGE:` line in the body) is refused
  on a major that already has a release, so it is the owner's decision, never an agent's.
- Never create, move or delete a tag by hand. A release tag that moves while a server still holds
  the old one is refused by that server until a person clears it (withdraw it upstream and release
  under a new number); a withdrawn tag later re-created under the same name is accepted as new, and
  still has to pass every signature, ancestry and fast-forward check.
- Minor is a feature, play or visible behaviour; patch is a fix only; major only at a new Fedora
  branch's first release. Releasable means CI passes and the plans it ships passed host acceptance.
- The release commit is signed by the release key (the workflow) or the owner (by hand) and tagged
  itself, because a GitHub merge commit is signed by GitHub and the self-update would refuse it. Design:
  [CLAUDE/Plan/00153-release-tags-fedora-major-semver/DESIGN-self-update-tags.md](Plan/00153-release-tags-fedora-major-semver/DESIGN-self-update-tags.md)
  and [DESIGN-release-workflow.md](Plan/00153-release-tags-fedora-major-semver/DESIGN-release-workflow.md).
- What the workflow promises is held by tests, not by review: every action pinned by commit SHA,
  each job's permissions pinned exactly, no `pull_request_target`, no workflow expression inside a
  shell script, the key's secret only in the `release` environment, and `qa.yml` letting the
  release branch through its base check (`src-go/qa/toolchain`), and what decides a release by
  `src-go/internal/release`. The release script's tests are `scripts/test-release.bash`,
  a hard gate in `qa-all.bash`.
