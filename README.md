# bashcoat

Fast, zero-dependency C0 code coverage tool for Bash scripts.

`bashcoat` wraps your target Bash scripts in lightweight instrumentation using Bash's built-in `BASH_XTRACEFD`, executes your test suite (e.g. Bats, shUnit2, or custom scripts), and outputs standard **LCOV (`lcov.info`)** reports and clean terminal summaries.

---

## Why bashcoat?

For years, measuring code coverage in Bash has required [kcov](https://github.com/SimonKagstrom/kcov) or [bashcov](https://github.com/infertux/bashcov). While useful, both present significant friction in modern development:

| Feature | `kcov` | `bashcov` | **`bashcoat`** |
| :--- | :---: | :---: | :---: |
| **macOS Native Support** | ❌ (ptrace / SIP issues) | ✅ | ✅ **100% native** |
| **Unprivileged Docker / CI** | ❌ (requires `SYS_PTRACE`) | ✅ | ✅ **No special caps** |
| **Runtime Dependencies** | C++ compiler, elf/dwarf libs | Ruby runtime & gems | **Zero (single binary)** |
| **AST-Precise Line Detection** | DWARF debug symbols | Regex | **Go Bash AST parser** |
| **Output Format** | Cobertura / HTML | SimpleCov / HTML | **Standard LCOV & Table** |

---

## Installation

### With Go (1.24+)

```bash
go install github.com/noissefnoc/bashcoat/cmd/bashcoat@latest
```

### With mise

```toml
# .mise.toml
[tools]
"go:github.com/noissefnoc/bashcoat/cmd/bashcoat" = "latest"
```

---

## Quick Start

### Basic Usage with Bats

Measure coverage of `src/deploy.sh` while running its Bats test suite:

```bash
bashcoat -t src/deploy.sh -- bats test/deploy.bats
```

Output:

```text
File          | Lines | Covered | Coverage
--------------+-------+---------+---------
src/deploy.sh |    42 |      39 |    92.9%
```

A standard `lcov.info` file is automatically generated in your working directory.

### Multiple Targets & Globs

Measure coverage across multiple files:

```bash
bashcoat -t 'src/**/*.sh' -o coverage.lcov -- bats test/
```

### With Any Test Runner or Custom Script

`bashcoat` works with any command following `--`:

```bash
# shunit2
bashcoat -t lib/utils.sh -- ./test_utils.sh

# Python / custom runner
bashcoat -t bin/tool.sh -- pytest tests/
```

---

## CLI Options

```text
Usage:
  bashcoat [options] -t <script.sh> -- <test_command> [args...]

Options:
  -t, -target <path/glob>   Target script path or glob pattern to measure (repeatable)
  -o, -output <path>        LCOV report output file path (default: "lcov.info", "" to disable)
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
      - uses: actions/checkout@v4

      - name: Install Bats
        run: sudo apt-get update && sudo apt-get install -y bats

      - name: Set up Go
        uses: actions/setup-go@v5
        with:
          go-version: '1.24'

      - name: Install bashcoat
        run: go install github.com/noissefnoc/bashcoat/cmd/bashcoat@latest

      - name: Run tests with coverage
        run: bashcoat -t 'src/*.sh' -o lcov.info -- bats test/

      - name: Upload to Codecov
        uses: codecov/codecov-action@v4
        with:
          files: lcov.info
          token: ${{ secrets.CODECOV_TOKEN }}
```

---

## How It Works

1. **AST-Precise Statement Identification**: Uses `mvdan.cc/sh/v3/syntax` to walk the Bash Abstract Syntax Tree, distinguishing executable statements from comments, blank lines, and syntax wrappers.
2. **Zero-Overhead Preamble Injection**: Temporarily injects an unobtrusive single-line `BASH_XTRACEFD` probe after the shebang and header comments, allocating a dynamic file descriptor to trace executed lines.
3. **Safe Subprocess Execution**: Runs your test command unaltered, piping inputs and exit codes transparently.
4. **Guaranteed Restoration**: Automatically restores all target scripts to their exact original byte contents via `defer`, ensuring no residue remains even if tests fail or are interrupted.
5. **LCOV Synthesis**: Computes execution counts per line and outputs industry-standard `lcov.info` compatible with all major CI coverage reporters.

---

## License

MIT © [noissefnoc](https://github.com/noissefnoc)
