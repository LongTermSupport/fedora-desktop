# Design: the self-update follows signed release tags

Plan 00153 Task 1.2. Design only; Phase 3 builds and tests it. Read from the code, not run:
`files/usr/local/sbin/fedora-desktop-self-update`, `helpers/self_update/update.py`,
`gate.py`, `cycle.py` and the contract in
`CLAUDE/Plan/00137-unattended-server-self-update/DESIGN-cycle.md`.

## How a commit is chosen today

`update.py` fetches only `refs/heads/<BRANCH>` (`--no-tags`), then walks the tip's
first-parent history down to the deployed commit and deploys the **newest commit signed by
the pinned principal**. One signed commit vouches for the unsigned or foreign-signed
commits beneath it, back to the deployed one. A commit above it with a bad signature
(`B`, `R`, `E`, unknown) refuses the whole cycle; unsigned (`N`) and not-the-owner (`U`)
are passed over. Two more modes keep one invariant, "the deploy clone's HEAD is a commit
the pinned signer signed": `--anchor` (the play, after cloning) moves the branch back to the
newest such commit, and `verify_head` (the wrapper and the cycle) checks it before root
imports anything from the clone.

What this cannot say: whether a point is **releasable**. Any owner-signed commit on the
branch is deployed, finished or not.

## The design: the newest signed release tag for this Fedora major

A release is a tag `<major>.<minor>.<patch>` (Plan 00153 Task 1.1). The credential moves
from "a signed commit" to "a signed tag on a signed commit"; the rest of the machinery
(pinned git settings, the signers-file check, the Fedora pin check, fast-forward only,
marker lines, the `HEAD` invariant) stays.

### The major

From `BRANCH` in `self-update.conf`: `F44` gives `44`. A `BRANCH` that is not `F<digits>` is a
config error (`ConfigError`), because there would be no tag family to follow. The target's
`vars/fedora-version.yml` is still checked against the running system, so a host cannot be
moved onto a release of another Fedora.

### The fetch

```
git fetch --prune remote \
    +refs/heads/F44:refs/remotes/remote/F44 \
    refs/tags/44.*:refs/tags/44.*
```

- The tag refspec has **no `+`**: git refuses to move a local tag that now points elsewhere.
  That failure is `EXIT_TAG_MOVED`, never retried with force. A release tag that moved
  is either the owner's mistake or an attack, and a person decides which.
- `--prune` applies to these refspecs, so a tag the owner withdrew upstream disappears
  here too and cannot be chosen after its withdrawal. Consequence, stated: if the withdrawn
  tag is the deployed one, the next-newest tag is *behind* HEAD, and the cycle refuses as
  "ahead of the newest release" until a person decides (it never downgrades by itself).

### Choosing the target

1. List the tags matching `^44\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$` exactly. Anything else
   under `44.*` (`44.1.0-rc1`, `44.01.0`) is ignored with a line on stderr, not an error:
   the namespace is the owner's, and a draft tag must not stop the fleet.
2. Order by **numeric** version, never by string or creation date (`44.10.0` beats `44.9.0`).
3. Take the highest. It is judged as follows; the first failure refuses, and **the cycle never
   falls back to an older tag**, because skipping the newest release because it looks wrong
   is how a withdrawn or tampered release gets quietly replaced by an old one:
   - it is an **annotated** tag (`%(objecttype)` is `tag`): a lightweight tag carries no
     signature and is refused (`EXIT_TAG_REFUSED`);
   - `git verify-tag` with the pinned `gpg.ssh.*` settings succeeds, and its stated
     principal is `PRINCIPAL` (the same string comparison `gate.judge` makes for commits).
     A good signature by another key is refused too: a tag by someone else is not a release;
   - the commit it points to is reachable from `refs/remotes/remote/F44`, so a tag on a side
     branch is not a release of this branch (`EXIT_TAG_REFUSED`);
   - that commit **also** carries a good signature by `PRINCIPAL` (the existing `_verdict`).
     This keeps the wrapper's invariant with no change to the wrapper: HEAD is an owner-signed
     commit. It also fixes what a release is made of: the release command (Task 2.1) must
     create a signed commit to tag (the changelog commit it writes anyway), because a GitHub
     web merge commit is signed by GitHub, not the owner, and tagging it would be refused;
   - the Fedora pin in that commit matches the running system (existing check).
4. If its commit is HEAD: `SELF-UPDATE-NOTHING`. If HEAD is an ancestor of it: fast-forward.
   Otherwise (HEAD ahead of it, or the histories diverged) refuse with the existing
   `EXIT_AHEAD` / `EXIT_DIVERGED` messages worded for tags.
5. No matching tag at all: **refuse loudly** (`EXIT_NO_RELEASE`), naming the release command.
   Never fall back to the branch tip. A fresh server therefore cannot be provisioned until
   the major's first release exists, which is the owner's point: tag `44.0.0` before the
   rollout (Plan 00137 has no running server yet, so there is nothing to migrate).

### The two other modes

- `--anchor`: after cloning, `checkout -B <branch> <newest valid tag's commit>` instead of
  walking first-parent history. No valid tag is `EXIT_NO_RELEASE`; the existing untracked-file
  refusals stay.
- `verify_head`: unchanged. After a tag-mode update HEAD is the tagged, owner-signed commit.

### Markers, exit codes and what the human sees

- A new marker `SELF-UPDATE-TAG <name>` beside `-NEW` / `-TARGET` / `-NOTHING`, carried into
  the published status so the host-health report shows "release 44.2.1", not a bare sha.
- New internal exit codes in `update.py`: `EXIT_NO_RELEASE` (20), `EXIT_TAG_REFUSED` (21),
  `EXIT_TAG_MOVED` (22). `cycle.py` already treats any non-zero update status as "the update or
  trust gate refused" (wrapper exit 20, alert sent); only the detail text names the new cause.

### What changes in the files

| File                                              | Change                                                                          |
| ------------------------------------------------- | ------------------------------------------------------------------------------- |
| `helpers/self_update/update.py`                   | tag listing, ordering, judging; new fetch; the anchor; the new codes and marker |
| `helpers/self_update/gate.py`                     | `parse_release_tag`, `choose_release` (pure, tested without git)                |
| `helpers/self_update/cycle.py`                    | read the new marker, carry the tag name into the result and status              |
| `files/usr/local/sbin/fedora-desktop-self-update` | header and refusal text only                                                    |
| `playbooks/.../play-self-update.yml`              | the anchor's wording; no new variables                                          |
| `CLAUDE/Plan/00137-.../DESIGN-cycle.md`           | a CORRECTION note under D3: the credential is now a signed tag                  |

## Tests (Phase 3 writes them first, in `tests/helpers/self_update/`)

Pure, in `test_gate.py`: the tag-name regex; numeric ordering (`44.10.0` over `44.9.0`);
`choose_release` with no tags, one, several, and a non-matching name among them.

With real git and real SSH signatures in a temp repository (as `test_update.py` already
does), one case each: newest tag deployed; no tag refused; lightweight tag refused; tag signed
by another key refused; highest tag bad while an older one is good refused (no fallback); tag
on a commit not signed by the principal refused; tag on a commit not on the branch refused;
a tag moved upstream refused without force; a tag withdrawn upstream (prune) with HEAD at it
refused as ahead; HEAD already at the newest tag is "nothing"; diverged histories refused;
Fedora pin mismatch refused; `--dry-run` names the target and moves nothing; `--anchor` lands
on the newest valid tag and refuses when there is none. `scripts/test-self-update-cycle.bash`
gains the end-to-end cycle against a tagged repository.

## Decisions for the owner (with the recommendation)

1. **Tag the signed commit the release command makes**, not whatever merge is on top. Needed
   so the wrapper's "HEAD is owner-signed" invariant stays unchanged. Recommended.
2. **Replace branch-tip deployment with tags outright**, no mode switch: no server runs the
   unattended cycle yet. Recommended; a switch would be a second code path to test and to
   leave behind.
3. **Refuse, never fall back**, when the newest tag is unusable, including a withdrawn
   tag that was deployed. Recommended; the cost is a person has to look.
