#!/usr/bin/env bash
set -euo pipefail

PRESET="${1:-smoke}"
WORKSPACE_DIR="${WORKSPACE_DIR:-/workspace}"
OUT_BASE="${COMPARE_OUT_DIR:-${WORKSPACE_DIR}/coverage/compare}"
OUT_DIR="${OUT_BASE}/${PRESET}"
WORK_BASE="/tmp/bashcoat_compare/${PRESET}"
TOOLS="${TOOLS:-bashcoat bashcov kcov}"

REPO_URL="${REPO_URL:-}"
REPO_REF="${REPO_REF:-}"
FIXTURE_DIR=""
TARGET_GLOBS="${TARGET_GLOBS:-}"
KCOV_INCLUDE="${KCOV_INCLUDE:-.}"
TEST_CMD="${TEST_CMD:-}"

case "${PRESET}" in
  smoke)
    FIXTURE_DIR="${WORKSPACE_DIR}/test/compare/fixtures"
    TARGET_GLOBS="${TARGET_GLOBS:-src/**/*.sh}"
    KCOV_INCLUDE="${KCOV_INCLUDE:-src}"
    TEST_CMD="${TEST_CMD:-bats test/}"
    ;;
  bats-core)
    REPO_URL="${REPO_URL:-https://github.com/bats-core/bats-core.git}"
    TARGET_GLOBS="${TARGET_GLOBS:-lib/bats-core/**/*.bash libexec/bats-core/*}"
    KCOV_INCLUDE="${KCOV_INCLUDE:-lib/bats-core,libexec/bats-core}"
    TEST_CMD="${TEST_CMD:-./bin/bats test/}"
    ;;
  pyenv)
    REPO_URL="${REPO_URL:-https://github.com/pyenv/pyenv.git}"
    TARGET_GLOBS="${TARGET_GLOBS:-libexec/pyenv*}"
    KCOV_INCLUDE="${KCOV_INCLUDE:-libexec}"
    TEST_CMD="${TEST_CMD:-bats test/}"
    ;;
  custom)
    if [[ -z "${REPO_URL}" && -z "${LOCAL_REPO:-}" ]]; then
      echo "error: custom preset requires REPO_URL or LOCAL_REPO" >&2
      exit 2
    fi
    if [[ -z "${TARGET_GLOBS}" || -z "${TEST_CMD}" ]]; then
      echo "error: custom preset requires TARGET_GLOBS and TEST_CMD" >&2
      exit 2
    fi
    ;;
  *)
    echo "Usage: $0 [smoke|bats-core|pyenv|custom]" >&2
    exit 2
    ;;
esac

rm -rf "${WORK_BASE}" "${OUT_DIR}"
mkdir -p "${WORK_BASE}/seed" "${OUT_DIR}"

if [[ -n "${FIXTURE_DIR}" ]]; then
  cp -a "${FIXTURE_DIR}/." "${WORK_BASE}/seed/"
elif [[ -n "${LOCAL_REPO:-}" ]]; then
  cp -a "${LOCAL_REPO}/." "${WORK_BASE}/seed/"
else
  echo "==> Cloning ${REPO_URL}..."
  if [[ -n "${REPO_REF}" ]]; then
    git clone --depth 1 --branch "${REPO_REF}" "${REPO_URL}" "${WORK_BASE}/seed"
  else
    git clone --depth 1 "${REPO_URL}" "${WORK_BASE}/seed"
  fi
fi

# Strip any pre-existing .simplecov so bashcov uses the harness formatter cleanly.
rm -f "${WORK_BASE}/seed/.simplecov"

META_FILE="${OUT_DIR}/meta.json"
python3 - <<'PY' "${META_FILE}" "${PRESET}"
import json, sys
with open(sys.argv[1], "w", encoding="utf-8") as f:
    json.dump({"preset": sys.argv[2], "work_roots": [], "tools": {}}, f, indent=2)
PY

run_and_record() {
  local tool="$1"
  local repo_dir="$2"
  shift 2
  python3 - <<'PY' "${META_FILE}" "${tool}" "${repo_dir}" "$@"
import json, subprocess, sys, time

meta_path, tool, repo_dir = sys.argv[1:4]
cmd = sys.argv[4:]

t0 = time.monotonic()
proc = subprocess.run(cmd, cwd=repo_dir)
dur = time.monotonic() - t0

with open(meta_path, "r", encoding="utf-8") as f:
    meta = json.load(f)

if repo_dir not in meta["work_roots"]:
    meta["work_roots"].append(repo_dir)
meta["tools"][tool] = {
    "status": "ok" if proc.returncode == 0 else "nonzero_exit",
    "exit_code": proc.returncode,
    "duration_sec": round(dur, 4),
}

with open(meta_path, "w", encoding="utf-8") as f:
    json.dump(meta, f, indent=2)
PY
}

read -r -a TARGET_ARR <<< "${TARGET_GLOBS}"
read -r -a CMD_ARR <<< "${TEST_CMD}"

for tool in ${TOOLS}; do
  tool_repo="${WORK_BASE}/${tool}"
  rm -rf "${tool_repo}"
  cp -a "${WORK_BASE}/seed" "${tool_repo}"

  case "${tool}" in
    bashcoat)
      echo "==> Running bashcoat..."
      bc_args=()
      for g in "${TARGET_ARR[@]}"; do
        bc_args+=(-t "${g}")
      done
      bc_args+=(-o "${OUT_DIR}/bashcoat.lcov" -summary=false --)
      bc_args+=("${CMD_ARR[@]}")
      run_and_record bashcoat "${tool_repo}" bashcoat "${bc_args[@]}"
      ;;
    bashcov)
      echo "==> Running bashcov..."
      cp "${WORKSPACE_DIR}/test/compare/simplecov_lcov.rb" "${tool_repo}/.simplecov"
      BASHCOV_LCOV_OUT="${OUT_DIR}/bashcov.lcov" \
        run_and_record bashcov "${tool_repo}" bashcov --root "${tool_repo}" --bash-path "$(command -v bash)" -- "${CMD_ARR[@]}"
      ;;
    kcov)
      echo "==> Running kcov..."
      IFS=',' read -r -a kcov_inc_arr <<< "${KCOV_INCLUDE}"
      inc_paths=()
      for p in "${kcov_inc_arr[@]}"; do
        if [[ "${p}" == /* ]]; then
          inc_paths+=("${p}")
        else
          inc_paths+=("${tool_repo}/${p}")
        fi
      done
      joined_inc="$(IFS=','; echo "${inc_paths[*]}")"
      mkdir -p "${OUT_DIR}/kcov_raw"
      run_and_record kcov "${tool_repo}" kcov --include-path="${joined_inc}" "${OUT_DIR}/kcov_raw" "${CMD_ARR[@]}"
      ;;
    *)
      echo "warning: unknown tool '${tool}' in TOOLS, skipping" >&2
      ;;
  esac
done

echo "==> Generating comparison report..."
python3 "${WORKSPACE_DIR}/test/compare/compare.py" --out-dir "${OUT_DIR}" --meta "${META_FILE}"
echo "==> Reports saved under ${OUT_DIR}/"
