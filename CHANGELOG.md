# Changelog

All user-visible changes to this library. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
uses [Semantic Versioning](https://semver.org/); before v1.0.0 minor
versions may break the API.

## v0.0.5 - 2026-11-08

- Added: `Options.Truncate`. Under it, the first file that does not fit
  `Budget` or `RenderedBudget` is cut instead of left out whole: it
  keeps the longest part that ends at a line boundary and fits with a
  marker line after it, `[agentsmd: N bytes of PATH left out over the
  budget]`, which counts against the budgets. The cut file keeps its
  place in `Result.Files` and is also in `Result.Omitted` with the new
  reason `Truncated` and the `N` bytes cut off as its `Size`; every
  later file is `OverBudget`. A file the marker alone does not fit is
  left out whole, as before. A file whose stat does not fit is read no
  further than one byte past the room the budgets leave, or past
  `MaxBytes` when that is less, so an endless file is cut too; when the
  read stops before the file ends, `N` is the stat's size less the
  bytes kept. `Truncate` turns no omission into an error: such a file
  that reads over `MaxBytes` with room to spare is left out as
  `OverBudget`, as without it, and `MaxBytes` holds as before for a
  file whose stat fits. Off by default; without it nothing changes
  (#13).

## v0.0.4 - 2026-10-27

- Changed: a directory's file is the first of `Options.Names` equal,
  byte for byte, to the name of an entry the directory lists. `Chain`
  used to ask the file system for each name, so on a case-insensitive
  file system, macOS's by default, an `agents.md` answered for
  `AGENTS.md` and was read, where Linux left it out. It is now left
  out on every host; a product that wants it lists `agents.md` in
  `Names`. A name that is not one path element is an error, and so is
  a directory of the chain that exists and cannot be listed. An
  `Options.FS` must now support listing directories (#4).
- Fixed: `MaxBytes` and `Budget` apply to the bytes a file reads, not
  the size its stat reported, so an `Options.FS` that expands imports,
  or a file that grows between the stat and the read, no longer takes
  the chain past either with nothing reported. A file that reads over
  `MaxBytes` is `ErrTooLarge`; one that reads past the budget left is
  omitted as over budget with the size it read and ends the chain.
  The read stops one byte past `MaxBytes`. A file whose stat is over
  `MaxBytes` but reads within it is now included. Files are read
  through the FS's `Open`, not `fs.ReadFile` (#11).
- Added: `Options.RenderedBudget`, a budget over `Render`'s output,
  wrapper and paths included, so a product bounding its prompt sets
  the number the model pays in one `Chain`. It is spent like `Budget`,
  and with both set the first file over either ends the chain. Paths
  count as `Chain` returns them: a product that rewrites `File.Path`
  before `Render` changes the rendered size (#5).

## v0.0.3 - 2026-10-09

- Added: `Options.FS`. When set, `Chain` walks and reads the chain
  through that `fs.FS` instead of the OS file system, so a product
  whose project is in a container or a remote workspace reads the
  project's AGENTS.md files where the project is. The path and `Root`
  are `fs.ValidPath` names in it (`"."` its root, and the default
  `Root`), files are found with `fs.Stat` and read with `fs.ReadFile`,
  and `File.Path`, `Omitted.Path` and `Omitted.By` are the names in
  the FS. The walk never names anything outside the FS and resolves no
  links itself, so a link that a confining FS refuses is an error, not
  a missing file. `Extra` stays OS paths. With `FS` nil nothing
  changes.

## v0.0.2 - 2026-09-21

- **Breaking**: `Options.Extra` is included before the chain rather
  than after it, and is the first charge on `Options.Budget`. The
  convention is that the nearest file wins, which `Chain` expresses by
  putting the nearest file last; appending an explicit path after the
  chain made it the strongest text in the prompt, and the README's own
  example of one is a file in the user's home directory, which should
  be the weakest. Codex reads the global file first for this reason. A
  product that relied on `Extra` overriding the tree must now render
  those files itself after `Render`.
- Added: `PartID`, the stable identifier of the one instructions part
  `Render` produces, for a product that records what the model was
  given in parts rather than as one string; an omitted file's stable
  key is its `Path`, which is now said where it is defined.

## v0.0.1 - 2026-09-20

- Initial release, moved out of `agentskill/instructions` as it stood
  in agentskill v0.0.1: `Chain` finds the AGENTS.md files that apply
  at a path, one per directory by name preference, farthest first and
  nearest last, within an optional byte budget, and reports every file
  found and left out; `Render` wraps them as pi renders
  `<project_context>`.
