# Agent team bus v2: members on other networks

How a v2 team's homeserver is reached from a second machine: which networks may connect, how
the connection is encrypted, how a member's credential gets to the machine it runs on, and what
a person has to do for Element. The behaviour is specified in
[SPEC-cross-network.md](../CLAUDE/Plan/00246-agent-bus-v2-landing-and-cross-network/SPEC-cross-network.md)
(expectations X1 to X30); this page says what to do and what to expect.

**Status.** The code is built and tested under fakes, and over loopback sockets for the TLS
terminator. Nothing here runs on a live machine yet: the host play that installs the base
units (`teamchat-tls@.service` among them) and the v2 launcher are Plan 00209 work that is not
built, so a command below is what the finished system does, not something to type today. The
v1 guide, [agent-bus.md](agent-bus.md), is still the guide for teams that exist.

## A team declares the networks that may connect

A v2 team declares `networks`: a list of up to 32 CIDRs in canonical masked form, for example
`192.0.2.0/24`. There is no `listen` or `allow_from` any more. Everything else follows from
the list:

- **Effective networks** are the declared ones, plus the host's own bus address as a `/32`,
  plus the LXC bridge's subnet while the team has an LXC member.
- **Listen addresses** are worked out at `team apply` and never declared: for each effective
  network, the address of the interface that routes it. A network whose route is the default
  route is refused, naming the CIDR, unless it is the host's own bus address or the bridge: a
  homeserver is not meant to be on the internet, so add a route (a VPN, a static one) instead.
- The unit's IP filter and the firewall rules are functions of the effective networks alone: the
  filter denies every address but loopback, each listen address and each effective network (the
  drop-in carries `IPAddressDeny=any` as well as the allow, since systemd grants whatever no rule
  denies), and the unit may bind only the team's port; the firewall has one
  rich rule per effective network, in the zone of the interface that routes it.
- `networks` is an approved field, like the others that decide who can reach the team: a change
  needs the owner's approval (`team approve`), and `--add` and `--remove` work on it. Removing a
  network and running `team apply` cuts that network's reach, in the filter and in the firewall.
- The team's `url` host must be one of the listen addresses when it is an IP literal, and may be
  a DNS name otherwise, and the port it reaches (written, or 443 for `https`) must be the team's
  `port`; `declare check` refuses anything else.

## Encryption: the team's own CA

Plain HTTP is for a loopback address only (`127.0.0.0/8` or `::1`, written as an IP). Every
other member URL is `https`, local ccy and LXC members included: a team without a `url` gets
`https://<teamchat_address>:<port>`, and `declare check` and `team apply` refuse an `http` url to
anything but loopback.

- A team has one CA, made at its first apply (ECDSA P-256, valid ten years). Its key is readable
  by root only, under the team's root state. The CA certificate is not secret; the key is. A key
  that is not root's with mode 0600 is refused by apply, issuing, backup and restore, each naming
  the fix (`chmod 600` or `chown root` on the file); nothing repairs it for you.
- Every backup of a team that has a CA carries the CA certificate and key (`ca.crt`, `ca.key`,
  version 2 of the backup's `backup.json`), so a restore keeps every issued bundle valid. The
  backup stays readable by root only; a copy taken off the host must be encrypted, because it
  holds the CA key.
- A restore puts the backup's CA back when the team has none, and then says to run `team apply`,
  which makes the leaf again from it. A team that already has that CA keeps it. A team with
  another CA is refused before anything changes: a restore never replaces a CA. To take the
  backup's CA instead, remove both CA files and restore again; every bundle the other CA issued
  must then be reissued. A backup taken before backups carried the CA restores as before and
  leaves the team's CA as it is; the restore's answer says so.
- The homeserver's leaf certificate is signed by that CA and names every listen address (and the
  `url` host, when it is a name). It is valid for a year and is made again at apply when the
  addresses change or fewer than 30 days remain; no backup carries it.
- A seat bundle carries the CA certificate (`tls_ca`) for an `https` URL. The member client trusts
  exactly that certificate: the system's roots are not consulted, TLS 1.3 is the minimum, and the
  name is checked against the URL's host. A certificate from another CA, from a system root, or for
  another address fails, with an error naming the pinned CA's fingerprint and the host.
- The homeserver binds loopback only and stays plain there. A small per-team unit,
  `teamchat-tls@<team>` (the command `teamchat tls`), accepts TLS on each listen address and
  relays the bytes to the homeserver on loopback. The leaf and its key reach it as systemd
  credentials, it has its own dynamic uid, and it never holds the CA key. `team apply` starts it
  once the homeserver is ready and checks that it listens on exactly the listen addresses.
- A stopped terminator cuts every member off while the homeserver still answers on loopback, so
  the health tick reads the terminator's unit and records a problem on the team's status when it
  is not active. The tick restarts nothing: the unit restarts always, so one that is not active
  was stopped or failed past its start limit.
- On an SELinux-enforcing host the team units run as `init_t` (their programs are under
  `/usr/local` and they set `NoNewPrivileges=yes`, so no domain transition happens), and `init_t`
  may not connect to an ordinary port. `play-teamchat.yml` therefore installs a small policy
  module, `teamchat` (`/usr/local/share/teamchat/teamchat.cil`), and labels each present team's
  port `teamchat_port_t`, the one port type `init_t` may bind and connect to; every other domain
  keeps the access it had. Check it with `semanage port -l | grep teamchat` and
  `ausearch -m avc -c teamchat`. A team's port that the policy already gives another type is
  re-labelled, so do not declare a port another service needs. Running only this part:
  `--tags selinux`.

## A member on another machine

The credential is issued on the homeserver host and sent to the machine that will use it. Nothing
ssh-es out of the homeserver host (its egress is DNS, NTP and web only): the ssh runs into it,
from the member machine or from the issuing person's own machine, with their own keys and agent,
and nothing runs as root over ssh. The sudo is a password sudo, its password given on the ssh's
stdin with `sudo -S`: there is no tty (never `ssh -t`, which would put the document through a
pty) and no NOPASSWD rule. `member add` reads no stdin, so the password line is all that goes in.

Pull from the member machine:

```
read -rs pw; printf '%s\n' "$pw" | ssh <bus> sudo -S -p '' teamchat-admin member add ... --credential token | teamchat seat place-issued --checkout <dir>; unset pw
```

Relay from the issuing person's machine:

```
read -rs pw; printf '%s\n' "$pw" | ssh <bus> sudo -S -p '' teamchat-admin member add ... --credential token | ssh <member> teamchat seat place-issued --checkout <dir>; unset pw
```

`<bus>` and `<member>` are entries in the issuing person's own ssh configuration; no hostname
belongs in a declaration, a file of this repository or a handle. A sudoers rule for the verb is
the homeserver host's own configuration to make or not; the play installs none. `seat place-issued` reads one issued
document on stdin, checks it, and places the seat in that machine's pool for the checkout: the
password or token only in the pool, mode 0600. Nothing secret is in its arguments or its
output. `--credential password` works as for any seat. If the ssh step fails, the seat was not
placed on that machine: reissue it by running the command again.

A seat is keyed on the homeserver's URL. The first bundle a checkout takes for a team records the
team's server name and its canonical URL (`https`, lower-case host, explicit port); a later bundle
with the same team id and another URL or server name is refused, naming both, so two homeserver
hosts cannot both stand for one team on one machine. Creating a seat for a URL the machine cannot
reach fails at once and names the URL; launching into a seat that exists always starts, and the
link catches up when the homeserver answers. A parked or retired seat is found by its login being
refused, and is reissued.

A script sends and reads with no session running by making each verb a session of its own:
`teamchat run --plain` starts the keeper and its link, runs the verb, ends, and exits with the
verb's status, its record on stdout.

```
printf '%s\n' "text" | teamchat run --plain --kind host --state <dir> --seat <team>/<seat> -- teamchat say --to <name> --wait-contact --team <team>
teamchat run --plain --kind host --state <dir> --seat <team>/<seat> -- teamchat history --team <team>
```

`--wait-contact` holds the say up to 10 s for the link's first contact with the homeserver, so
it is posted rather than refused or queued in a session that ends at once; a `"queued":true`
record still means the message waits for the seat's next session. A seat a running session holds
is refused at once, exit 75 `seat-held`. `<dir>` holds the seat as `<dir>/<team>/`: no command
takes a pool seat into a session yet (Plan 00209 Task 4.2).

A handle never carries a real hostname. The `site` part of a handle comes from the machine's
declared `teamchat_site` (default `local`) and from nothing else. A declared site that spells the
machine's hostname, a dot-label of it, or a DNS label of a team's URL host is refused; that check
is best effort, and the guarantee is that no code path reads a hostname into a handle.

## Element

An Element Desktop profile for an `https` team only works if Element trusts the team CA. The
Element play (`play-agent-bus-element.yml`) places it, with nothing done by hand: for each team it
names whose approved `url` is `https`, it reads the CA (`/var/lib/teamchat-admin/<team>/tls/ca.crt`)
as root and puts it where the Flatpak build of Element reads it. Where that is, is the play's
`agent_bus_element_ca_store`: `nssdb` (the default) is Chromium's own certificate database inside
Element's Flatpak data, so only Element trusts the CA; `system` is the host's trust anchors, which
every program on the host then trusts. The play's last step makes a TLS handshake from inside
Element's sandbox to the team's address and checks the certificate against the stores Element
reads; it fails the play, naming the CA file and the store, when the CA is not trusted. Until the
play has passed for a team, `teamchat-admin element check` and `apply` say on their error output
that Element does not trust the team's CA yet and to run the play.

Whether the Flatpak build honours each store has not yet been seen on a real desktop: that host
check decides which store the play uses. The play runs only on the homeserver host, where the
team's `approved.json` is; Element on another machine has no profile from this repository.

## What is proven, and what is not

On one machine with two addresses: a team that declares both networks serves TLS on each address,
a member connects through each with the bundle's CA, and the rules the render produces allow the
declared networks and nothing outside them. That is held by tests, not by a second machine: the
connection from an address outside the networks is refused by the filter and the firewall rules
as rendered, which the tests read (the filter as systemd applies it, deny half included), not by
a live systemd or firewall. The second-machine run (a member on
another host joins over TLS, its credential issued over ssh, and a connection from an undeclared
network is refused) is a check for the VM lab or the host once the play and the launcher exist.
Not built: a health probe that makes a TLS request through the terminator, which would also catch
a unit that is active and not answering.
