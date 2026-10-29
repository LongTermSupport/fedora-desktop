## 22:59 · decision · —   — Q6 answered: no reset; the host agent triages and decides each old-range item

The owner answered Q6, verbatim: "reset would nuke ALL podman? that is definitely not
acceptable, we have lots of running ccy sessions eg this one. re own I guess? but maybe we can
be a bit more clever than one decision for all - can the desktop level session properly triage
and decide case by case? crap can be removed".

- Option B (`podman system reset`) is rejected, for good.
- Task 4.7 becomes 4.7a-4.7d and Decision 4: a root scan that only reads, a user-side triage
  in Go that groups each old-range path by what owns it (layer, image and tags, containers and
  their state, volume and its mounts) and proposes REMOVE, RE-OWN, ASK or HELD per item, and a
  decisions file the host agent writes under `untracked/`, applied by play-podman.yml.
- A layer a running container's overlay holds is HELD, not re-owned: a change under a mounted
  overlay is undefined, and the brief said never to touch a running container's files. If the
  shared base layers of the running ccy sessions are old-range, the store cannot be made clean
  until they stop; the triage will show whether that case exists.
- Task 4.4 (remove the orphaned volume by rule) is superseded: each volume is an item of the
  triage.
