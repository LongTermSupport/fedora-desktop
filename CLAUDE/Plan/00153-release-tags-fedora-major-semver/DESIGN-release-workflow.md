# Design: the automated release workflow (Task 5.1, issue 82)

Plan 00153 Task 5.1. The design half of the gate: what the workflow must satisfy, read from the code
that servers run, and the decisions that are the owner's. Nothing here is built except the version
computation (Task 5.2, `src-go/internal/releasever`). Task 5.3's workflow waits on D1 to D5.

## What a server demands of a release (from the code, not from the issue)

Read from [DESIGN-self-update-tags.md](DESIGN-self-update-tags.md), `helpers/self_update/signers.py`
and `files/usr/local/sbin/fedora-desktop-self-update`:

- The newest tag `<major>.<minor>.<patch>` is chosen by numeric order and judged: it is **annotated**,
  `git verify-tag` passes against the server's allowed-signers file with the signer equal to the one
  pinned `PRINCIPAL`, its commit is **reachable from `F<major>`**, and that commit **also** carries a
  good signature by the same principal. A tag that fails is refused; there is no fallback to an older tag.
- A server trusts a **list of public keys, all under one principal**: `self_update_signing_public_keys`
  becomes one `<principal> <key>` line per key (`signers.py`). A second key is one more list entry
  on each server and no server code change; a second principal is a code change (the wrapper and the
  cycle pin one `PRINCIPAL`).
- A tag that moves upstream is refused by every server that holds the old one until a person clears
  it, and a release the owner withdrew is never replaced by an older one on its own.

## What the workflow has to produce

A **signed release commit** reachable from `F<major>`, and a **signed annotated tag** on that commit,
both by a key the servers list. GitHub's own merge commit is signed by GitHub, which a server refuses,
so the tag cannot sit on a merge commit. That is why `scripts/release.bash` makes its own signed
`Release <version>` commit and tags that.

## Decisions for the owner

**Decided on 2026-11-04, each as proposed:** D1 (a), D2 (a), D3 a push trigger, D4 the title check.
D5 is not a choice but the owner's own actions, listed under "What Task 5.3 builds". The options and
the reasons stay below as the record of what was weighed.

**D1. The key and its principal.**

- (a) The release key is one more entry in `self_update_signing_public_keys`, under the same principal
  as the owner's keys. No server code change. The key then has the owner's authority for every server
  that lists it: it can sign any commit or tag the servers would accept. It lives only in the protected
  environment, so its safety is the environment's.
- (b) A second principal for releases only, with servers accepting release tags from it and commits
  from either. Stronger separation, but a change to `gate.py`, `cycle.py`, the wrapper and their tests,
  and a migration of every server's allowed-signers file.
- Proposal: (a) now; (b) only if the owner wants the release key to be unable to sign an ordinary commit.

**D2. How the release commit reaches a protected branch.** `release.bash` says `F<major>` is protected
(changes arrive by pull request), so the workflow cannot simply push a commit to it.

- (a) The workflow opens a pull request with the signed release commit, a person merges it with a
  merge commit, and a second run tags the release commit (what `release.bash prepare` and `tag` do, with
  the two steps run by the workflow). Branch protection stays whole; a release costs the owner the
  environment approval and one merge.
- (b) The workflow's identity is a bypass actor on `F<major>` and pushes the signed release commit
  directly. One approval, but the workflow can then write to the protected branch.
- Proposal: (a). The acceptance in #82 says "after one approval"; (a) is two clicks, and the owner decides
  whether that counts.

**D3. What starts a run.** Every push to `F<major>`, a manual dispatch, or a schedule. Each run needs the
environment's reviewer, and a range with no releasable commit makes no release (Task 5.2), so a push
trigger would start runs that stop at the version step. Proposal: a push trigger, so the approval is
asked only for a range that has something to release, with the version step run before the environment
job (the version step reads no secret).

**D4. The commit-message convention.** Only a `feat` or `fix` header releases, and this repository's
commit subjects mostly have none (`Plan 00246: ...`, `Add play-...`, `Merge ...`, `Auto: ...`). Without
a convention a merge to `F44` would rarely release. Options: a `commit-msg` and pull-request-title
check that requires a conventional header on F44-bound work; or the workflow reads the merged pull
request's title instead of each commit. Proposal: the title check, since merges are by pull request.

**D5. The tag ruleset.** Release tags (`[0-9]*.[0-9]*.[0-9]*`) immutable, creatable only by the workflow.
What a ruleset can express, and whether the workflow's identity can be the one actor allowed to create
a matching tag, has to be read from the repository's settings; it was not checked from the container,
and an agent has no access to those settings. The owner creates the environment (with its reviewer and
the release branch limit), its secret (the private key, never in the repository or a transcript) and
the ruleset; Task 5.3 names them.

## What Task 5.3 builds once D1 to D5 are answered

One workflow, `release.yml`: the version step (Task 5.2, no secret, no write permission) decides the
next version or stops; the release job runs in the protected environment, signs the release commit and the
annotated tag with the environment's key, and fails closed on a missing key, an unapproved run or an
existing tag; the GitHub Release is created with generated notes. Third-party actions are pinned by
commit SHA, `permissions:` is the least per job, there is no `pull_request_target`, and no untrusted text
(a commit message, a title) reaches a shell step except through an environment variable. Task 5.4 then
writes `docs/releases.md`, replaces `CLAUDE/Release.md`'s "never tag unless the owner asks" and the Non-Goal
of this plan, and adds the workflow's row to the workflow documentation.
