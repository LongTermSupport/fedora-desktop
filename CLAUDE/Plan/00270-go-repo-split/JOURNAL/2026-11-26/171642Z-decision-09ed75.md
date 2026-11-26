## 17:16 · decision · —

Owner answered the team's question 32 (Q2, Q4, Q7 of this plan), relayed by the coordinator:
"1 defintely no force pushign history to fedora-desktop, I'm not even hugely attached to the
history - some initial commit is fine. Make sure full hooks daemon setup is done for each repo
with full planning etc and theh the first plan can have a handover doc that descreibe the
history and confirms how to get history from fedora-desktop if required. fedora-desktop can
then have a deletion commit once the carve-out is completed. 2 The new repo is defintely only
for v2 the new repo is absolutely clean 3 not sure what this one is about? this is <name>
level stuff? yes new ocntainers are secure" (one install-specific name replaced by `<name>`
for this public repository)

Applied:

- Q2: no history split (no filter-repo, no subtree, no human-run step). Each new repository
  starts from one clean initial commit naming the source sha, gets the full hooks-daemon and
  planning setup and CI from its first commits, and its first plan carries HANDOVER.md with the
  commands to read the history in fedora-desktop. fedora-desktop gets a plain deletion commit
  once each carve-out is proven (Tasks 2.2 to 2.6). Never a force-push or history rewrite.
- Q4: the new repositories carry v2 only. The v1 packages are the new group stays-v1 in
  INVENTORY.md and stay here; Task 1.5 cuts ccy's edges to them. Removing v1 from
  fedora-desktop is not approved: Phase 7 is gated (prove unused, tell the coordinator, wait).
- Q7: approved as proposed, host nftables on the veth, written by this repository's LXC play
  (Task 5.3).
