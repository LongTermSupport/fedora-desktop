# Plan 00211: the owner's steps after deploy 2

Deploy 2's acceptance ends by asking a test question in one team's questions room. The
question comes from that team's coordinator, in its own session, whether that is a ccy
session or an LXC member. Its run report names the team, the coordinator and the question
number (`Q<n>`). A rerun while an earlier run's test question is not closed (open or
answered) asks no new one: pingbus refuses a question that such a one already asks, so the
run reuses it. Its report says the question was already asked and is not closed, and names
its `Q<n>`, whose title carries the earlier run's tag. The steps below and A5 use that
question; if you have already answered and edited it, steps 4 and 5 are done. If no team has
a coordinator, the acceptance is REJECTED and names what to do. If the coordinator is
offline (its session not running, or a host member not driven from the acceptance), the run is ACCEPTED with the test question NOT JUDGED: no question is asked,
and its last line says to run `deploy-stage-2.bash` again once the coordinator is online.
These steps wait for that run. Then, in Element, signed in as yourself:

1. Accept the invites to `<team> · reports` and `<team> · questions`.
2. In the reports room, see that the composer is disabled: you cannot post there.
3. See the acceptance's test report there, and that it raised no notification.
4. See that the test question in the questions room raised a notification.
5. Answer it with "Reply in thread", with any text. Then edit that reply.
6. Confirm that no react button is offered on a message in the questions room.

Then run `./CLAUDE/Plan/meta-deploy.bash`, whose queue then holds
`acceptance-after-owner.bash`. It checks (A5) that your reply and its edit reached the
coordinator, attached to the test question, and that its session took both. The
coordinator's session must be running for that check: with it offline, A5 is NOT JUDGED and
the run says to run it again once the coordinator is online. Say which of steps 2 to 6 did not hold,
if any: A5 cannot see those, so they are journalled from what you report.
