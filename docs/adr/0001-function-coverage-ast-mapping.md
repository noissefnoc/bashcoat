# ADR 0001: Function Coverage via Static AST Innermost `FuncDecl` Mapping

- **Status**: Accepted
- **Date**: 2026-11-26
- **Branch**: `feat/function-coverage`

## Context

`bashcoat` measures C0 line coverage for Bash 5.0+ scripts using static AST statement detection (`mvdan.cc/sh/v3/syntax`) paired with runtime `BASH_XTRACEFD` / `PS4` tracing.

To support function-level coverage across standard report formats—LCOV (`FN`/`FNDA`/`FNF`/`FNH`), Cobertura XML (`<methods>`), and the terminal summary (`Funcs` / `Fn Cov`)—we needed an approach that preserves `bashcoat`'s core priorities:

1. **Minimal runtime overhead** and zero extra runtime dependencies.
2. **Zero behavioral side effects** on tested scripts (including strict `set -euo pipefail`, caller `$?`, subshells, pipelines, and test runners like Bats).
3. **Accurate function coverage ratio (`FNH`/`FNF`)** with deterministic, tool-compatible reports.

## Decision

We adopt **Static AST Innermost `FuncDecl` Ownership Mapping** without modifying runtime instrumentation (`PS4`) or trace format:

1. **Innermost Function Ownership**: During the existing `mvdan.cc/sh/v3/syntax` AST walk in `internal/executable`, maintain a stack of enclosing `*syntax.FuncDecl` nodes so each executable statement line is owned strictly by its innermost enclosing function. Top-level statements belong to no function, and nested function bodies do not falsely mark outer functions as covered.
2. **Post-Trace Aggregation**: In `internal/trace`, map collected line hits onto statically extracted function definitions. A function is marked covered when at least one of its owned executable lines has hits (`LinesHit > 0`).
3. **Duplicate-Name Aggregation**: Multiple declarations sharing the same function name within a single file (e.g., conditional OS branches) are aggregated by name (earliest `StartLine`, union of owned executable lines, summed call counts) to avoid duplicate-key collisions in LCOV tools such as `genhtml`.
4. **Approximate `FNDA` Counts**: LCOV `FNDA` call counts are approximated from the execution count of each function's earliest executable line (falling back to the minimum non-zero hit count within the function).

## Alternatives Considered

We prototyped and benchmarked three approaches in parallel across 10 edge-case scenarios and two real Bash 5.3 workloads (Call-heavy: 2,000 function calls / 16k xtrace lines; Tight-loop: 1 call / 2,000 iterations), plus Go microbenchmarks (`ParseAll` on 20k lines).

| Approach | Call-heavy Runtime | Trace Size | `ParseAll` Time / Mem | Edge-Case Notes | Verdict |
| :--- | :---: | :---: | :---: | :--- | :--- |
| **AST Mapping** *(Chosen)* | **±0%** (66.1 ms vs 67.2 ms) | **+0.0%** | **1.31 ms / 1.28 MB** | Preserves `$?`; handles nesting, subshells, pipelines, `main()` collision; cannot split 2 funcs on 1 line or `eval` funcs | **Accepted** |
| **`PS4` `${FUNCNAME[0]}` + AST** | +9.5% (73.5 ms) | +9.4% to +10.9% | +22.7% / +24.9% | Splits same-line funcs & observes `eval` funcs; preserves `$?`; still requires AST for uncovered denominator (`FNF`) and still yields approximate `FNDA` | **Rejected** |
| **AST Function-Entry Probe Injection** | +41.1% (94.7 ms) | +17.6% (+25% lines) | +18.0% / +20.0% | 100% exact `FNDA` call counts, **but breaks caller `$?`** under `set -euo pipefail` (e.g., `(exit 42) \|\| on_err` observes `$?=0`) | **Rejected** |
| **`DEBUG` trap / `extdebug` + `declare -F`** | N/A | N/A | N/A | Conflicts directly with Bats (`trap ... DEBUG` / `EXIT`) and alters Bash execution semantics | **Rejected** |

### Key Reasons Alternatives Were Rejected

- **`PS4` `${FUNCNAME[0]}`**: Because unexecuted functions never appear in runtime traces, AST extraction is still mandatory for `FNF`. Paying a ~10% runtime/I/O tax and ~23% parser/memory overhead on every executed statement solely to cover rare same-line multi-function definitions and `eval`-defined functions was not justified—especially since `FNDA` remains approximate under `PS4`.
- **AST Function-Entry Probe Injection**: Injecting a command (`printf ... || :;`) at function entry clobbers the caller's `$?` before the first user statement executes. Under `set -e`, attempting to restore a non-zero `$?` triggers `errexit` itself, making safe `$?` preservation incompatible with simple statement injection. In addition, emitting both a probe xtrace line and a probe record per call caused a +41% slowdown on call-heavy workloads. *(This finding is also directly relevant when designing v2 scope-level instrumentation.)*

## Consequences

### Positive
- **Zero runtime overhead** and **zero trace bloat** compared to line-only coverage.
- **Zero risk of breaking target script semantics** (`$?`, `set -e`, `pipefail`, Bats traps).
- Minimal, maintainable code footprint isolated to AST extraction, trace aggregation, and report formatting.
- Preserves a clean upgrade path: adding runtime `PS4` disambiguation later would reuse the same AST and report data models.

### Trade-Offs & Mitigations
- **Multiple functions on one physical line** (e.g., `f1() { echo 1; }; f2() { echo 2; }`) share a line number and cannot be distinguished. *Mitigation*: document formatting with `shfmt` so each function occupies distinct lines.
- **Dynamically defined functions (`eval`)** are absent from the static AST and excluded from function coverage.
- **Approximate `FNDA` counts**: If a function's first executable line is a loop header (`for`/`while`), `FNDA` reflects loop iterations rather than entry count. Function coverage ratio (`FNH`/`FNF`) is unaffected.

## Revisit Triggers

Reconsider this decision if:
- Exact per-function invocation counts (`FNDA`) become a hard requirement over zero-overhead function coverage ratio (`FNH`/`FNF`).
- v2 introduces AST-based scope-level instrumentation with a proven `$?`- and `set -e`-safe function wrapper mechanism.
