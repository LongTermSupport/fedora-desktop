## 19:18 · decision · —

Phases A, B and Task C.4 are cleared to build. The decision is the coordinator's, in a teamchat reply of 2026-11-26 19:01Z. All three are engineering calls. The plan's earlier hold ("unbuilt until the owner has seen them") was this plan's own wording, not the owner's. The owner's stated direction points the same way: Q32 "yes new containers are secure", and his priority to push the fedora-desktop work forward. He is away, unattended, until Tuesday. The coordinator has put a one-line note in the owner's tracker so he can reverse it then.

The bar each must clear:

- Phase A: a container must not be able to widen its own mounts. It must not take a capability away either. A deliberate, legitimate change to `.claude/ccy/mounts` needs a host-side approve verb that re-records the approved hash, and a refusal of an unapproved change names the file and prints that verb. An independent, security-minded review sub-agent reviews it before it ships, and the review is named.
- Phase B: the replayed-field validation is hardening of the same kind; build it.
- Task C.4: built after C.2 ships, as planned. Its token-sharing rule stays the other team's owner's call, and the design works either way.
