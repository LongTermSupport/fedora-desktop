# The owner's directions for v2

Moved out of `PLAN.md` so the plan stays within its size limit; the text is unchanged.
`PLAN.md` keeps a one-line summary of each and links here.

1. "the python version is NOT a golden archetype. its a prototype and first draft. this go version
   DOES NOT have to stick to python approaches etc. FIX THINGS IMPROVE THINGS MAKE IT EXCELLENT"
2. "i dont even care about staying compatible - now is the time for breaking changes and really
   making this system PROPERLY DECENT"
3. "already running teams like yours msut keep workiing", "teams v2 can be a different thing"
4. "It's time to properly lock into Go - ditch the python and bash legacy and push hard into Go. We
   need fast tools, fast tests and CI, clean and robust" (2026-10-30). Finishing v2 and moving the
   teams onto it comes before further v1 hardening; a v1 defect is fixed in v1 only when it stops
   a team now, and is otherwise carried as a v2 requirement. `bus/pycompat` and its python3
   oracle tests exist only to read v1's files, and go with v1.
5. "go for breaking changes on the ccy teams stuff - I am the single user, as long as your plays
   handle updating the desktop teams then we're good. Push hard, break stuff - move forwards"
   (2026-10-30). This answers OD-CUTOVER: every team moves by one step, which the plays run, and
   v1 is then retired. Direction 3's "already running teams must keep working" now means the
   plays carry each team across, not that v1 and v2 run side by side.
6. Where v2 is going (2026-10-30, answering gate A): "what we've built so far, which only works on
   local host, is not the intended end result": one homeserver per team, which "might be hosted
   locally ... on another server ... somewhere in the cloud", and agents "spread across multiple
   machines, possibly in multiple different networks", reaching it over "some kind of vpn or mesh
   network"; "not the intention that the home server would be on the internet but some kind of
   private networking is implied". The irreversible choices stand "as long as all of these points
   are not locking us into things that are going to prevent that going forwards" (Task 0.6).
7. **v1 is feature-frozen; cross-network membership is v2 only** (2026-11-02): "It goes in v2 and
   lets get v2 done - carrying two versions is nuts"; "if we have v2 imminent then v1 should
   feature freeze apart from bug fix". No v1 feature work starts, the remote-seat items
   included; a v1 change is a bug fix or nothing. On networks: "pingbus should not invent its
   own wireguard overlay ... it should simply support working across multiple routable ip
   ranges - how they are routed is not its concern", and "the team should have explicit configs
   for networks it allows team members to be on expressed as normal CIDR". So v2 has no overlay
   (Plan 00220's overlay is dropped from the bus): a team declares its member networks as a
   list of CIDRs, the one source for its listen addresses, `allow_from`, the unit's IP filter,
   the firewall rules and the seat URL check; a seat is keyed on the homeserver URL; transport
   is TLS with the team's own CA pinned in the bundle (plain HTTP only on loopback); a remote
   seat's token is issued over ssh; and a seat handle never carries a real hostname. These
   replace Task 0.6's "plain HTTP only inside a mesh tunnel".

Clarified with direction 3: **v2 is a separate system.** None of its wire, files, locks, CLI or
process model has to stay compatible with v1, so there is no compatibility contract and no parity
oracle against the Python. The owner's standing instruction "DO NOT GATE ON ME" stands: decisions
are defaulted and may be overturned ([DECISIONS.md](DECISIONS.md)).
