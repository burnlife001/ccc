# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this repo is

Design/spec repository for **ccc** — a Go single-exe rewrite of the TypeScript `ccs` (Claude Code switcher) tool. There is no Go source here yet; this repo holds the plan, the architecture contract, the acceptance criteria, and TypeScript reference files extracted from the old `ccs` project (`E:/__work/__github/ccs`).

The Go source tree will live one level up at `E:/__work/__github/ccc/` (sibling of this `docs/` directory).

## The three governing documents (read before writing code)

1. **`todolist.md`** — phased build plan (Phase 0 scaffold → Phase 4 polish). Work proceeds phase by phase; each phase lists its own verification step.
2. **`go架构规范.md`** — the architecture contract. **Hard rules that override convenience:**
   - Exactly five files (four layers), all `package main`: `main.go` (entry, sole `os.Exit`), `cli.go` (dispatch), `exec.go` (sole file allowed to import `os/exec`), `profiles.go` (profile file CRUD), `paths.go` (sole source of path constants).
   - Dependency direction is one-way: `main → cli → {exec, profiles} → paths`. No upward imports; `exec` and `profiles` never import each other.
   - No third-party dependencies (stdlib only; any exception needs a stated reason in the commit message).
   - No goroutines/channels, no `init()`, no package-level mutable vars, no interfaces until a second implementation exists.
   - Errors bubble up with `%w` wrapping; only `main` prints and exits. No bare `panic` outside a `recover()` guard in `main`.
3. **`ccs验收标准.md`** — acceptance test matrix (P0 must all pass to ship, P1 to replace old ccs, P2 bonus). Every behavior claim must be verifiable against one of its numbered rows. It also lists explicit non-goals: no account isolation, no `CLAUDE_CONFIG_DIR` rewriting, no MCP/hook injection, no daemons, no writes under `~/.claude/`, no runtime deps.

## Core design decision (do not deviate)

**A profile JSON file IS the `--settings` argument — zero transformation, byte-for-byte passthrough.** `ccc <profile> [args...]` spawns `claude --settings <exeDir>/.ccc/profiles/<name>.json [args...]` with stdio inherited and unknown flags appended verbatim. `exec.go` must not parse, validate fields of, or rewrite profile content — only check existence and JSON validity.

## What to copy from `ccs-lite-reference/` (TypeScript)

| File | Port to Go |
|---|---|
| `shell-executor.ts` | Three-branch Windows spawn (direct exe / `.ps1` via `powershell.exe -File` / `.cmd` via `cmd /c`) and `stripAnthropicEnv` (drop all parent `ANTHROPIC_*` before spawn so profile settings win) |
| `settings-flow.ts` (414–433) | `--settings <path>` argument assembly and verbatim user-arg passthrough |
| `headless-executor.ts` (295–333) | `-p` mode command line: `--output-format stream-json --verbose`, optional `--permission-mode`, and session resume via `--resume <sessionId>`; `session_id` is captured from stream-json output and stored in `<exeDir>/.ccc/sessions.json` |
| `delegation-handler.ts` | Dispatch order: `<profile>:continue` → `<profile>` → subcommands; unknown flags pass through untouched |
| `ccs.ts` | Entry dispatch order: `profile` / `profile:continue` / subcommand (`version`/`list`/`add`/`rm`/`which`) resolution |
| `settings_minimax_m3.template.json` | The exact template `ccc add` generates |

## Build, install, verify

- Build: `go build -ldflags "-s -w" -o dst/ccc.exe ./src` (via `build.ps1`). No install step, no PATH changes — the single exe (~2.5 MB) runs straight from `dst/`. Target: exit code 0 on `./dst/ccc.exe version`. Go sources live in `src/`; build artifacts land in `dst/`.
- Verification is **acceptance-driven**, not test-suite-driven: execute the operations in `ccs验收标准.md` and record operation → expected → evidence. Unit tests (table-driven, per `go架构规范.md` §6) are auxiliary; `exec.go` is tested via its pure argument-assembly function, never real spawns.
- Test environment expectations: Windows 11, both pwsh and git-bash; `claude` CLI available; `dst/.ccc/profiles/minimax_m3.json` pre-populated from the template with a real token.

## Data locations

- Profiles (runtime): `<exeDir>/.ccc/profiles/<name>.json`; sessions: `<exeDir>/.ccc/sessions.json` — **data dir sits next to ccc.exe** (i.e. `dst/.ccc/` in this repo), derived at runtime from `os.Executable()`, never persisted as absolute paths. `CCC_HOME` env var overrides the parent dir (test isolation hook only). `dst/.ccc/` is gitignored — it holds plaintext tokens.
- Old tool's data for migration/verification: `C:/Users/yg/.ccs` and `E:/__work/__frequently/personal_data/apikey/decrypt/cc_switcher`.
