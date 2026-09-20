# agentsmd

The [AGENTS.md](https://agents.md/) convention for Go agents: the
instruction files that apply at a path, found from its directory up to
a root, one per directory, nearest last, and rendered into the text a
product puts in its instructions. The module imports the standard
library alone.

```go
res, err := agentsmd.Chain(cwd, agentsmd.Options{
	Names:  []string{"AGENTS.override.md", "AGENTS.md"},
	Root:   repoRoot,
	Extra:  []string{filepath.Join(home, ".dex", "AGENTS.md")},
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

`Chain` walks from the path's directory to `Root`, takes in each
directory the first of `Names` that exists, and returns the files
farthest first and nearest last, then `Extra`, so that later text
refines earlier text. `Budget` caps the total: the first file that
would exceed it ends the chain, nothing is cut short, and running out
is not an error. Every file found and left out, shadowed by a
preferred name or over budget, is in `Result.Omitted` with its size,
so a product can tell the user and a session can record what the model
was not given as well as what it was.

`Render` wraps the files as pi renders `<project_context>`: one
`<project_instructions path="...">` per file, in order.

The package is about a repository checkout on the local file system.
It does not expand imports inside files, know any file name specially,
or fetch anything; a product with a remote checkout hands its files to
`Render` itself.

## Development

```sh
make check    # gofmt, tidy, vet, deps, staticcheck, govulncheck, race tests
```

The fixture tree is `testdata/tree`; the golden render is
`testdata/golden/render.txt`, regenerated with `go test . -update`.
