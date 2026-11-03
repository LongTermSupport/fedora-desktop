The lead saved this read-only narrow check reply. Verdict READY; the optional 12.6 fix (Old covers
n3d's 3 MiB form, New stands alone) and the stray line at 454 were made before the build.

**n3f spec narrow check of `SPEC-n3f.md` at 3cf1c434b: READY to build.** One wording fix in section 12.6 is worth making, but it is not a blocker.

**1. Round-3 items**
- **B1 FIXED** (QX5 "A build owed or under way", lines 427-451; QX1 lines 294-313; QX7; n3f-D21). The `build` record is `{start, after, late_from}`. A redaction's write records the build owed, and a load that finds the record resumes the build, never a catch-up. Tests (h) and (i) cover a kill after the redaction's write and a kill mid-build.
- **B2 FIXED** (RM1 lines 683-696, n3f-D11). `remind_late` takes only questions that are `open` with `remind_after_s`. A number leaves when it is reminded, leaves `open`, is evicted, or is not open at a build's last page. The 300-question test is there.
- **B3 FIXED** (QX7 lines 542-558, n3f-D20, 12.2, 12.3). `Next()` is 1 plus the highest counted plus the slots since it was last raised. The counting window is unchanged. Q1 then a redacted Q2 gives Q3. No "number given again" wording is left.
- **F1 FIXED** (QX6 lines 506-511). The scalars' values are 108 bytes, sized 256 with keys. The `asked_by` grammar caveat is stated.
- **F2 FIXED** (QX1 lines 318-320, QS4). The filter waits for the head only. Unechoed blocks only composes, verbs and reminders.
- **F3 FIXED** (QX1 ready line, lines 321-327). `outbox.questions_handover` is set, with a hook line naming `teamchat questions --team <t>`.
- **F4 FIXED.** I confirmed the QX7 text and the QX6 test list. I did not re-read section 11 for the `-tags tuwunel` E1 row; the fix log claims it.
- **F5 FIXED** (QX3 line 370). `questions_uncounted` is `{count, said}`.

**2. Store-size sum.** All figures recompute:
- A question is 4729 bytes, sized 5120.
- The `questions` key is 983 232 + 99 328 + 16 448 + 2048 = 1 101 056.
- The seat keys add 856 + 1296 + 256 = 2408, so n3f adds 1 103 464.
- Adding n3d's 3 001 136 gives 4 104 600.
- That is **under 4 194 304, by 89 704 bytes.**

**3. Section 12 old wording.** I matched each "Old" against the real design files after joining wrapped lines.
- **Found word for word:** A 13.3 role line, A 6.5 numbering sentence, A 10.2 `n` bullet, B `ask` row, B row 10 codes, B row 20, B 2.3.2 "about 1.9 MiB … 2 MiB", B 2.14 "2 MiB at worst", B 2.6 step 10 `human_text` sentence, and SPEC-n3 SR2's clause.
- **Not found:** nothing.
- **Gap in 12.6:** the "Old" caps sentence is the 2 MiB text in this worktree. Once n3d's amendment (3 MiB) is applied to B, that Old will not match. It should say "Old: the 2 MiB sentence, or n3d's 3 MiB form of it".
- **Gap in 12.6, second part:** the new sentence opens "The questions room adds its own …" and then says "their sum". It does not restate the base lists, so it only reads whole after n3d's sentence precedes it.
- **What B must finally read:** it does say 4 MiB, "the next 256 KiB" rounding, `MaxStore` and `TestStoreFullFitsTheFileCap`. 2.14 reads "4 MiB at worst".

**4. Epoch change.** Stated at QX1 lines 309-311: the key, `reminded_ms` and `remind_late` are dropped as at a new term. The reason is that a restore already redelivers items marked `restored`, and a reminder made again after it is of that class. QX5 line 414, n3f-D9 and T11 agree.

**5. Contradiction search** over `Next`, `remind_late`, `questions_unechoed`, `questions_handover`, `build`:
- No contradictions found.
- `late_from` is the replaced index's `Next()`, which now includes slots, and RM1 line 684 uses it consistently.
- The seven keys agree across G2, QX6 and 12.6.
- QS4 and QX1 agree on "head" versus "ready".
- n3f-D14 and 12.4 agree on `--re` being a grammar check only.
- Cosmetic only: a stray "A" on its own line at line 454.

**Verdict: READY to build.** Optionally first fix 12.6 so its "Old" also covers n3d's 3 MiB sentence and its "New" is complete on its own.