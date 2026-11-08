## 05:31 · finding · —   — CI finds a root-only test in the admin wiring (PR 128 S8)

CI's first run on the review's head (f5343f39a) failed one test, `TestTeamCAIsTheHostsCA` in `admin/wire`, that passed in every container, which runs as root:

- Cause: review S8 made every CA read take the owner's uid, and `admin/wire` gave production a constant `stateOwnerUID = 0`. The test plants `ca.key` as the test user, so under CI's unprivileged user the read refused it, `owned by uid 1001, not 0`. Red reproduced as uid 1001 with `go test -exec 'setpriv --reuid=1001 --regid=1001 --clear-groups' ./internal/teamchat/admin/wire/`.
- Class: a test or code path that works only because root is not subject to permission checks. Already in GoProjectRules.md's list of classes that cannot be a rule (no syntax tree shows the assumption); this is its third form, and CI's unprivileged user is its detector. The tree-wide run as another uid fails for the setup's own reasons (a uid with no account, files it cannot read), so it is not a gate.
- Fix: the owner is a field of the wiring's `machine` (root in `production()`, asserted by `TestNewIsTheHostsWiring`); the CA test gives the machine its fixture's owner. Green as root and as uid 1001. GoProjectRules.md names the form and the reproduction.
- Every integration branch carrying f5343f39a (S12 to S15) failed the same test and needs this commit merged down.
