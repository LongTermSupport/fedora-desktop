## 10:00 · decision · —   — the owner's answers: Q1-Q5 decided; the orphaned volume removed by a play step

The owner answered the unblocking list of 2026-10-28 with "all recommended", typed directly in
the coordinator's session.

- Q1, Q3, Q4 (part C item 7, the coordinator's decisions, not overruled): a project image
  unused for 30 days is removed; health is flagged under 10% or 50 GiB free, over 50 dangling
  images, or the timer silent for 3 days; the first timer run clears the backlog without a
  separate dry run.
- Q2 (item A6): the 30 dangling volumes are kept and reported; the owner reviews the list when
  the timer prints it.
- The orphaned volume (item A6, now Q2b): removed through a one-off root play step. New
  unticked Task 4.4 in `play-podman.yml`: probe then remove, found by its property (data owned
  outside the user's uid and subuid range, mounted by no container), never by a hard-coded
  name, each removal logged; acceptance that `podman system df` exits 0 afterwards (added to
  Task 7.1 too).
- Q5 (item A4): host_health stays Python; two probes are not a rework, by the same reading the
  owner approved for Plan 00234's model manager.
