# Plan: AGENTS.md

The [AGENTS.md](https://agents.md/) convention, loaded into the model's
context: the instruction files a repository keeps for coding agents,
found from a path up to a root and rendered into the text a product
puts in `agentturn.Config.Instructions`, which `agentsession` records
in the config entry.

This began as the `instructions` subpackage of `agentskill` and moved
out at agentskill v0.0.2, because the two are different concepts. A
skill is loaded on use: its description is always in the prompt, its
body and files reach the model through a tool when a task matches.
AGENTS.md is loaded up front: every applicable file is in the prompt
before the first turn. They share nothing but the field they land in,
and nobody looking for AGENTS.md handling expects to find it in a
skills module.

## Goals

- Discover AGENTS.md files from a directory to a root, one per
  directory by name preference, nearest last, as Codex reads them.
- Bound what goes in without cutting a file short, and say what was
  left out, so the product and the session know what the model was
  not given as well as what it was.
- Render the files as pi renders `<project_context>`.
- Standard library only.

## Non-goals

- Deciding which names apply. Codex reads `AGENTS.override.md` then
  `AGENTS.md`; Claude Code reads `CLAUDE.md`; dex wants both. The
  caller lists names in preference order and the package knows none
  specially.
- Imports inside a file (`@path` in CLAUDE.md). AGENTS.md has none. A
  product that wants them expands them before rendering.
- Files below the path. `Chain` on the target file covers the case the
  convention describes; reopen if a product wants a whole-tree index.
- A remote checkout. The convention is about a repository on disk; a
  product with files elsewhere hands them to `Render` itself.
- Prompt templates, slash commands, `SYSTEM.md` and `APPEND_SYSTEM.md`.
  Those are the product's prompt builder, which composes this output.

## API

```go
// Chain returns the instruction files that apply at path: in path's
// directory and each of its ancestors up to Root, the first file of
// Names that exists, farthest first and nearest last, followed by
// Extra in order, within Budget. With nearest last, later text
// refines earlier text, which is how the convention's "closest file
// takes precedence" reads when a product includes every file, as pi
// and Claude Code do. A file found and not included is reported.
func Chain(path string, opts Options) (Result, error)

type Options struct {
    Names    []string // tried in order per directory, first found wins; default: AGENTS.md
    Root     string   // stop after this directory; default: the filesystem root
    Extra    []string // explicit paths appended last, such as ~/.dex/AGENTS.md; missing ones are skipped
    MaxBytes int64    // per file that is included; default 1 MiB, a larger one is an error
    Budget   int64    // total; the first file that would exceed it ends the chain, without error; default: none
}

type Result struct {
    Files   []File    // included, in order
    Omitted []Omitted // found and left out, in the order met
}

type File struct {
    Path    string // absolute
    Content string // verbatim
}

type Omitted struct {
    Path   string // absolute
    Size   int64
    Reason Reason // OverBudget or Shadowed
    By     string // the file that stood in for it, for Shadowed
}

// Render wraps the files as pi does: one <project_instructions
// path="..."> per file inside <project_context>, in order.
func Render(files []File) string
```

`Chain` takes a path rather than a cwd so one call serves both the
session's working directory and the file a tool is about to touch in a
monorepo. `Names` is a preference order and yields at most one file
per directory, as Codex reads `AGENTS.override.md` before `AGENTS.md`,
so a developer can shadow a committed file without deleting it, and
dex can let `CLAUDE.md` stand in where `AGENTS.md` is absent; the
package knows no name specially. `Budget` caps the total, the way the
reference's `project_doc_max_bytes` does: the first file that would
exceed it, and everything after it, is left out and `Chain` returns
what fits without error, so a large file deep in a tree degrades the
prompt rather than failing the run. No file is cut short, because a
half instruction file is a worse instruction than none; `MaxBytes`
stays a per-file error for a file that would be included and could
never fit. Neither omission is silent: `Result.Omitted` names every
file `Chain` found and left out, with its size and why, so a product
can tell the user that a rule file was shadowed or did not fit, and
the session can record what the model was not given as well as what
it was. Only a file that is included is read; a shadowed or
over-budget file is stat'd for its size and left alone. This package
stays on the local file system: the convention is about a repository
checkout, and a product with a remote checkout hands the files to
`Render` itself.


## Invariants

- `Chain` returns files farthest first and nearest last, at most one
  per directory, then `Extra` in order.
- A returned file is byte for byte what is on disk; nothing is
  truncated, reflowed or summarised.
- Every file `Chain` found and did not return is in `Result.Omitted`
  with the reason; only a returned file is read.
- A breached `Budget` is never an error; a breached `MaxBytes` on a
  file that would be returned always is.
- The package imports the standard library only.

## Testing

Table-driven and offline. `testdata/tree` holds files at several
depths, a directory with both `AGENTS.md` and `AGENTS.override.md`,
and a directory with `CLAUDE.md` alone; the golden render is
regenerated with `go test . -update`. A symlinked file is created at
test time, since a link in `testdata` does not survive every checkout.

## Open questions

- Provenance. The rendered chain lands in `Config.Instructions` as
  text, so the session records what the model read but not which file
  each piece came from or its hash. `Result` is already the record of
  what was found, included and omitted; a manifest would add the
  SHA-256 of each included file and a place for it in the session,
  decided with `agentsession` alongside the same question for skills.
- `Budget` has no default. Codex's is 32 KiB, but Codex truncates to
  fit and this package does not, so a default would drop whole files;
  whether to do that is the product's call. Reopen if every product
  ends up setting the same value.
