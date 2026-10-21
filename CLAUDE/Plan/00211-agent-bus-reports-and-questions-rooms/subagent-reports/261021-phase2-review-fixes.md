# Plan 00211 Phase 2: the review's fixes (not pushed, not run)

Branch `agent-a052b8b727c42e7c6-500c3edc`, based on F44. The Phase 2 branch is merged in, and
each finding of `261021-review-00211-phase2-opus.md` is fixed.

| Commit   | What                                                                                                                      |
| -------- | ------------------------------------------------------------------------------------------------------------------------- |
| ee2645b2 | The Phase 2 branch merged in, versions above F44 (the changelog follows 3.99.13.8)                                        |
| 0d0be54e | S5, test first: two unrecorded side-room candidates, so the first created is adopted. CCY 3.99.13.10, container 2.78.10   |
| 6b6091a0 | B1, S1-S4: the acceptance drives the coordinator in its ccy or LXC session, plus the SPEC, PLAN, owner doc and queue text |

## For the coordinator

- **The version moved again.** admin.py is baked into the image, so the commit hook asked for a
  bump past 2.78.9. The release is now CCY 3.99.13.10, container 2.78.10, and the 3.99.13.9
  changelog entry (never released) was renumbered to match.
- **B1 design.**
  - The coordinator is the registry's (`agent-bus list`).
  - Its session is found by handle. For a ccy seat, `podman exec` reads
    `/tmp/pingbus-home/*/member.json` in each container labelled `ccy=true`. For an LXC member,
    the guest's bundle is read.
  - Commands then run with `PINGBUS_HOME=/tmp/pingbus-home` (ccy) or through the guest path
    (LXC).
  - The acceptance REJECTS before A3 changes anything when no team has a coordinator, or when
    the coordinator is in no session it can drive.
  - A host-type coordinator is not driven, and the REJECT says so.
- **S2 definitions.**
  - A "running member" is a seat held in a running ccy session. An LXC session cannot be seen
    from the host, so A4 covers LXC members.
  - A5's "received as envelopes" is checked as: consumed by the session and not in
    `dropped.log`. SPEC §11.2 now says exactly that.
- **S5 behaviour change.** Before, two candidates were refused. Now the first created is
  adopted. Neither candidate is indexed, so no member can have trusted either.
- **Not done.** Nothing ran on the host. No playbook changed, so there was nothing to
  syntax-check.
