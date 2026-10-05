# Releases

A release is a signed git tag `<fedora-major>.<minor>.<patch>` on the release branch for a
Fedora version: `44.0.0`, `44.1.0`, `44.1.1` on branch `F44`, then `45.0.0` on `F45`. The major
follows Fedora; minor is a new feature, play or visible behaviour; patch is a fix only.

Development carries on on the branch as usual. A point is tagged once CI passes on it and the
plans it ships have passed their checks on a real host. `CHANGELOG.md` (created by the first release) lists what
each release contains (`ccy-changelog.md` stays the launcher's own).

## Making a release (owner)

```bash
scripts/release.bash prepare first|minor|patch    # changelog entry, signed commit, pull request
# merge the pull request with a merge commit, then:
git switch F44 && git pull --ff-only
scripts/release.bash tag 44.1.0                   # signed tag, pushed, GitHub Release
```

`prepare` works from a clean `F<major>` that matches its remote and has green CI, and opens the
pull request for you to merge. `tag` signs the tag on the signed release commit itself, not on
GitHub's merge commit, and checks signatures against your own `user.signingkey`. Both refuse, with a
reason, rather than guess; `--dry-run` says what each would do. If the GitHub Release fails after the
tag is pushed, run `tag` again to finish it. Agents never run it.

The unattended server self-update will deploy the newest release tag instead of the branch tip
(Plan 00153, Phase 3).
