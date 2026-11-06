## 23:55 · action · —   — n3f-b3: the tried report's recovery, the reports E2E rows, project_room_default

n3f-b3 (feat/bus-v2-n3f-b3): the recovery of a tried report is built. Test first, it was red because the report was posted a second time and the reports room held three own events. The recovery now reads the reports room with no anchor (at most 500 events, the residual window). A 403 on that read finds nothing; any other error, a missing roster, or a room no recovery reads fails the recovery.

The four wiring end-to-end rows are built on fakehs: OF1-n3f's report row, n3f-D1's three stdin cases, RP5 with two members, and a refused join shown in status. Each was run red on a planted defect, except RP5's item half and OF1's stopped-worker half, which the report says plainly.

New project rule project_room_default: a send's room tested against one side room. It found postRoom, recheck and sendFailed, each now a switch on the room. A fake that keeps fewer rooms is listed under "cannot be a rule", and the client's hsRecorder is fixed. Report: subagent-reports/261105-n3f-b3-opus-report.md.
