# Plan 00150 — encoding research

Input: `assets/example-terminal-screenshot.png`, a 3028x110 terminal capture (light text
and colour emoji on a dark background), 92,961 bytes. All candidates were made with
ImageMagick (`magick`, libwebp 1.6.0) and `-strip`.

## Sizes

| Candidate                         | Bytes   | Base64 chars | zstd -19 bytes |
| --------------------------------- | ------- | ------------ | -------------- |
| PNG24 (re-saved original)         | 102,905 | 137,208      | 98,130         |
| PNG8, 4 colours                   | 11,477  | 15,304       | 11,028         |
| PNG8, 8 colours                   | 14,831  | 19,776       | 14,308         |
| PNG8, 16 colours                  | 20,103  | 26,804       | 19,579         |
| PNG8, 32 colours                  | 21,632  | 28,844       | 21,120         |
| PNG8, greyscale 8                 | 23,564  | 31,420       | 23,146         |
| WebP lossless                     | 48,686  | 64,916       | 48,700         |
| WebP q40                          | 16,610  | 22,148       | 16,541         |
| WebP q60                          | 19,832  | 26,444       | 19,746         |
| WebP q80                          | 25,172  | 33,564       | 25,064         |
| JPEG q70                          | 37,543  | 50,060       | 36,011         |
| WebP q40, resized to 2000 px wide | 10,076  | 13,436       | —              |
| WebP q50, resized to 2000 px wide | 11,272  | 15,032       | —              |
| WebP q60, resized to 2000 px wide | 12,238  | 16,320       | —              |

## Findings

- **Extra compression is not worth it.** zstd -19 saves only 1–4% on an image that is
  already encoded, and base64 output itself compresses badly. Pipeline: re-encode, then
  base64. Nothing else.
- **Palette PNG loses emoji colour.** At 4 and 8 colours the text is still legible but the
  emoji turn grey. That matters when, as in this example, the emoji are the subject.
- **Lossy WebP keeps colour and legibility.** At q40 and above the text is crisp in Read.
- **Capping the longest edge at 2000 px is free.** The Read tool displays this image at
  2000x73 whatever its source size. The q50 resize looked the same in Read as the
  original and cut the bytes by about 30%.
- **Chosen default: WebP q50, longest edge ≤ 2000 px.** For this input that is a
  ~15.5 KB block.

## Busy images and the block budget (Task 1.5)

The single setting does not scale. A 1906x1385 media-player UI with album art comes out at
129,334 bytes at q50, a ~175,000-char block. A block is only useful if the receiving agent
can re-type it into one tool call.

**Measured transcription.** A 35,311-char block (that UI at 1000 px, q30) was printed and
copied back into one bash call by the agent; `sha256sum -c` said OK. 15.5K and 35K are
proven, and nothing larger was tried.

**Legibility held at every step tried.** The UI at 1000 px q30 keeps its titles and
buttons readable. A receipt with a small-print table stays readable at 1200 px q30. A
1811x5992 web page squeezed to 604x2000 keeps its body text.

**Chosen: a ladder under a 40,000-char budget.** The encoder tries 2000 px q50, then
2000 q30, then 1568, 1200, 1000 and 800 px at q30, and prints the first block that fits.
If none fits it prints nothing and fails, telling the user to crop. On the five test
images:

| Image                       | Source    | Chosen step   | Block chars |
| --------------------------- | --------- | ------------- | ----------- |
| Terminal line (the example) | 3028x110  | 2000 px, q50  | 15,523      |
| Media-player UI             | 1906x1385 | 1000 px, q30  | 35,605      |
| Receipt                     | 1050x2299 | 1200 px, q30  | 36,874      |
| Tall web page               | 1811x5992 | 2000 px, q50  | 31,496      |
| Phone UI                    | 1125x2436 | 1568 px, q30  | 32,051      |
| Random noise (failure case) | 2000x2000 | none, exits 1 | —           |

## Block format (prototype)

The block is valid bash, so the receiving agent runs it rather than having to work out
how to decode it:

```
# IMGPASTE v1: 2000x73 image. Agent: run this whole block in bash, then view <path>
base64 -d > <path> <<'IMGPASTE_END'
<base64, 76 columns>
IMGPASTE_END
echo '<sha256>  <path>' | sha256sum -c
```

The full sha256 lets `sha256sum -c` catch any transcription error. A transcription error
is a real risk: the receiving agent re-types the base64 into its tool call.

The encoder's stderr lines start with `# `. A terminal copy often takes them along with the
block, and as comments they cannot make the receiver's run fail.

## Decode path

`<path>` is `./imgpaste-<id>.webp`, relative to the receiver's current directory. A
`/tmp` path was refused in this repository: the project-containment hook blocks an
agent's command from writing outside the repo root, and other receivers may have
similar limits.
