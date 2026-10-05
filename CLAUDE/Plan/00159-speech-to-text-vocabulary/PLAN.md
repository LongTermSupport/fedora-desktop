# Plan 00159: speech to text vocabulary

**Status**: Not Started
**Created**: 2026-10-05
**Owner**: joseph
**Priority**: Medium

## Overview

Whisper keeps mishearing the same phrases, names and jargon most of all ("Hooks Daemon"
comes out as "Hooks Steaming"). The owner asked for the standard fix: a vocabulary the
recognizer is primed with, and a list of known mishearings corrected afterwards.

Owner's answers:

- **Both mechanisms.** (a) The vocabulary goes to faster-whisper as `hotwords` (and is
  added to the text-so-far prompt continuous dictation already passes), in every mode:
  batch, streaming, the warm server and article mode. (b) A "heard → meant" replacement
  list is applied to the final text, before Claude post-processing sees it.
- **The words are personal, so they never go in this public repo.** They live under
  `~/.config/fedora-desktop/`, the new home for per-user fedora-desktop data, and that
  folder is tracked in the owner's private config repo (`<account>/fedora-desktop-config`,
  which `run.bash` already reads through the GitHub API), so the words are versioned and
  the same on every machine.
- Replacements run before Claude post-processing (the recommended answer; not
  contradicted).

## Goals

- `~/.config/fedora-desktop/speech-to-text/vocabulary.txt` (one word or phrase per line)
  primes every recorder; `replacements.txt` (`heard => meant`, whole-phrase,
  case-insensitive) corrects the final text.
- Settings has a row for each that opens it in the editor.
- `~/.config/fedora-desktop/` is a checkout of the private config repo, managed by a play,
  never overwriting local edits. Without a config repo it is a plain folder and every
  feature still works.
- The Claude prompt files move from `~/.config/speech-to-text/` to
  `~/.config/fedora-desktop/speech-to-text/` once, keeping the owner's edits.

## Non-Goals

- Shipping any words in this repo. The files are created empty, with a commented example.
- A trained or fine-tuned model.

## Tasks

### Phase 0: decisions

- [ ] ⬜ **Task 0.1**: Owner: how much of the config repo is on disk. The config repo also
  holds the Ansible vault and each host's config. Recommended: a sparse checkout of one
  folder (`home-config/`) at `~/.config/fedora-desktop/`, so the vault never lands there.
- [ ] ⬜ **Task 0.2**: Owner: how edits get back to GitHub. Recommended: a
  `fedora-desktop-config sync` command (commit, pull with rebase, push), also run by the
  panel's Settings rows after the editor closes; never automatic in the background.

### Phase 1: the config folder

- [ ] ⬜ **Task 1.1**: A play (tests first for any helper) that clones or sparse-checks-out
  the config repo at `~/.config/fedora-desktop/`, fast-forward only, refusing to touch a
  dirty or diverged checkout; a plain folder when there is no config repo.
- [ ] ⬜ **Task 1.2**: The sync command from Task 0.2.

### Phase 2: vocabulary

- [ ] ⬜ **Task 2.1**: A reader shared by the recorders: the vocabulary as `hotwords`,
  the replacement list parsed strictly (a bad line is an error naming the line).
  Check how faster-whisper 1.1.1 (RealtimeSTT's pin) and 1.2.1 treat `hotwords` together
  with `initial_prompt`, and fit within the prompt's token limit.
- [ ] ⬜ **Task 2.2**: `wsi`, `wsi-stream`, `wsi-stream-server` and article mode pass the
  vocabulary and apply the replacements, before Claude post-processing.
- [ ] ⬜ **Task 2.3**: Settings rows; the Claude prompt files move; docs.

### Phase 3: review and deploy

- [ ] ⬜ **Task 3.1**: `qa-reviewer`; the play run through `meta-deploy.bash`.
- [ ] ⬜ **Task 3.2**: Owner: add "Hooks Daemon" and a replacement for "hooks steaming",
  dictate it, and see it come out right.

## Success Criteria

- [ ] "Hooks Daemon" dictated comes out as "Hooks Daemon" with the owner's lists.
- [ ] No word list is in this repository; the lists are in the private config repo.
- [ ] With no config repo and empty lists, dictation behaves exactly as before.

## Delivery & Milestones

- <!-- delivery commit hashes -->
