# Changelog

All user-visible changes to this library. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
uses [Semantic Versioning](https://semver.org/); before v1.0.0 minor
versions may break the API.

## Unreleased

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
