## 23:07 · action · —   — The owner's look passes; a checking message and a readable GPU line

The owner checked Task 2.2 and it passes: "listing models at 4.4GB as too big, other things all look fine, it's showing the GPU detail". Two follow-ups, added as Tasks 2.4 and 2.5 (2.3 was already taken by the review fixes' host run): "it opens as a black screen for quite a long time - can we have a loading message?" and "the GPU detail ... could be clearer, it's quite low contrast".

The black screen: `on_mount` called `auto_suggestions` (wsi-resolve-model --suggest, once per recorder mode) and `machine_fit` (--fit) on Textual's own thread. Each resolver run imports CTranslate2 and asks CUDA for a device count, so the first frame waited for three of them. The probes now run in a thread worker (`probe_machine`, `_probe_machine`), the first frame's status line is `Checking the GPU and the models...`, and `_machine_probed` fills the table, the status line and the cursor. A download asked for before then is held with a warning.

The contrast: `#status` was `color: $text-muted` on `$surface`. It is now `$text`, with the GPU part in bold.

Tests first: `OpensAtOnceTest` (six tests) red, then green. The plan's `check-fit.py screen` leg now checks the first status line and waits for the worker. Task 2.6 is the host run (play-speech-to-text.yml through changed-plays, then acceptance.bash) and the owner's second look.
