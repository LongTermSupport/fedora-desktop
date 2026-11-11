## 21:02 · decision · 5.1   — the release workflow's design, and the five decisions that are the owner's

Task 5.1's design half written: `DESIGN-release-workflow.md`. The owner's half, five decisions, is what Task 5.3
waits on, and the plan now says so by name.

**What the code says a release must be** (read, not run): the servers' self-update takes the newest
`<major>.<minor>.<patch>` tag and refuses it unless it is annotated, verifies against the allowed-signers file as the
one pinned principal, sits on a commit reachable from `F<major>`, and that commit is signed by the same principal. The
servers trust a list of keys under one principal (`helpers/self_update/signers.py`), so a second key is a list entry,
not a code change; a second principal is a code change in the gate, the cycle and the wrapper. `release.bash` makes
its own signed release commit and says `F<major>` is protected, so the workflow cannot just push one.

**The decisions** (each with a proposal): D1 the key and its principal (a listed key under the same principal now);
D2 how the release commit reaches a protected branch (a pull request merged by a person, or a bypass for the workflow's
identity); D3 what starts a run (a push, with the version step before the approval); D4 the commit-message convention
(only `feat` and `fix` headers release, and this repository's subjects mostly have none, so a pull-request title
check); D5 the environment, its secret and the tag ruleset, which only the owner can create and whose expressiveness
was not checked from the container.

**Not verified, stated in the design:** what a tag ruleset can express for the workflow's identity. Everything else in
the design is read from the repository's own code and docs. Task 5.3 (the workflow) and 5.4 (the docs) follow D1 to D5.
