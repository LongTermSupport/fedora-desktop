## 21:00 · action · 5.2   — the version computation, built ahead of the Task 5.1 decision

Task 5.2 built ahead of Task 5.1's decision, on `feat/release-version`: `src-go/internal/releasever`, the next
release version from the commits since the last release. Issue 82 (the owner's) states the rule, so the
computation did not have to wait for the key, environment and ruleset decisions of 5.1; those bind Task 5.3's
workflow, not this function.

**Rules (V1-V6, in the package doc, each pinned by a test).** A release is on `F<major>` and named
`<major>.<minor>.<patch>`, the major being the branch's. Only a conventional header decides: `feat` is a minor,
`fix` a patch, the highest wins and a minor resets the patch; every other type, and a subject that is no header
(a merge, a revert, a `Release x.y.z` commit, a plain sentence), releases nothing; a range with nothing
releasable is no release. A breaking marker (`!` before the colon, or a `BREAKING CHANGE:` or `BREAKING-CHANGE:`
line starting a line of the body) is refused on any type once the major has a release; the major's first release
is `<major>.0.0` and allows one, because the major itself is what changes.

**Proof.** The suite was written first and was red by not compiling. It passes under `-race -shuffle=on` at 93.7%
coverage with golangci-lint clean. Two mutations turned it red and were restored byte for byte: the breaking check
inverted (eight cases fail), and a minor bump that kept the patch (four cases fail).

**For Task 5.1, an owner question this surfaced.** This repository's commit subjects mostly carry no conventional
header (`Plan 00246: ...`, `Add play-...`, `Merge ...`, `Auto: ...`), and only a `feat` or `fix` header releases.
Without a commit-message convention (a `commit-msg` check on F44, or squash-free merges that keep the typed
subjects), a merge to F44 would rarely produce a release. That is a design decision for the owner, not a defect
here. Not built: the command that reads `git log -z` and the last tag and calls this package (Task 5.3's), and
the release workflow itself.
