# Research: the Linux dictation landscape for long-form writing

Question from the owner: what is the state of the art for speech-to-text and transcription
for writing documents (a book) on Linux, and has someone already built a good open-source,
Whisper-based Linux dictation app, with voice commands for paragraphs, punctuation and
editing, so that this repo is not reinventing the wheel?

Researched 2026-10-04. Star counts, last-push dates and latest releases were read from the
GitHub API on that day. Feature claims come from each project's README or source. Anything
I could not check is marked **(unverified)**.

The system this is compared against: a GNOME Shell 50 extension plus Python scripts
(`wsi-*`), faster-whisper (large-v3-turbo / distil-large-v3.5) with Silero VAD in a warm
server, continuous dictation, text pasted into the focused window with wl-copy + ydotool
(Ctrl+V / Ctrl+Shift+V), and optional Claude post-processing. Fedora 44, GNOME 50 Wayland,
a GPU with limited VRAM.

Model accuracy is covered in more depth in [RESEARCH-stt-models-2026.md](RESEARCH-stt-models-2026.md).
Section 4 below only summarises what bears on choosing an app.

---

## 1. Short answer

- No open-source Linux app does everything here. The three most relevant are
  **Vocalinux** (IBus and RemoteDesktop-portal injection, plus a real voice-command set),
  **Handy** (by far the most popular, but push-to-talk only, and wtype/dotool injection that
  is weak on GNOME) and **Speech Note** (a notepad that suits long-form writing, but it
  injects through ydotool like this repo and has no editing commands).
- Nothing found matches this repo's combination: warm GPU faster-whisper, unlimited
  continuous dictation into the focused window on **GNOME Wayland**, and LLM clean-up. Most
  of the active Whisper tools are built for wlroots compositors (Hyprland, Sway) and treat
  GNOME as a fallback case.
- **Injection:** IBus `commit_text()` is the cleanest way to put text into the focused field
  on GNOME. It needs no clipboard, no paste shortcut and no `/dev/uinput`, and it does not
  care about keyboard layout. One active app uses it: Vocalinux (2026). The older
  IBus-Speech-To-Text (Vosk) has had no commits since 2022. The RemoteDesktop portal with
  libei is the approved way to send keys, which matters for editing commands.
- **Recommendation: keep building, and borrow two things.** Add an IBus engine as an
  injection backend, and add a small voice-command layer in the style of Vocalinux's
  phrase table. For the book itself, consider a "document mode" that writes to a file or
  buffer, with LLM tidying per paragraph, rather than typing into a word processor. Details
  in section 6.

---

## 2. Tools compared

### 2.1 Summary table

| Tool                                                                     | Engine(s)                                                                                                       | GPU                                                                            | GNOME Wayland injection                                                                                                      | Voice commands                                                                                                                                                      | Long-form / continuous                                                             | Status (2026-10-04)                                                                                                                                                                                                                                      | Licence               |
| ------------------------------------------------------------------------ | --------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | --------------------- |
| [Vocalinux](https://github.com/VocaHQ/vocalinux)                         | whisper.cpp (default), Faster Whisper, OpenAI Whisper, Vosk, Parakeet, remote HTTP                              | Vulkan via whisper.cpp; CUDA via faster-whisper extra (unverified on this GPU) | **IBus engine (`commit_text`)**, **RemoteDesktop portal (`NotifyKeyboardKeysym`)**, wtype, ydotool, clipboard                | Yes: punctuation, new line/paragraph, delete/scratch that, undo/redo, select word/line/paragraph/all, cut/copy/paste, capitalise/uppercase/lowercase/no-spaces-next | Toggle or push-to-talk; live insertion is claimed                                  | v0.17.0 2026-09-16, nightlies daily; 904 stars                                                                                                                                                                                                           | AGPL-3.0              |
| [Handy](https://github.com/cjpais/Handy)                                 | whisper.cpp (Small to Large/Turbo), Parakeet V3, Parakeet Unified EN GGUF, Moonshine streaming GGUF             | Yes for Whisper                                                                | wtype (which fails on GNOME), dotool, ydotool, enigo, clipboard paste                                                        | None found                                                                                                                                                          | Push-to-talk / toggle; whole utterance                                             | v0.9.8 2026-10-03; 32.8k stars                                                                                                                                                                                                                           | MIT (brand excluded)  |
| [Speech Note (dsnote)](https://github.com/mkiol/dsnote)                  | whisper.cpp, Faster Whisper, Vosk, Coqui STT, april-asr; "whisper.cpp + Parakeet" listed                        | CUDA/ROCm Flatpak add-ons; Vulkan                                              | ydotool (daemon required)                                                                                                    | None found                                                                                                                                                          | Notepad with long sessions, file transcription, inline timestamps                  | v4.9.0 2026-06-28 (Sailfish; release note says the Linux desktop build comes "at a later date"); add-on binaries 2026-09-05; 1.7k stars                                                                                                                  | MPL-2.0               |
| [Voxtype](https://github.com/peteonrails/voxtype)                        | Whisper, Parakeet, Moonshine, SenseVoice, Paraformer, Dolphin, Omnilingual, Cohere Transcribe, OpenVINO Whisper | Vulkan, CUDA/ROCm (source builds), OpenVINO NPU                                | wtype (fails on GNOME, so it falls back), dotool, ydotool, **eitype/libei**, clipboard                                       | Spoken punctuation and replacement rules                                                                                                                            | Push-to-talk; "meeting mode" with chunked continuous transcription; streaming path | v1.1.0 2026-09-24; 1.6k stars                                                                                                                                                                                                                            | MIT                   |
| [hyprwhspr](https://github.com/goodroot/hyprwhspr)                       | pywhispercpp, faster-whisper, Parakeet TDT v3, Cohere Transcribe, Qwen3-ASR, ONNX-ASR, REST/WebSocket           | CUDA, Vulkan                                                                   | wl-clipboard + wtype; GNOME/Mutter "with additional configuration"                                                           | Word overrides                                                                                                                                                      | Toggle, PTT, auto, continuous, long-form, live streaming                           | v1.47.0 2026-10-01; 1.2k stars                                                                                                                                                                                                                           | MIT                   |
| [OpenWhispr](https://github.com/OpenWhispr/openwhispr)                   | whisper.cpp, Parakeet (sherpa-onnx), cloud APIs                                                                 | Metal, CUDA, Vulkan                                                            | Not documented in the README (unverified)                                                                                    | "Voice assistant" hotkey for LLM commands                                                                                                                           | Notes app with AI actions, file import                                             | v1.10.2 2026-09-15; 9.0k stars                                                                                                                                                                                                                           | MIT (Electron)        |
| [nerd-dictation](https://github.com/ideasman42/nerd-dictation)           | Vosk only                                                                                                       | No                                                                             | xdotool, ydotool, dotool, wtype                                                                                              | A user Python config script can rewrite anything; numbers to digits                                                                                                 | `--continuous`                                                                     | Last push 2025-10-10, no releases; 1.9k stars                                                                                                                                                                                                            | GPL-3.0               |
| [IBus-Speech-To-Text](https://github.com/PhilippeRo/IBus-Speech-To-Text) | Vosk (gst-vosk)                                                                                                 | No                                                                             | **IBus engine** with live preedit                                                                                            | Punctuation, "capital letter X", spelling mode, cancel                                                                                                              | Continuous while the IME is active                                                 | 0.4.0 2022-10-02, no commits since 2022-11; 40 stars. **Stale.** There was a [Fedora Change proposal](https://www.fedoraproject.org/wiki/Changes/ibus-speech-to-text) and a [COPR](https://copr.fedorainfracloud.org/coprs/matiwari/IBus-Speech-To-Text) | GPL-3.0               |
| [Blurt](https://github.com/QuantiusBenignus/blurt)                       | whisper.cpp (local, server or whisperfile)                                                                      | Via whisper.cpp                                                                | Clipboard / PRIMARY selection, optional xdotool                                                                              | None                                                                                                                                                                | One utterance, stops on silence                                                    | v1.0.6 2025-03-30, last push 2026-05; tested to GNOME 49; 109 stars                                                                                                                                                                                      | GPL-3.0               |
| [WhisperWriter](https://github.com/savbell/whisper-writer)               | faster-whisper / API                                                                                            | CUDA                                                                           | Keyboard simulation (pynput); X11-oriented (unverified on Wayland)                                                           | None                                                                                                                                                                | Several recording modes                                                            | Last push 2024-08-24. **Stale.** 1.1k stars                                                                                                                                                                                                              | GPL-3.0               |
| [Numen](https://git.sr.ht/~geb/numen)                                    | Vosk                                                                                                            | No                                                                             | dotool (uinput)                                                                                                              | Full hands-free control language: syllable spelling, "scribe" for transcription                                                                                     | Yes                                                                                | 0.7, packaged in Alpine 3.22 (2025-09). Upstream status unverified (sourcehut returned 502)                                                                                                                                                              | AGPL-3.0 (unverified) |
| [Talon](https://talonvoice.com/)                                         | Proprietary Conformer models                                                                                    | n/a                                                                            | X11 only; **no Wayland, and public Linux support is being removed** ([OSnews, 2026-05-31](https://www.osnews.com/?p=145162)) | Most complete command grammar available                                                                                                                             | Yes                                                                                | Closed source                                                                                                                                                                                                                                            | Proprietary           |
| [Vibe](https://github.com/thewh1teagle/vibe)                             | whisper.cpp                                                                                                     | Yes                                                                            | n/a (file transcription app)                                                                                                 | n/a                                                                                                                                                                 | Files only                                                                         | v3.2.2 2026-09-05; 7.7k stars                                                                                                                                                                                                                            | MIT                   |

Smaller one-person projects also turned up: [whisper-dictate](https://github.com/Abdullah438/whisper-dictate)
(whisper.cpp + ydotool), [wayland-whisper-dictation](https://github.com/tommyqhoang/wayland-whisper-dictation)
(GNOME on Debian), [MySuperWhisper](https://github.com/OlivierMary/MySuperWhisper) (19 stars),
[TalkType](https://github.com/ronb1964/TalkType) (21 stars) and
[dictator](https://github.com/chris17453/dictator) (faster-whisper on GNOME, 5 stars). Each is
roughly what this repo already has, with fewer features. Kaldi/Dragonfly
([dragonfly](https://github.com/dictation-toolbox/dragonfly),
[kaldi-active-grammar](https://github.com/daanzu/kaldi-active-grammar), v3.2.0 2025-11) are
still maintained command-grammar frameworks, but neither is a dictation app and both target
X11 and Windows.

### 2.2 Notes on the leading options

**Vocalinux** is the closest match in features, and the most interesting to borrow from.
Its source has a real IBus engine (`text_injection/ibus_engine.py`). The engine registers
with IBus, takes text over a local socket that checks the peer's credentials, waits for
`FocusIn` and then calls `commit_text()`. The same code also handles a GNOME-specific
problem: switching IBus engines changes the XKB layout, so it reads GNOME's `input-sources`
and restores the layout afterwards. There is also a RemoteDesktop portal backend
(`remote_desktop_portal.py`) that saves a restore token, sends keys with
`NotifyKeyboardKeysym` and handles shortcuts (Ctrl+…) for its editing commands. The
command table (`command_processor.py`) is a simple phrase-to-action map. Caveats: AGPL, a
young project, and some recent commits are authored by an AI bot
(`devin-ai-integration[bot]`). I have not run it, so its accuracy and GNOME 50 behaviour
are unverified.

**Handy** is the popular choice (32.8k stars, near-daily releases, a Tauri/Rust app). It has
good model support, including Parakeet and Moonshine streaming GGUF through its
transcribe.cpp work, and an LLM post-processing toggle. For this owner, though: there are no
voice commands; it is built around one push-to-talk utterance at a time; on GNOME, wtype
does not work, so it needs ydotool or dotool (its README says this for Ubuntu 26.04); and the
recording overlay is turned off on Linux because of focus-stealing. Adopting it would mean
losing continuous unlimited dictation, the warm faster-whisper server, the
terminal-specific paste shortcut and the GNOME panel integration.

**Speech Note** works best as a writing surface. It has its own notepad, file
transcription, translation and TTS read-back (useful for proof-listening to a chapter). It
puts text into other windows only through ydotool, the same as this repo. It has no voice
commands. The latest desktop release lags behind the Sailfish one.

**Voxtype** has the widest engine list, and it ships an **eitype/libei** backend, which is the
only libei-based text injector I found in a dictation tool. Its focus is wlroots (Hyprland,
Sway, River). On GNOME, wtype fails and it falls back to dotool or ydotool, and I could not
confirm that its eitype path works on GNOME 50. It has spoken punctuation, but no editing
commands.

---

## 3. Text injection on GNOME Wayland

| Method                                                       | How it works                                                                                   | GNOME 50                                                  | Strengths                                                                                                                                                   | Weaknesses                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ------------------------------------------------------------ | ---------------------------------------------------------------------------------------------- | --------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| ydotool / dotool (uinput)                                    | Kernel virtual keyboard                                                                        | Works (current repo)                                      | Works everywhere, including XWayland and terminals                                                                                                          | Needs `/dev/uinput` access (root-equivalent input injection); sends keycodes, so Unicode/layout handling is awkward; paste shortcut differs between terminals and other apps; uses the clipboard                                                                                                                                                                                                                               |
| wtype (`zwp_virtual_keyboard_v1`)                            | Wayland virtual-keyboard protocol                                                              | **Not supported by Mutter** (stated by Voxtype and Handy) | Good Unicode on wlroots                                                                                                                                     | Not available on GNOME                                                                                                                                                                                                                                                                                                                                                                                                         |
| RemoteDesktop portal + libei (`NotifyKeyboardKeysym` or EIS) | Portal session; the compositor injects input                                                   | Supported                                                 | Approved route; keysym-based; no uinput                                                                                                                     | "Allow remote interaction?" dialog; persistence through restore tokens was unreliable in a [Nov 2025 report](https://semicomplete.com/blog/xdotool-and-exploring-wayland-fragmentation/) (whether xdg-desktop-portal-gnome persists RemoteDesktop grants on GNOME 50 is unverified); still typing key by key, so slow for long text. Tools: Vocalinux backend, [wdotool](https://github.com/cushycush/wdotool), Voxtype eitype |
| **IBus engine (`commit_text`)**                              | The input method commits a string to the focused text field over text-input-v3 / the IM module | Works: GNOME Shell drives IBus natively                   | **Whole string in one atomic commit, no clipboard, no paste shortcut, no uinput, layout-independent, full Unicode**; preedit can show interim text in place | The engine must be the active input source (layout switching problem, solved in Vocalinux); only reaches clients that speak IBus/text-input. GTK/Qt/VTE apps do. Some terminals, XWayland apps and Electron apps without Wayland IME flags may not (unverified per app); cannot send editing keys (needs a second backend)                                                                                                     |

Apps using IBus this way: **Vocalinux** (active) and **IBus-Speech-To-Text** (Vosk, stale
since 2022). There is also a crop of tiny 2026 IBus voice engines, mostly built around
Chinese cloud ASR ([typeless-ibus](https://github.com/day253/typeless-ibus),
[ibus-voice](https://github.com/stonega/ibus-voice),
[ibus-voice-ime](https://github.com/CongliangK/ibus-voice-ime), with a faster-whisper
backend), and an Fcitx5 equivalent
([fcitx5-voice-input](https://github.com/devcxl/fcitx5-voice-input), with Silero VAD).
**Speech Note has no IBus mode.** Its README describes only ydotool on Wayland.

Conclusion: the most robust GNOME setup is **IBus for text plus a key backend for
commands**. The key backend can be ydotool (already installed) or the portal. A side
benefit: IBus preedit would let continuous dictation show the current segment's interim
text in place and replace it when the final text arrives, without the paste-and-erase
problems.

---

## 4. Models that fit 4 GB (English dictation)

Open ASR Leaderboard, English short-form average WER / RTFx
([leaderboard paper v4, 2026-03-30](https://arxiv.org/html/2510.06961v4)):

| Model                                                                                                                                                | WER % | RTFx | Notes for 4 GB                                                                                                   |
| ---------------------------------------------------------------------------------------------------------------------------------------------------- | ----- | ---- | ---------------------------------------------------------------------------------------------------------------- |
| Cohere Transcribe (2B, Apache-2.0, [2026-03-26](https://the-decoder.com/cohere-releases-open-source-model-that-tops-speech-recognition-benchmarks/)) | 5.42  | 525  | Top of the board; whether it fits in 4 GB without quantisation is unverified; supported by Voxtype and hyprwhspr |
| Canary-Qwen 2.5B                                                                                                                                     | 5.63  | 418  | NeMo; tight at 4 GB                                                                                              |
| Qwen3-ASR 1.7B                                                                                                                                       | 5.76  | 148  | hyprwhspr                                                                                                        |
| Parakeet TDT 0.6B v2 (English)                                                                                                                       | 6.05  | 3390 | Small, very fast; Handy, Vocalinux, Voxtype, hyprwhspr, OpenWhispr, Speech Note                                  |
| Parakeet TDT 0.6B v3 (multilingual)                                                                                                                  | 6.32  | 3330 | Long-form English 10.7 vs Whisper turbo 11.0                                                                     |
| Distil-Whisper large-v3.5                                                                                                                            | 7.21  | 202  | Currently installed                                                                                              |
| Whisper large-v3                                                                                                                                     | 7.44  | 146  |                                                                                                                  |
| Whisper large-v3-turbo                                                                                                                               | 7.83  | 200  | Currently installed                                                                                              |
| Voxtral Small 24B                                                                                                                                    | 6.62  | 54   | Does not fit                                                                                                     |

Streaming models: [Kyutai STT 1B en/fr](https://huggingface.co/kyutai/stt-1b-en_fr) (about
2.5 GB VRAM, 0.5 s delay, semantic VAD), [Moonshine v2 streaming](https://arxiv.org/pdf/2602.12241)
(medium 245M, 2.16 % on LibriSpeech clean; Handy ships GGUFs) and
[Voxtral Realtime](https://mistral.ai/news/voxtral-transcribe-2) (4B, open weights; a 4 GB
fit is unverified). These give true live text rather than VAD segments, but none of them is
clearly more accurate than turbo on long-form writing, and leaderboard WER does not measure
punctuation or casing, which matter for prose. Whisper still produces punctuated, cased
text natively. Parakeet TDT also outputs punctuation and capitals. See
[RESEARCH-stt-models-2026.md](RESEARCH-stt-models-2026.md) for the ranked recommendation.

---

## 5. Document-writing workflows

What the tools offer for writing at length:

- **Spoken punctuation and layout** ("comma", "new paragraph"): Vocalinux, Voxtype,
  IBus-Speech-To-Text and nerd-dictation's config script. With Whisper this is partly
  unnecessary, because the model punctuates on its own. The commands that still matter for
  a book are *new paragraph*, *new line*, *scratch that* / *delete that* and quotation
  marks.
- **LLM clean-up**: Handy (post-process toggle), Voxtype (pipe to a command, with an Ollama
  example), OpenWhispr (cloud or llama.cpp) and Speech Note (no). This repo already has
  Claude post-processing.
- **Writing into a notepad, not the target app**: Speech Note and OpenWhispr (notes with AI
  actions). Commercial tools (Wispr Flow, Superwhisper, Dragon) are not on Linux (unverified
  for Wispr Flow's current platforms).
- **Command and control** (select, move, format) at Dragon level: only Talon, which is
  closed and leaving Linux, and Numen / Dragonfly, which are Vosk/Kaldi based, expect a
  learned command language and target X11 or uinput.

No open-source Linux tool does Dragon-style "select <phrase>" / "correct <phrase>" against
the text in a document. That needs to read the target's text (accessibility/AT-SPI or IBus
surrounding text) and is not solved anywhere I found.

A practical book workflow, from what exists: dictate long passages continuously into one
buffer or file (the plan's journal file already exists), and accept a few layout commands
(new paragraph, scratch that). Then run an LLM pass per paragraph or chapter that only
fixes punctuation, paragraphing and homophones, and keeps a diff so nothing is rewritten
silently. Revise afterwards in the editor. This keeps the ASR loop simple and puts the
"editing" in a step that can be reviewed.

---

## 6. Recommendation

**Keep building. Do not adopt an existing tool wholesale.** Reasons:

1. None of them matches the existing system on GNOME. Handy and Voxtype both list wtype as
   their main backend and fall back to ydotool or dotool on GNOME, which is what this repo
   already does. Adopting Handy would lose unlimited continuous dictation, the warm
   faster-whisper server, the GNOME panel extension, the terminal-aware paste and the
   Claude clean-up integration, and gain Parakeet plus a large community. Vocalinux has the
   best GNOME injection, but it is AGPL, young, and would replace a working pipeline that
   the plan is improving anyway.
2. Model choice no longer depends on the app: Parakeet and Cohere Transcribe can be added to
   the warm server (see the models research). Switching apps is not needed to get better
   models.

**Borrow these, in priority order:**

1. **An IBus injection backend** (the idea, from Vocalinux and IBus-Speech-To-Text). A small
   Python IBus engine that receives final text from `wsi-stream-server` and calls
   `commit_text()`. That gets rid of wl-copy, clipboard clobbering and the Ctrl+V /
   Ctrl+Shift+V split for apps that support IBus. Keep ydotool paste as the fallback for
   clients without IBus. Watch for: activating the engine without breaking the XKB layout
   (Vocalinux's GNOME `input-sources` handling is the reference), and checking which
   terminals the owner uses accept IBus commits. Optional later: preedit for interim text.
2. **A small voice-command layer** applied to each final segment before injection: *new
   paragraph*, *new line*, *scratch that* (delete the last segment, which needs a key
   backend: ydotool BackSpace × length, or the portal) and *open/close quote*. Use
   Vocalinux's phrase table as the vocabulary reference. Keep the list short, because
   Whisper already punctuates.
3. **A "document mode"** that sends text to a Markdown file or buffer instead of the focused
   window, with a per-paragraph LLM tidy-up that produces a diff. This fits the plan's
   existing journal or buffer design, and suits book writing better than injecting live
   into a word processor.
4. **Later, possibly:** the RemoteDesktop portal for the key-sending part, if keeping ydotool's
   uinput access becomes undesirable. First check that GNOME 50 persists the permission
   (unverified).

Uncertain: I have not run Vocalinux, Handy or Voxtype on GNOME 50, so their injection
behaviour there is taken from READMEs and source. IBus commit support in specific terminals
and Electron apps (including whichever terminal hosts Claude Code) still has to be checked
with a probe on the host before relying on it.

## Sources

- GitHub API metadata (stars, push dates, releases), read 2026-10-04, for every GitHub repo linked above.
- [Vocalinux source: ibus_engine.py, remote_desktop_portal.py, command_processor.py](https://github.com/VocaHQ/vocalinux/tree/main/src/vocalinux)
- [Handy README](https://github.com/cjpais/Handy), [Voxtype README](https://github.com/peteonrails/voxtype) and [releases](https://github.com/peteonrails/voxtype/releases)
- [Speech Note README](https://github.com/mkiol/dsnote/blob/main/README.md) and [releases](https://github.com/mkiol/dsnote/releases)
- [IBus-Speech-To-Text](https://github.com/PhilippeRo/IBus-Speech-To-Text), [Fedora Change page](https://www.fedoraproject.org/wiki/Changes/ibus-speech-to-text)
- [hyprwhspr](https://github.com/goodroot/hyprwhspr), [OpenWhispr](https://github.com/OpenWhispr/openwhispr), [nerd-dictation](https://github.com/ideasman42/nerd-dictation), [Blurt](https://github.com/QuantiusBenignus/blurt)
- [Numen on pkg.go.dev](https://pkg.go.dev/git.sr.ht/~geb/numen), [Alpine package](https://pkgs.alpinelinux.org/package/v3.22/community/x86_64/numen)
- [OSnews on Talon dropping Linux, 2026-05-31](https://www.osnews.com/?p=145162)
- [xdotool and Wayland fragmentation, 2025-11-15](https://semicomplete.com/blog/xdotool-and-exploring-wayland-fragmentation/), [wdotool](https://github.com/cushycush/wdotool)
- [Open ASR Leaderboard paper v4](https://arxiv.org/html/2510.06961v4), [Cohere Transcribe coverage](https://the-decoder.com/cohere-releases-open-source-model-that-tops-speech-recognition-benchmarks/), [Kyutai STT](https://kyutai.org/2025/06/19/stt-open-source.html), [Moonshine v2 paper](https://arxiv.org/pdf/2602.12241), [Voxtral Transcribe 2](https://mistral.ai/news/voxtral-transcribe-2)
- Roundups consulted for discovery only: [airtypes](https://airtypes.com/blog/voice-dictation-linux-whisper), [blipai](https://www.blipai.app/blog/best-dictation-apps-for-linux), [OpenWhispr blog](https://openwhispr.com/blog/best-dictation-tools-linux-2026)
