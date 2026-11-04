# Releases

A release is a signed git tag `<fedora-major>.<minor>.<patch>` on the release branch for a
Fedora version: `44.0.0`, `44.1.0`, `44.1.1` on branch `F44`, then `45.0.0` on `F45`. The major
follows Fedora; minor is a new feature, play or visible behaviour; patch is a fix only.

Development carries on on the branch as usual. A point is tagged once CI passes on it and the
plans it ships have passed their checks on a real host. `CHANGELOG.md` (created by the first release) lists what
each release contains (`ccy-changelog.md` stays the launcher's own).

## Making a release

The release workflow (`.github/workflows/release.yml`) cuts every release after the first of a
major. It costs the owner one approval of the `release` environment, one merge, and a second
approval:

1. **A push to `F44`** that has something releasable starts it. What counts is the title of each
   merged pull request: `feat:` is a minor, `fix:` a patch, anything else releases nothing, and a
   breaking marker (`feat!:` or a `BREAKING CHANGE:` line) is refused on a major that already has a
   release. Pull requests bound for `F44` therefore carry a conventional title, and the check
   `pr-title.yml` fails one that does not (making it a required check is the owner's call).
2. After the first approval the workflow opens a pull request with the signed `Release X.Y.Z`
   commit (the changelog entry).
3. **Merge that pull request with a merge commit.** That push starts the second run.
4. After the second approval the workflow signs the annotated tag on the release commit itself
   (not on GitHub's merge commit, which servers refuse), pushes it and creates the GitHub Release.

It refuses, and pushes nothing, when the release key is missing, the tag already exists, the
release commit is unsigned or off the branch, or CI has not passed on it. If the GitHub Release
step fails after the tag is pushed, create it by hand (`gh release create X.Y.Z --verify-tag --title X.Y.Z --notes-file <the entry>`); servers read the tag, not the Release.

### Set up once (owner)

Only the owner can create these; the workflow stops at its first step until they exist.

- The `release` environment, with a required reviewer and the branch limit `F*`.
- Its secret `RELEASE_SIGNING_KEY`: a new, unencrypted SSH key made for this, never reused.
- That key's public half as a **deploy key with write access** (a push by the key starts CI; a
  push with the workflow's own token never does).
- The same public line added to `self_update_signing_public_keys` on every server, then the play
  re-run ([configuration.md](configuration.md#unattended-server-self-update)). Servers trust the
  key under the owner's principal, so it can sign anything a server accepts: keep it only in the
  environment.
- A tag ruleset: release tags `[0-9]*.[0-9]*.[0-9]*` immutable, creatable only by the deploy key.
- The repository setting that lets Actions create pull requests.

## Making a release by hand (owner)

The first release of a major is made by hand, because the history to judge is the whole branch,
and the same two steps are the fallback if the workflow cannot run:

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
tag is pushed, run `tag` again to finish it. Agents never run it unless the owner asks.

## What servers do with a tag

The unattended server self-update deploys the newest release tag by default, not the branch tip
(`self_update_channel: tags`; `branch` is the explicit opt-in to the tip). A branch with no release
tag, or a newest tag that fails its checks, is refused rather than answered with an older tag, so
tag `<major>.0.0` before provisioning a server. Never move a release tag that was already pushed:
servers refuse it until a person withdraws it and releases under a new number. See
[configuration.md](configuration.md#unattended-server-self-update). The design, and why each
choice was made:
[DESIGN-release-workflow.md](../CLAUDE/Plan/00153-release-tags-fedora-major-semver/DESIGN-release-workflow.md).
