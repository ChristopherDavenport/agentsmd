# agentsmd

The [AGENTS.md](https://agents.md/) convention for Go agents: the
instruction files that apply at a path, found from its directory up to
a root, one per directory, nearest last, and rendered into the text a
product puts in its instructions. The nearest file is the last one
written and wins. The module imports the standard library alone.

```go
res, err := agentsmd.Chain(cwd, agentsmd.Options{
	Names:  []string{"AGENTS.override.md", "AGENTS.md"},
	Root:   repoRoot,
	Extra:  []string{filepath.Join(home, ".dax", "AGENTS.md")},
	Budget: 32 << 10,
})
if err != nil {
	return err
}
for _, o := range res.Omitted {
	log.Printf("%s (%d bytes): %s", o.Path, o.Size, o.Reason) // shadowed or over budget
}
cfg.Instructions = agentsmd.Render(res.Files)
```

`Chain` returns `Extra` first, then walks from the path's directory to
`Root`, takes in each directory the first of `Names` that the directory
lists under exactly that spelling, and returns those files farthest
first and nearest last, so that later text refines earlier text. `Extra`
comes first because a file outside the tree, such as the user's own in
their home directory, is the farthest of the lot: Codex reads
`~/.codex/AGENTS.md` before the repository's files for that reason, and
the repository's nearest file still has the last word. `Budget` caps the
total and is spent in that same order: the first file that would exceed
it ends the chain, nothing is cut short, and running out is not an
error. `RenderedBudget` caps `Render`'s output the same way, wrapper and
paths included, for a product that bounds its prompt rather than the
files. `Budget` and `MaxBytes` count the bytes a file reads, not the
size its stat reported, so an `FS` that expands a file as it reads it is
held to them too. Every file found and left out, shadowed by a preferred
name or over budget, is in `Result.Omitted` with its size and its path,
so a product can tell the user and a session can record what the model
was not given as well as what it was.

`Render` wraps the files as pi renders `<project_context>`: one
`<project_instructions path="...">` per file, in order. The result is
one instructions part, identified by `agentsmd.PartID`: a product that
records what the model was given in parts rather than as one string
hands it the whole rendering under that id, and names a file it
considered and left out by that file's `Omitted.Path`.

The walk reads the OS file system unless `Options.FS` is set. A
product whose project lives somewhere it has no OS path for, such as a
container or a remote workspace, passes that project as an `fs.FS`:

```go
res, err := agentsmd.Chain("services/api", agentsmd.Options{
	FS:    workspace.FS(), // "." is the project's root
	Root:  ".",
	Extra: []string{filepath.Join(home, ".dax", "AGENTS.md")},
})
```

The path, `Root` and the chain's directories are then names in the FS
(`fs.ValidPath`, `"."` for its root), directories are listed with
`fs.ReadDir`, files are stat'd with `fs.Stat` and read through `Open`,
and `File.Path` and `Omitted.Path` are the names in the FS, which the
product maps to what it shows before `Render`. `RenderedBudget` counts
the paths as `Chain` returns them, so a product that rewrites them
changes the rendered size and bounds the rewritten rendering itself. The
walk only asks the FS for names below `"."` and resolves no links
itself, so an FS that confines, such as `os.Root.FS()`, keeps its
confinement: a link it refuses is `Chain`'s error, not a missing file.
`Extra` stays OS paths, since the user's own file is not part of the
project.

The package does not expand imports inside files, know any file name
specially, or fetch anything.

## Beyond the convention

[agents.md](https://agents.md/) says the file is named `AGENTS.md` and
that the one nearest the edited file wins. It does not say how several
files combine, how large they may be, how their text enters a prompt,
or whether one file imports another. This library decides:

- **Name.** A directory's file is the first of `Names` (`AGENTS.md` by
  default) equal byte for byte to a name the directory lists, on every
  host: `agents.md` is not `AGENTS.md`, on macOS either.
- **Combining.** One file per directory from `Root` down to the path's
  directory, farthest first and nearest last, so the nearest is the
  last text the model reads; `Extra` comes before them all.
- **Size.** Nothing is cut short. A file over `MaxBytes` is an error;
  the first file over `Budget` (the files) or `RenderedBudget` (the
  rendering) ends the chain without error. All three count the bytes a
  file reads, not the size its stat reported.
- **Paths in the budget.** `RenderedBudget` counts each path as `Chain`
  returns it; rewriting `File.Path` before `Render` changes the size.
- **Rendering.** `<project_context>\n`, then per file
  `<project_instructions path="P">\n`, the content, `\n` if the content
  does not end in one, and `</project_instructions>\n`; then
  `</project_context>`, with no newline. `P` has `&`, `<`, `>` and `"`
  escaped as `&amp;`, `&lt;`, `&gt;` and `&quot;`. No files render as
  nothing. These bytes are the `agentsmd` part a session records and
  hashes; `testdata/golden/render.txt` is an example.
- **Imports.** None: an `@path` line is text, for a product to expand.

## Development

```sh
make check    # gofmt, tidy, vet, deps, staticcheck, govulncheck, race tests
```

The fixture tree is `testdata/tree`; the golden render is
`testdata/golden/render.txt`, regenerated with `go test . -update`.
