## 21:08 · finding · —   — Bulk branch deletes go through branch-delete-list (#150)

Task 2.3 (#150) is merged at 362298621. The F42/F43 incident's rule, "an exempt branch is never a cleanup candidate", is now a command as well as prose. `scripts/branch-delete-list.bash` turns a list of names into delete commands only when every name passes, and otherwise refuses the whole list, naming each branch it refused and why:

- exempt or parked in the branch policy;
- an `F[0-9]*` release line, or origin's default branch;
- not on origin, a duplicate, or not a plain name;
- not archived at exactly its tip under `refs/archive/`.

Run against the real origin, the incident list is refused. `CLAUDE/QA.md` names the command as the way to build a bulk delete list.

What it cannot stop: an owner `!` command that skips it, and the forge's admin bypass on the "Protect F-version branches" ruleset. Both remain owner decisions.
