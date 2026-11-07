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
  a DNS name otherwise; `declare check` refuses anything else.

## Encryption: the team's own CA

Plain HTTP is for a loopback address only (`127.0.0.0/8` or `::1`, written as an IP). Every
other member URL is `https`, local ccy and LXC members included: a team without a `url` gets
`https://<teamchat_address>:<port>`, and `declare check` and `team apply` refuse an `http` url to
anything but loopback.

- A team has one CA, made at its first apply (ECDSA P-256, valid ten years). Its key is readable
  by root only, under the team's root state, and the team's backup carries it, so a restore keeps
  every issued bundle valid. The CA certificate is not secret; the key is.
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

## A member on another machine

The credential is issued on the homeserver host and sent to the machine that will use it, over
ssh, run as the person issuing it with their own keys and agent. The member machine holds no
credential for the homeserver host, and nothing runs as root over ssh:

```
sudo teamchat-admin member add ... --credential token | ssh <alias> teamchat seat place-issued --checkout <dir>
```

`<alias>` is an entry in the issuing person's own ssh configuration; no hostname belongs in a
declaration, a file of this repository or a handle. `seat place-issued` reads one issued
document on stdin, checks it, and places the seat in that machine's pool for the checkout: the
password or token only in the pool, mode 0600. Nothing secret is in its arguments or its
output. `--credential password` works as for any seat.

A seat is keyed on the homeserver's URL. The first bundle a checkout takes for a team records the
team's server name and its canonical URL (`https`, lower-case host, explicit port); a later bundle
with the same team id and another URL or server name is refused, naming both, so two homeserver
hosts cannot both stand for one team on one machine. Creating a seat for a URL the machine cannot
reach fails at once and names the URL; launching into a seat that exists always starts, and the
link catches up when the homeserver answers. A parked or retired seat is found by its login being
refused, and is reissued.

A handle never carries a real hostname. The `site` part of a handle comes from the machine's
declared `teamchat_site` (default `local`) and from nothing else. A declared site that spells the
machine's hostname, a dot-label of it, or a DNS label of a team's URL host is refused; that check
is best effort, and the guarantee is that no code path reads a hostname into a handle.

## Element

An Element Desktop profile for an `https` team only works if Element trusts the team CA. Whether
the Flatpak build honours a CA added for it has not been checked on a real machine, so
`teamchat-admin element apply` says on its error output, for each `https` team, that the CA must be
trusted and where it is: the CA certificate is in the team's root-only state directory
(`/var/lib/teamchat-admin/<team>/tls/ca.crt`), which only root reads, so copy it out as root to
import it. The same file is in every member bundle.

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
