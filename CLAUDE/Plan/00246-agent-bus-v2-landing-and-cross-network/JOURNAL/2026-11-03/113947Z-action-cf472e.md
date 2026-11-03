## 11:39 · action · —   — renumbered again: 00245 to 00246

F44 took the number 00245 as well (for the plan on valuable work kept in untracked places)
while this plan was on its branch, one number after the collision that moved it from 00244.
The plan counter is local to each clone, so the two clones allocated the same number again.
This plan is now 00246: its folder was moved with `git mv`, and its references (the plan
index row, the `Plan 00245` mentions in Plan 00209 and in the project record, the plan's own
heading) say 00246. Entries and agent reports written before the move still say 00244 or
00245; they are history and are not edited. The clone's counter is raised to 246. The owner's
rule from pull request 124 applies: when F44 is merged in, a collision is resolved by moving
this plan to the next free number and updating every reference.
