# Contributing

Issues and pull requests are welcome.

## Before you start

This library finds and renders AGENTS.md files; it does not decide
which names a product looks for, expand imports inside a file, fetch
anything or read outside the local file system. A change that needs a
transport or a prompt builder belongs in the product.
`docs/plans/agentsmd.md` is the design; read it first.

For anything larger than a bug fix, open an issue first so the shape of
the change can be discussed before you spend time on it.

## Development

Go 1.25 or later is required. The full local check is:

```sh
make check        # gofmt, tidy, vet, deps, staticcheck, govulncheck, race tests
```

The module imports the standard library alone; `make deps` and a test
fail if anything else creeps in.

The fixture tree lives under `testdata/tree` with the golden render
under `testdata/golden`; regenerate it with `go test . -update` and
review the diff.

## Pull requests

- Keep the change focused; unrelated cleanups belong in their own PR.
- Add or update tests. Tests are table-driven and run offline.
- Run `make check` before pushing. CI runs the same steps on the minimum
  and current Go versions.
- Note user-visible changes under *Unreleased* in `CHANGELOG.md`.
