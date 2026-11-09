## 15:50 · action · —   — S7b part two: the terminators unit and the loopback-only homeserver

Task 8.8, slice S7b, second part: the terminator's unit, and the homeserver bound to loopback alone (X4, X18).

What changed, test first, on the cross-network branch:

- The homeserver's config binds `127.0.0.1` alone and its drop-in allows loopback alone and waits for no device. Everything that is a function of the effective networks is now the drop-in of a second unit, `teamchat-tls@<team>`: its device units, `ExecStart=` with each listen address, a `LoadCredential=` each for the leaf and its key (never the CA's key), the IP filter and the socket bind. A golden render holds the drop-in; a second network changes the terminator's drop-in, the firewall record and units.json's listen set and nothing of the homeserver's. units.json's reload set loses its device-unit digest, which only the homeserver's `[Unit]` section fed.
- `team apply` has a step after the homeserver's address check: it writes the terminator's drop-in, reloads systemd, starts the unit or restarts it when its drop-in changed or the leaf was made again (a unit reads its credentials only at its start), enables it, and waits for its bound addresses on the port to be exactly the listen set. The readiness wait is now one function over any probe. `serve` starts the terminator after the homeserver and makes the same check from units.json; `team remove` stops and disables it first and deletes its drop-in with the homeserver's.
- Names: `paths.TLSUnit`, `TLSUnitDropIn`, `TLSUnitTemplate`, `TLSDropInFile`, `TLSUnitTemplateFile`; the unit adapter accepts the terminator's unit name; the team id `tls` is reserved, as `fence` is (the paths package's own test derives the reserved ids from the fixed names, and went red on it first).

Verification: the host, admin, units and paths tests; the project rules over teamchat. Two mutations were run and each turned the intended test red: dropping the terminator's bound-address check (`TestTheBoundAddressesOfBothUnitsAreAsserted`) and dropping the restart on a made-again leaf (`TestAnExpiringLeafRestartsTheTerminator`).

Four existing tests had to change for the new design, each deliberately: the reload-set tests used a narrowed network as their reload-set change, which is now the terminator's, so they use a raised memory ceiling; the networks tests read the terminator's drop-in instead of the homeserver's.

Open, and written in the spec: no play installs any base unit yet, so `teamchat-tls@.service` is part of the host play that installs the others (X18a gives its lines); the health probe does not judge the terminator yet, which matters because a stopped terminator cuts every member off while the homeserver reads as healthy. That is the next slice after this one.
