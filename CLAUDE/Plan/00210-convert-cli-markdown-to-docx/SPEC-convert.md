# SPEC-convert: the `convert` command

The intended behaviour of `convert` (Plan 00210, issue #102). Each expectation is numbered
and has a Go test in `src-go/internal/convert` written before the code it pins. There is no
bash original: this is a new tool.

## 1. What it is

`convert FILE --to FORMAT[,FORMAT...]` turns one file into other formats by calling a proven
converter (an *engine*). It does not reimplement any conversion. `convert FILE` lists what
FILE can become. The first and only pair is Markdown to DOCX, through pandoc.

## 2. The conversion table

The table is data, in one Go file (`internal/convert/table.go`). Each **input type** has:

| Field     | Meaning                                                           |
| --------- | ----------------------------------------------------------------- |
| `Name`    | the type's word, e.g. `markdown`                                  |
| `Exts`    | the file extensions it is recognised by, lower case, e.g. `.md`   |
| `Outputs` | the formats it can become, in the order `convert FILE` lists them |

Each **output** has:

| Field    | Meaning                                                                   |
| -------- | ------------------------------------------------------------------------- |
| `Format` | the word `--to` takes, which is also the output file's extension (`docx`) |
| `Desc`   | one line for the listing, e.g. `Word document`                            |
| `Engine` | the engine that does this pair                                            |

An **engine** is a Go value that names its program (for the missing-engine check), the play
that installs it, and runs one conversion through `execx.Runner`. Its program name is spelled
as a literal in its own `execx.Cmd` (the owned-programs project rule judges a literal).

The first table:

| Input      | Extensions         | Output | Engine | Engine argv                                                   |
| ---------- | ------------------ | ------ | ------ | ------------------------------------------------------------- |
| `markdown` | `.md`, `.markdown` | `docx` | pandoc | `pandoc --from=markdown --to=docx --output=<tmp> <abs input>` |

Pandoc's own `markdown` reader is used: it reads ATX and setext headings, bullet and numbered
lists, pipe and grid tables, `**bold**`/`*italic*`, fenced and indented code blocks, and
footnotes, which covers the issue's acceptance list. The input path is made absolute, so a
file named `-x.md` can never be read as an option.

A new pair is one row, an engine (new or reused) and its tests. LibreOffice
(`soffice --headless`, with a private `-env:UserInstallation` profile) is the expected engine
for office formats; it is not in this plan.

Table invariants, held by a test over the real table: every extension is lower case, starts
with `.`, and belongs to one input type; every output format is lower case, unique within its
input and has an engine and a description; every engine names a program and its play.

## 3. Command line

```
convert FILE                          list the formats FILE can become
convert FILE --to FORMAT[,FORMAT...]  write each one beside FILE
convert -h | --help                   the usage text
```

Options: `--to LIST` or `--to=LIST`; `-f`/`--force` (replace an existing output); `-h`/`--help`.

## 4. Expectations

- **E1** `-h` or `--help` anywhere prints the usage text on stdout and exits 0, whatever else
  the line holds.
- **E2** An unknown option, a `--to` with no value, a second `--to`, no FILE or more than one
  FILE is a usage error: `convert: <reason> — see 'convert --help'.` on stderr, exit 64.
  `--` ends the options, so a file named `-x.md` can be given.
- **E3** FILE that does not exist, or is not a regular file, is exit 1:
  `convert: '<FILE>' does not exist.` / `convert: '<FILE>' is not a regular file.`
- **E4** The input type is chosen by FILE's extension, case-insensitively. An extension no input
  type has is exit 1: `convert: cannot convert '<FILE>': known input types are markdown (.md, .markdown).`
- **E5** With no `--to`, stdout lists the outputs of FILE's type, one per line:
  `<format>  <description> (<engine program>)`, and exit 0. Nothing is run.
- **E6** `--to` is a comma-separated list: each item trimmed and lower-cased, duplicates
  collapsed in first-seen order. An empty item is a usage error (exit 64).
- **E7** A format the input type cannot become is exit 64, before anything runs:
  `convert: '<FILE>' (markdown) cannot become '<format>'; it can become: docx.`
- **E8** Each output is written beside FILE: FILE's directory, FILE's name without its extension,
  then `.<format>`.
- **E9** An output that already exists is exit 1 unless `--force`, checked for every requested
  format before any conversion runs: `convert: '<output>' already exists; use --force to replace it.`
- **E10** Every engine the request needs is looked up on PATH before any conversion runs. A
  missing one is exit 1: `convert: <program> is not installed; the play that installs it is <play> (run it, do not install by hand).` The same message is given if the engine disappears
  between the check and the run (the runner's not-installed error).
- **E11** An engine writes to a temporary beside the output (`<output>.tmp.<pid>`), which is
  renamed onto the output only when the engine exits 0, so no reader ever sees half a file.
- **E12** An engine that exits non-zero is exit 1:
  `convert: <program> could not convert '<FILE>' to <format>: <its stderr>`. The temporary is
  removed. Outputs already written by earlier formats in the same run stay, and are listed on
  stdout as written (E13). No later format runs.
- **E13** Each output written is printed on stdout, one path per line, as given relative to the
  invocation (the payload a caller captures); exit 0 when all are written.
- **E14** Cancelled (SIGINT, SIGTERM) while an engine runs: the engine gets SIGTERM through the
  runner, the temporary is removed, and the exit is 130 with `convert: cancelled.`
- **E15** stdout that cannot be written turns an otherwise successful run into exit 1.

## 5. Exit codes

| Code | Meaning                                                                      |
| ---- | ---------------------------------------------------------------------------- |
| 0    | listed, or every requested output written                                    |
| 1    | input missing or of an unknown type, output exists, engine missing or failed |
| 64   | usage: bad option, no or extra FILE, empty or unsupported `--to` item        |
| 130  | cancelled                                                                    |
