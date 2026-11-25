# bashcoat

Fast, lightweight C0 code coverage tool for **Bash 5.0+** scripts.

`bashcoat` wraps your target Bash scripts in lightweight instrumentation using Bash's built-in `BASH_XTRACEFD`, executes your test suite (e.g. Bats, shUnit2, or custom scripts), and outputs standard **LCOV (`lcov.info`)** or **Cobertura XML (`coverage.xml`)** reports alongside clean terminal summaries.

> [!NOTE]
> **Requirements**: `bashcoat` requires **Bash 5.0 or newer** at runtime. On macOS, the system `/bin/bash` is Bash 3.2; install modern Bash via Homebrew (`brew install bash`) and ensure it precedes `/bin/bash` in `PATH` or your script shebang (`#!/usr/bin/env bash`). Scripts executed under Bash `< 5.0` will exit early with a clear error.

> [!IMPORTANT]
> **In-Place Instrumentation**: `bashcoat` temporarily instruments matching target scripts in place (creating `.bashcoat.bak` backups and restoring them on exit). While `SIGINT`/`SIGTERM` are handled gracefully, a hard `SIGKILL` or crash can leave `.bashcoat.bak` files behind, so running against a clean Git working tree is recommended.

---

## Why bashcoat?

For years, measuring code coverage in Bash has required [kcov](https://github.com/SimonKagstrom/kcov) or [bashcov](https://github.com/infertux/bashcov). While useful, both present significant friction in modern development:

| Feature | `kcov` | `bashcov` | **`bashcoat`** |
| :--- | :---: | :---: | :---: |
| **macOS Native Support** | ❌ (ptrace / SIP issues) | ✅ | ✅ **100% native** |
| **Unprivileged Docker / CI** | ❌ (requires `SYS_PTRACE`) | ✅ | ✅ **No special caps** |
| **Runtime Dependencies** | C++ compiler, elf/dwarf libs | Ruby runtime & gems | **Zero (single binary)** |
| **AST-Precise Line Detection** | DWARF debug symbols | Regex | **Go Bash AST parser** |
| **Output Format** | Cobertura / HTML | SimpleCov / HTML | **LCOV, Cobertura XML & Table** |

---

## Installation

### Prebuilt Binaries (GitHub Releases)

Download the archive for your OS/architecture from [GitHub Releases](https://github.com/noissefnoc/bashcoat/releases) and extract `bashcoat` into a directory on your `PATH`:

```bash
# Example: Linux amd64
curl -fsSL https://github.com/noissefnoc/bashcoat/releases/latest/download/bashcoat_0.1.0_linux_amd64.tar.gz \
  | tar -xz -C /usr/local/bin bashcoat
```

> [!NOTE]
> On macOS, if you download the `.tar.gz` archive via a web browser, Gatekeeper attaches a quarantine attribute. Remove it before running:
> ```bash
> xattr -d com.apple.quarantine ./bashcoat
> ```

### With Go (1.27+)

```bash
go install github.com/noissefnoc/bashcoat/cmd/bashcoat@latest
```

### With mise

```toml
# .mise.toml (prebuilt GitHub Release binary)
[tools]
"github:noissefnoc/bashcoat" = "latest"

# Or build from source via Go
# "go:github.com/noissefnoc/bashcoat/cmd/bashcoat" = "latest"
```

---

## Quick Start

### Basic Usage with Bats

Measure coverage of `examples/src/deploy.sh` while running its Bats test suite:

```bash
bashcoat -t examples/src/deploy.sh -- bats examples/test/deploy.bats
```

Output:

```text
File                   | Lines | Covered | Coverage
-----------------------+-------+---------+---------
examples/src/deploy.sh |    13 |      11 |    84.6%
```

A standard `lcov.info` file is automatically generated in your working directory.

### Multiple Targets & Globs

Measure coverage across multiple files:

```bash
bashcoat -t 'examples/src/**/*.sh' -o coverage.lcov -- bats examples/test/
```

### With Any Test Runner or Custom Script

`bashcoat` works with any command following `--`:

```bash
# Standalone test script
bashcoat -t examples/src/lib/utils.sh -- ./examples/test_utils.sh

# Direct script execution / custom runner
bashcoat -t examples/src/deploy.sh -- ./examples/src/deploy.sh staging
```

---

## CLI Options

```text
Usage:
  bashcoat [options] -t <script.sh> -- <test_command> [args...]

Options:
  -t, -target <path/glob>   Target script path or glob pattern to measure (repeatable)
  -f, -format <format>      Report output format: lcov or cobertura (default: "lcov")
  -o, -output <path>        Report output file path (default: "lcov.info" for lcov, "coverage.xml" for cobertura; "" to disable)
  -summary                  Print coverage summary table to stdout (default: true)
  -q, -quiet                Suppress summary table and diagnostic logs
  -v, -version              Print version information
```

---

## CI Integration (GitHub Actions + Codecov)

```yaml
name: Test & Coverage

on: [push, pull_request]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7

      - name: Install Bats
        run: sudo apt-get update && sudo apt-get install -y bats

      - name: Set up Go
        uses: actions/setup-go@v6
        with:
          go-version: '1.27'

      - name: Install bashcoat
        run: go install github.com/noissefnoc/bashcoat/cmd/bashcoat@latest

      - name: Run tests with coverage
        run: bashcoat -t 'src/*.sh' -o lcov.info -- bats test/

      - name: Upload to Codecov
        uses: codecov/codecov-action@v6
        with:
          files: lcov.info
          token: ${{ secrets.CODECOV_TOKEN }}
```

> [!NOTE]
> For readability, the example above uses major version tags (e.g. `@v6` / `@v7`). In production workflows, pin third-party actions to full commit SHAs (e.g., via a GitHub Actions lockfile or tools like `pinact`) to harden supply-chain security.

---

## Report Formats & Integrations

`bashcoat` natively outputs **LCOV (`lcov.info`)** and **Cobertura XML (`coverage.xml`)**:

- **GitHub Code Quality (`actions/upload-code-coverage`)**: Pass `-f cobertura` to output Cobertura XML directly (no external converter needed). In GitHub Actions (with `code-quality: write` permission on the job):
  ```yaml
  - name: Run tests with coverage
    run: bashcoat -t 'src/**/*.sh' -f cobertura -o coverage.xml -- bats test/

  - name: Upload to GitHub Code Quality
    uses: actions/upload-code-coverage@v1
    with:
      file: coverage.xml
      language: Shell
      label: code-coverage/bashcoat
  ```
- **HTML report (`genhtml` from `lcov`)**:
  ```bash
  # macOS: brew install lcov  /  Ubuntu: sudo apt-get install lcov
  genhtml lcov.info -o coverage-html
  ```
- **Editor / IDE**: Extensions such as VS Code's [Coverage Gutters](https://marketplace.visualstudio.com/items?itemName=ryanluker.vscode-coverage-gutters) read `lcov.info` directly to highlight covered and uncovered lines inline.
- **Other CI services**: Services such as Codecov, Coveralls, SonarQube, Jenkins, and GitLab accept LCOV and/or Cobertura XML natively.

---

## How It Works

1. **AST-Precise Statement Identification**: Uses `mvdan.cc/sh/v3/syntax` to walk the Bash Abstract Syntax Tree, distinguishing executable statements (including `(( ... ))`, `[[ ... ]]`, `let`, and multi-line constructs) from comments, blank lines, function declarations, and syntax boundaries.
2. **Single-Line Preamble Injection**: Temporarily injects an unobtrusive single-line `BASH_XTRACEFD` probe after UTF-8 BOM, shebang, and header comments, validating and allocating a trace file descriptor per process.
3. **Safe Subprocess Execution**: Runs your test command in an isolated process group, piping inputs and exit codes transparently.
4. **Defensive In-Place Restoration**: Protects target files via per-file advisory locking, atomic writes, preserved file permissions, multi-signal handling (`SIGINT`/`SIGTERM`/`SIGHUP`/`SIGQUIT`), and `.bashcoat.bak` backup files (detecting leftover backups or instrumentation markers on startup; note that uncatchable signals like `SIGKILL` or OOM require manual restoration from `.bashcoat.bak`).
5. **Report Synthesis**: Computes execution counts per line in a single pass and outputs industry-standard LCOV (`lcov.info`) or Cobertura XML (`coverage.xml`) compatible with CI coverage reporters.

---

## Known Limitations (v1)

- **Global `set -x` / `PS4` / `BASH_XTRACEFD` Side Effects**: Because v1 instruments at the file level via `BASH_XTRACEFD` and `set -x`, `source`-ing an instrumented library enables `set -x` (affecting `$-`) in the calling Bash process and test runners such as Bats (though non-target files are never attributed into coverage results). Conversely, if tested code executes `set +x` or overrides `PS4` / `BASH_XTRACEFD`, subsequent lines in that shell will stop recording coverage.
- **Bash 5.0+ Only**: Bash 3.2/4.x are intentionally unsupported and exit immediately with an error.
- **Summary Output Stream**: By default, `-summary` prints the table to `stdout`; pass `-summary=false` or `-q` if your test command's `stdout` is piped or asserted strictly.
- **Multi-Line `eval` `LINENO` Offset**: In Bash, executing a multi-line string via `eval` advances `LINENO` from the `eval` call line, so lines inside the evaluated string may be attributed to subsequent line numbers in the enclosing script.
- **Reserved Trace Delimiter**: Target script paths containing the literal sequence `|bc_end|` are unsupported because `|bc_end|` is used as the internal `PS4` field delimiter.

---

## Roadmap (v2)

- **Function / Scope-Level Instrumentation**: Isolate coverage tracing to target functions/scopes (via AST transformation with line-number mapping or `$?`-preserving scope wrappers) so that `source`-ing an instrumented library does not leak `set -x` / `$-` into callers or test runners.

---

## Comparative Testing (`bashcoat` vs `bashcov` vs `kcov`)

A Docker/OrbStack-based comparison harness is available under [test/compare/](test/compare/) to run `bashcoat`, `bashcov`, and `kcov` side-by-side against built-in fixtures or real-world Bash repositories (`pyenv`, `bats-core`, or any custom repository):

```bash
docker compose -f test/compare/compose.yaml build
docker compose -f test/compare/compose.yaml run --rm compare ./test/compare/run.sh smoke
docker compose -f test/compare/compose.yaml run --rm compare ./test/compare/run.sh pyenv
docker compose -f test/compare/compose.yaml run --rm compare ./test/compare/run.sh bats-core
```

See [test/compare/README.md](test/compare/README.md) for custom repository configuration and output report details (`coverage/compare/<preset>/comparison.md`).

---

## Contributing & Maintenance

`bashcoat` is a personal project maintained on a best-effort basis. Bug reports, questions, and feature suggestions are welcome via GitHub Issues, though response times may vary. Pull requests for new features or design changes are not accepted at this time—please open an Issue to share ideas or feedback instead.

---

## License

MIT © [noissefnoc](https://github.com/noissefnoc)
