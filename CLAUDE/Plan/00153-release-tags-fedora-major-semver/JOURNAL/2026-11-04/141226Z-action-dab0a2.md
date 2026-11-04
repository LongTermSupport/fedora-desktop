## 14:12 · action · —   — Task 5.3 built; 5.4 documents written

Task 5.3 is built, from the owner's D1 to D4. Task 5.4's documents are written; its `qa-reviewer` pass is owed.

**What was built, test first.**

- `internal/gitx/release.go`: the four questions the release asks git (the release tags, the first-parent log
  with parent counts and whole messages, the commits whose subject matches, whether a remote branch exists).
  Each reads strictly; a git that cannot answer is an error.
- `internal/release` (`release-plan decide`, `prepare`, `notes`): R1-R7 in the package doc. A merge commit stands
  for the title and body of its pull request (read through `gh api --jq`, reshaped to camelCase so no lint
  exception is needed); the first release of a major is left to `release.bash prepare first`; a release commit
  merged without its tag is what a tagging run is for; a release branch already on the remote is not prepared
  twice. A malformed repository name or sha is refused before `gh` runs (a test found `..` passing the first
  pattern, and the fix is in).
- `cmd/release-plan`, a thin main with a row in `scripts/go-binary-suites.tsv`.
- `.github/workflows/release.yml`, the local action `.github/actions/release-key` and `pr-title.yml`.
- `TestTheReleaseWorkflowKeepsItsPromises` in `src-go/qa/toolchain`, with a fixture that breaks each promise.

**Proof.** The tests were written first and were red by not compiling. They pass under `-race -shuffle=on` (three
runs), golangci-lint is clean, coverage is 94.8% for `release` and 97.1% for `gitx`. Three mutations turned the
`release` suite red and were restored: an equal version counted as pending, the lowest pending release winning,
and a merge commit never asked about its pull request. Reasoning about the mutations showed one check that could
never fire (a tag test above the newest tag) and it was removed.

**Not verified, and why.** The workflow has not run: it needs the objects only the owner can create (D5), now listed
in `docs/releases.md`. Three facts about GitHub are stated from its documented behaviour and not tried here: a push
with the workflow token starts no workflow (why the key is also a deploy key), an SSH deploy key's push does, and a
ruleset can name a deploy key as its one bypass actor.

**Left for the owner (D5).** The `release` environment with its reviewer and the branch limit `F*`; the secret
`RELEASE_SIGNING_KEY`; its public half as a deploy key with write access and in every server's
`self_update_signing_public_keys`; the tag ruleset; the setting that lets Actions open pull requests.
