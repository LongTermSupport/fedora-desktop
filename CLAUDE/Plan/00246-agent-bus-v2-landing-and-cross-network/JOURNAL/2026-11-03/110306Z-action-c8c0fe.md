## 11:03 · action · —   — renumbered from 00244

F44 took the number 00244 for another plan (the Element plan) while this plan was on its
branch, and the plan counter is local to each clone, so the two collided. This plan is now
00245: its folder was moved with `git mv`, and its references (the plan index row, the
`Plan 00244` mentions in Plan 00209 and in the project record, the commit-message
convention from here on) now say 00245. Entries written before the move still say 00244; they
are history and are not edited. The clone's counter is raised to 245 so the next plan does not
collide again. The owner asked for this on pull request 124.
