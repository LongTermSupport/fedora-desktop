## 22:06 · action · —   — PR 128 review blockers B1 to B3, S2 and S3 fixed

The coordinator's review of PR 128 (FIX-FIRST, 2026-11-07, three blockers) is fixed on the branch.

- **B1:** the team backup carries the CA as manifest version 2 (`ca.crt` 0644, `ca.key` 0600), and a restore keeps the CA's fingerprint; a version 1 backup still restores and leaves the team's CA as it is; a backup of another CA is refused before anything changes.
- **B2 (S16):** the production System serves the team CA through `host.CAPEM`, so an https bundle and an LXC reach fact can be issued.
- **B3:** both teamchat drop-ins write `IPAddressDeny=any` and `SocketBindDeny=any` beside their allow lines. The deny in `sandbox.go` belongs to the homeserver's base unit and systemd never applies one unit's base text to another unit's drop-in; no code makes the terminator's base unit yet. `TestEveryUnitTextDeniesWhatItDoesNotAllow` judges every unit text the Go makes; X29 reads the filter as systemd applies it and goes red when the deny is dropped. It proves rendered text, not a live systemd or firewall.
- **S2, S3:** the default team url is https; plain http only to a loopback address; a url's port must be the team's port (`declare.URLProblem`).

Defence Before Fix, B1 and B2: the class is code built and tested but reachable from no production entry point. go-qa's dead-code stage runs `deadcode -test`, which counts test callers as live; without `-test` over `./cmd/...` it named the four functions, and 239 other functions remain unreachable (160 in teamchat). No gate was added: a blocking gate over that many instances is the owner's decision.

Builder choices the coordinator should confirm: an old version 1 backup restores and leaves the team's CA; after a re-apply has made a new CA, a restore is refused until the operator removes both CA files.
