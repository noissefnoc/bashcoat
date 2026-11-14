# Comparative Coverage Harness (`bashcoat` vs `bashcov` vs `kcov`)

Run `bashcoat`, `bashcov`, and `kcov` side-by-side inside an Ubuntu 26.04 container (via OrbStack / Docker Compose) on both built-in fixtures and real-world Bash repositories such as `pyenv` and `bats-core`.

## Prerequisites

- OrbStack (or Docker) with `docker compose`
- `SYS_PTRACE` capability (configured in [compose.yaml](compose.yaml) so `kcov` can run inside the container)

## Quick Start

1. Build the comparison container image:

   ```bash
   docker compose -f test/compare/compose.yaml build
   ```

2. Run the offline smoke comparison (uses [fixtures/](fixtures/)):

   ```bash
   docker compose -f test/compare/compose.yaml run --rm compare ./test/compare/run.sh smoke
   ```

3. Run against real-world Bash repositories:

   ```bash
   # pyenv
   docker compose -f test/compare/compose.yaml run --rm compare ./test/compare/run.sh pyenv

   # bats-core (pass TOOLS="bashcoat kcov" to skip bashcov on large suites)
   docker compose -f test/compare/compose.yaml run --rm -e TOOLS="bashcoat kcov" compare ./test/compare/run.sh bats-core
   ```

4. Run against any custom repository (e.g. a fork or `bash-core`-style repo):

   ```bash
   docker compose -f test/compare/compose.yaml run --rm \
     -e REPO_URL="https://github.com/<owner>/<repo>.git" \
     -e TARGET_GLOBS="src/**/*.sh" \
     -e KCOV_INCLUDE="src" \
     -e TEST_CMD="bats test/" \
     compare ./test/compare/run.sh custom
   ```

## Output

Reports are written under `coverage/compare/<preset>/` (gitignored):

- `bashcoat.lcov` — `bashcoat` LCOV output
- `bashcov.lcov` — `bashcov` LCOV output (via `simplecov-lcov`)
- `kcov_raw/` — raw `kcov` output (parsed via `cobertura.xml`)
- `comparison.md` / `comparison.json` — runtime, exit code, per-file coverage, and line-level discrepancies

## Interpreting Differences & Tool Trade-offs

Each tool uses a different mechanism to collect Bash coverage, so identical test suites naturally produce differences in executable line counts, hit counts, runtime, and test compatibility:

- **Executable line detection (AST vs. line scanning):** `bashcoat` parses scripts with `mvdan.cc/sh/v3/syntax` (AST) to determine executable lines, whereas `kcov` relies on line-based scanning. As a result, `kcov` may count lines inside multi-line quoted arguments (such as inline `sed` or `awk` scripts) as uncovered executable lines, or stop recognizing executable lines after complex quote escaping, while `bashcoat` may differ from `kcov`/`bashcov` on certain compound constructs.
- **Tracing mechanism (`PS4`/`BASH_XTRACEFD` vs. `DEBUG` trap):** `kcov` uses a `DEBUG` trap alongside `ptrace`/`LD_PRELOAD`, which can conflict with test frameworks or scripts that install their own `DEBUG` trap (for example, `bats-core` itself uses a `DEBUG` trap internally and tags some tests `no-kcov`). `bashcoat` avoids `trap DEBUG` by using `PS4` and `BASH_XTRACEFD`.
- **Scope & overhead (`-t` targeted instrumentation vs. global `BASH_ENV`):** `bashcov` enables tracing globally across every Bash process and subshell and aggregates traces in Ruby, which can add substantial overhead or interact with open-FD tests on large multi-process suites (such as `bats-core` with 500+ tests). `bashcoat` instruments only files matching `-t`, keeping runtime lower on suites with many non-target helper or test scripts.
- **Current `bashcoat` v1 limitations:** Because `bashcoat` injects `set -x`, `PS4`, `BASH_XTRACEFD`, and `__bashcoat_*` variables into instrumented files (and prepends a header line to files without a shebang), tests that strictly assert on variable namespaces, `set -x` / stderr output, or exact `LINENO` values in stack traces of shebang-less sourced files can fail under `bashcoat` (for example, 4 of 517 tests in `bats-core`'s own self-test suite). See the root [README](../../README.md) for the full list of v1 limitations.
