# Plan 00269 research: how a seat is added today, and where it breaks

Read on branch `v2-smoke`. Placeholders only: team A, the serving host, the laptop.

## 1. The owner's complaint

Owner, 26 Nov 11:01Z (names replaced): "so i do i realy have to run an <other repo> script to add a
seat to a team? this is super clunky. \<team A's project> should not be so coupled to <other repo>
like this, something smells".

Today, a laptop seat of team A, which is served on another machine, takes two shell scripts that
live in another repository:

1. one issues the seat on the serving host (run by an agent seat that has sudo there);
2. one places it on the laptop (run by the owner from that other repository's checkout).

Both scripts' headers say their intended home is this repository's seat verbs (#415, #416). The
coupling the owner smells is real: a team's checkout needs a second repository and an agent with
root on the serving host to gain a seat.

## 2. What this repository already has

### `ccy-teams seats add` (`src-go/internal/teams/seatadd`)

- One run issues and places a seat for one checkout. The handle is derived, never typed: repo from
  the checkout's origin, site from the machine's `teamchat_site`, type `podman` or `host`
  (`seatadd.go`, package doc and `prefix`).
- The number is the lowest no pool seat of the checkout uses and the roster keeps for no handle of
  the same prefix (`freeHandle`). It needs the team's **status record**, which exists only on the
  serving host.
- `SudoIssuer` runs `teamchat-admin member add ... --credential password` through password sudo on
  **this** machine. The answer is held in memory, handed to `placeissued.Place` and cleared
  (`seatadd.go:94-109`, `:165-170`). This is the security shape the new verb must keep.
- A team with no status record here (served on another machine) is refused 78, and the error prints
  a cross-machine pipe to type by hand (`seatadd.go:147-149`). A seat issued but not placed prints a
  `member reissue ... --handle <handle>` pipe (`seatadd.go:175`). Both hand the owner a seat handle
  to type: R90 violations.
- `Adder.NewSeat` is the launch's in-process add (register R5): `cc` and `ccy --teams <team>` call
  it when no free seat of their kind exists; a remote team is the same 78 (`seatadd.go:191-217`).
  This is register R91's dead end.

### `teamchat seat place-issued` (`src-go/internal/teamchat/member/placeissued`)

- Reads one issued document on stdin (strict JSON, capped), places it in this machine's pool for
  one checkout under the allocation lock, replaces an existing record (old one quarantined, its
  credential unlinked). Opens no connection; "the document arrives on stdin, by whatever pipe the
  owner builds" (`doc.go`). `Place` is already callable in process (`seatadd` does).

### `teamchat-admin member add | return | reissue` (`src-go/internal/teamchat/admin`, `apply`)

- All are `ClassRoot` verbs (`admin/run.go:158`).
- `member add` of a handle already enrolled is refused with text only:
  "member H is enrolled: the seat number is taken, perhaps just now by another checkout of the same
  repository; choose again (member reissue only for a bundle this checkout lost)"
  (`apply/member.go:319`, `refusedAdd`). The caller cannot tell a race from a lost credential
  except by reading the words.
- `member reissue` takes an enrolled handle and issues a new credential (`apply/member.go:58,68`).
  The LXC apply already chooses add, return or reissue by the roster state of a handle
  (`host/lxc/apply.go`, `joinOf`): the same decision table this plan needs for a pool seat, except
  that a pool seat's reissue must have the owner's word, since an LXC record proves the placement
  and a pool does not.
- There is no read-only verb that lists a team's members with their state and enrolment time for a
  caller on another machine; `freeHandle` reads the local status record directly.

### The documented cross-machine route (`docs/agent-bus-cross-network.md`, "A member on another machine")

- The serving host has no ssh egress (DNS, NTP and web only): ssh runs **into** it, from the member
  machine ("pull") or from the person's own machine ("relay"), with that person's own keys.
- The sudo there is a password sudo, its password on the ssh's stdin with `sudo -S -p ''`; never
  `ssh -t` (the document would pass through a pty); no NOPASSWD rule; the play installs no sudoers
  rule for the verb.
- `<bus>` is an entry in the person's own ssh configuration; no hostname belongs in a declaration
  or a file of this repository.
- This is exactly what the other repository's scripts wrap, by hand.

### Where a remote team is recorded on the laptop

- `system.Life` (`teams/system`): "No record and no entry: the team is served on another machine
  ... nothing here records another machine's teams." So for a team with no seat on this machine
  yet, nothing here says which machine serves it. Once a seat exists, its pool record holds the
  team's canonical homeserver URL (cross-network doc, "A seat is keyed on the homeserver's URL"),
  which names the server but not an ssh route to it.
- `teamchat_teams` entries (`teamchat/declare` `Team`) have `state` present or absent only; a
  present entry means "install here". A team served elsewhere has no way to be declared today,
  hence PLAN D2's `served_by`.

## 3. Related plans and register rows

- Plan 00265 register R59 (removed teams refused; `system.Life`), R75 (removed teams offered; the
  open `ccy-teams seats forget <team>`), R89 (one restore command), R90 (no seat numbers for a
  human), R91 (`cc` dead end on a remote team). PLAN Task 4.2 builds R90's detector and the
  checkout-picks-its-seat launch. This plan does not repeat them; it supplies the add path they call.
- Plan 00197 (v1 seat broker daemon) designed a device-flow grant over the bus; Plan 00264
  (runtime teams) proposes a policy-bounded request spool. Both are the "over the bus" family.
- Plan 00257 (cross-host LXC members) covers LXC guests joining a remote team; a pool seat (podman
  or host `cc`) is this plan's.
- Issues #415 (seat move) and #416 (tracking: seat and team verbs; this verb is a new row there).

## 4. The live reissue case

A laptop podman seat of another team is enrolled on the bus, but no pool on the laptop holds its
credential. `member add` for a new number works; `member add` for that handle is refused "seat
number is taken". The seat is recoverable only by `member reissue` of that handle, piped into
`place-issued`, which today means typing the handle. Nothing tells the owner that this enrolled
seat is "the one this checkout lost" rather than one another checkout holds.

## 5. Options for reaching the serving host

| Route                                                         | Credential and sudo                                               | Against                                                                                                                                                 |
| ------------------------------------------------------------- | ----------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------- |
| ssh pull from the laptop (chosen)                             | the owner's own ssh key; a narrow NOPASSWD rule there (section 6) | needs the serving host's IaC to declare the rule; needs a declared route                                                                                |
| over the bus, a host agent issues                             | an agent seat with root on the serving host                       | the coupling the owner objects to; an agent holding sudo; the laptop needs a seat to ask with; the credential crosses the homeserver                    |
| over the bus, a broker with owner approval (Plan 00197 shape) | a daemon on the serving host; the owner approves in a room        | a new daemon and protocol; first seat still has no voice on the bus; out of scope here, can replace the issuer later behind the same `Issuer` interface |

The ssh route is what the docs already prescribe and what the other repository's scripts do by
hand; the verb makes it one step and keeps the secret rules. Decided by the team coordinator, 26 Nov
11:07Z (with Q1 and Q2), reversible on the owner's word: ssh is kept, and the password sudo the
docs describe is replaced by a narrow NOPASSWD rule (section 6). The route is declared on the
team's `teamchat_teams` entry (`served_by`, PLAN D2), not asked and kept in a seat store as first
proposed.

## 6. Why the sudoers rule allows one fixed command

The real argument shapes (`admin/verbs.go` `memberFlags`, `parseFlags`; `seatadd.go:100`):

- `member add --team <team> --prefix <prefix> --seat <n> --kind <type> --human-text <bool> --credential password|token`
- `member reissue --team <team> --handle <handle> --credential password|token`

Flags come in any order, each at most once. The team, prefix, handle and (after D3) the avoid list
are free text, so a sudoers rule over these verbs needs `*` for them, and sudoers' `*` matches any
characters including spaces. `teamchat-admin member add --team * --credential password` therefore
also matches `member add --team <t> --kind lxc --credential token --prefix ... --credential password`.
The first two words still fix the verb, but which credential and kind are issued would be held only
by `teamchat-admin` refusing a duplicated flag, not by the rule. No `*` pattern is narrow here.

So the rule names one fixed command line with no arguments (`member serve ""`), and the request
moves to stdin, where `member serve` checks it strictly: add, reissue or list; type podman or host;
credential password. The "tiny fixed wrapper" is a verb of `teamchat-admin`, not a separate script,
so it is typed, tested and installed with the verbs it calls (`/usr/local/sbin/teamchat-admin`,
`paths.AdminBinaryPath`, linked by `play-teamchat.yml`'s `tc_admin_link`). `sudo -n` needs no tty
and never prompts, so a missing rule fails at once instead of hanging on a password.
