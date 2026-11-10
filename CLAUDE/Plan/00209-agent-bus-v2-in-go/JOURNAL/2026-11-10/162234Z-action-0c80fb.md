## 16:22 · action · —   — Task 4.1: the v2 kit in the ccy image

Task 4.1 done in 9e13ce887 (CCY 3.114.0, container 2.81.0): the image carries the v2 kit at
/opt/claude-yolo/optional/teamchat. The binary is built static from src-go as committed at HEAD
(git archive of the HEAD:src-go tree, whose id is the source digest), so an uncommitted change in
the checkout neither enters the image nor stops the play. The rest of the kit lives under
files/opt/claude-yolo/optional/teamchat/, held equal to kit.Assemble's output for the image path
by kit/image_test.go (red before the files existed, green after). The image build runs
`teamchat version`. No session uses the kit until Task 4.2.

Not built here: the stage B detector of INSTALL-PATH.md (the image's digest against
installed.json's) needs installed.json, which no program writes yet. The container-version gate
does not judge src-go, so a teamchat source change reaches the image on the next ccy play run
without a container bump (the same holds for pingbus-status-line); the digest records which
source it is.

Task 4.2 is not started: see subagent-reports/261110-stage-bc-report.md for what it needs and
the bash ratchet that bounds where its wiring can go.
