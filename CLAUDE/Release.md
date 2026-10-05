# Releases (agent rules)

Release tags are `<fedora-major>.<minor>.<patch>` on `F<major>`, signed and annotated, made only
by `scripts/release.bash` (see [docs/releases.md](../docs/releases.md)).

- **Never tag, and never run `scripts/release.bash`, unless the owner explicitly asks.** A release is
  the owner's call: the unattended self-update deploys the newest tag.
- Never create, move or delete a tag by hand. A tag that moves is refused by the self-update.
- Minor is a feature, play or visible behaviour; patch is a fix only; major only at a new Fedora
  branch's first release. Releasable means CI passes and the plans it ships passed host acceptance.
- The release commit is signed by the owner and tagged itself, because a GitHub merge commit is
  signed by GitHub and the self-update would refuse it. Design:
  [CLAUDE/Plan/00153-release-tags-fedora-major-semver/DESIGN-self-update-tags.md](Plan/00153-release-tags-fedora-major-semver/DESIGN-self-update-tags.md).
- The release command's tests are `scripts/test-release.bash`, a hard gate in `qa-all.bash`.
