# Changelog

All user-visible changes to this library. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project
uses [Semantic Versioning](https://semver.org/); before v1.0.0 minor
versions may break the API.

## Unreleased

- Initial release, moved out of `agentskill/instructions` as it stood
  in agentskill v0.0.1: `Chain` finds the AGENTS.md files that apply
  at a path, one per directory by name preference, farthest first and
  nearest last, within an optional byte budget, and reports every file
  found and left out; `Render` wraps them as pi renders
  `<project_context>`.
