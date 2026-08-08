# OpenMANETd — Codex Guide

`CLAUDE.md` is the canonical repository guide. Before modifying this repository,
read it completely and follow its commands, architecture notes, generated-code
boundaries, testing patterns, and platform gotchas.

The detailed rules under `.claude/rules/` also apply to Codex. Read the rules
relevant to the work before editing:

- All Go changes: `concurrency.md`, `performance.md`, `idiomatic-go.md`, and
  `testing.md`.
- API changes: `api-design.md` and `testing.md`.
- Frontend changes: `frontend.md` and the frontend sections of `testing.md`.
- Instrumentation snapshot changes: `instrumentation.md`.

Treat these referenced documents as repository instructions, not optional
background material. Keep this file as a small entrypoint so the Claude and
Codex guidance cannot drift into conflicting copies; update the canonical guide
or shared rule files when project policy changes.

At minimum, every behavior change must have tests, all Go changes must be
formatted and lint-clean, and concurrency-sensitive changes must pass the race
detector. Never directly edit generated files under `internal/api/`,
`frontend/src/gen/`, or `internal/database/models/`.
