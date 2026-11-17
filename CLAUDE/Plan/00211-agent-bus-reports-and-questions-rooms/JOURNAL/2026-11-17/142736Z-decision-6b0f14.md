## 14:27 · decision · — — Task 2.15: question types and closed-question titles

Owner decision (relayed by the coordinator): questions carry a type, shown as an icon after Q<n>, and a closed question's root says so in its title. Built as Task 2.15, test-first.

Design points:

- The type lives in the body only. Pingbus's own parser accepts the question key only with exactly its four fields, so a new field would make every deployed older reader drop the question. With the icon in the body, an older reader still parses it; its headline just includes the icon.
- The retitle keeps the sections and the question key in m.new_content. A reader that applies the edit (or a server that returns edited content) still parses the question.
- Matrix lets only a message's sender edit it. A root a previous coordinator seat posted cannot be retitled; pingbus says so and the closing post in its thread remains the record.
- The backfill is idempotent: the question index records each root's retitle from the room's edits.
- The send gate admits a backfill edit only when it will really be sent. An earlier draft admitted skipped roots too, and the second run then hit the duplicate window; a test caught it.
