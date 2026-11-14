#!/usr/bin/env python3
"""Compare coverage outputs from bashcoat (LCOV), bashcov (LCOV), and kcov (Cobertura XML)."""

from __future__ import annotations

import argparse
import glob
import json
import os
import posixpath
import sys
import xml.etree.ElementTree as ET
from pathlib import Path
from typing import Dict, List, Optional, Set, Tuple


FileHits = Dict[int, int]
ToolProfiles = Dict[str, FileHits]


def normalize_path(raw: str, work_roots: List[str]) -> str:
    p = raw.replace("\\", "/")
    for root in work_roots:
        r = root.rstrip("/")
        if r and (p == r or p.startswith(r + "/")):
            p = p[len(r) + 1 :]
            break
    while p.startswith("./"):
        p = p[2:]
    p = posixpath.normpath(p)
    if p == ".":
        return ""
    return p


def parse_lcov(path: Path, work_roots: List[str]) -> ToolProfiles:
    profiles: ToolProfiles = {}
    if not path.is_file():
        return profiles

    current_file: Optional[str] = None
    current_hits: FileHits = {}

    for line in path.read_text(encoding="utf-8", errors="replace").splitlines():
        if line.startswith("SF:"):
            current_file = normalize_path(line[3:].strip(), work_roots)
            current_hits = profiles.setdefault(current_file, {})
        elif line.startswith("DA:") and current_file is not None:
            parts = line[3:].split(",", 2)
            if len(parts) >= 2:
                try:
                    lineno = int(parts[0].strip())
                    hits = int(parts[1].strip())
                    current_hits[lineno] = current_hits.get(lineno, 0) + hits
                except ValueError:
                    pass
        elif line == "end_of_record":
            current_file = None
            current_hits = {}

    return profiles


def parse_kcov_cobertura(kcov_dir: Path, work_roots: List[str]) -> ToolProfiles:
    profiles: ToolProfiles = {}
    if not kcov_dir.is_dir():
        return profiles

    candidates = sorted(
        kcov_dir.glob("**/cobertura.xml"),
        key=lambda p: (0 if "kcov-merged" in p.parts else 1, len(p.parts)),
    )
    if not candidates:
        return profiles

    xml_path = candidates[0]
    try:
        tree = ET.parse(xml_path)
    except ET.ParseError as exc:
        print(f"warning: failed to parse {xml_path}: {exc}", file=sys.stderr)
        return profiles

    root = tree.getroot()
    source_roots = [s.text.strip() for s in root.findall("./sources/source") if s.text]

    for cls in root.findall(".//class"):
        filename = cls.attrib.get("filename", "")
        if not filename:
            continue
        candidate = filename
        if not filename.startswith("/") and source_roots:
            # Cobertura filename is relative to <sources><source>...</source></sources>
            for sr in source_roots:
                joined = posixpath.join(sr.rstrip("/"), filename)
                if normalize_path(joined, work_roots) != posixpath.normpath(joined):
                    candidate = joined
                    break
            else:
                candidate = posixpath.join(source_roots[0].rstrip("/"), filename)
        norm = normalize_path(candidate, work_roots)
        if not norm:
            continue
        hits_map = profiles.setdefault(norm, {})
        for line_el in cls.findall("./lines/line"):
            try:
                lineno = int(line_el.attrib["number"])
                hits = int(line_el.attrib["hits"])
            except (KeyError, ValueError):
                continue
            if lineno not in hits_map or hits > hits_map[lineno]:
                hits_map[lineno] = hits

    return profiles


def summarize_file(hits: Optional[FileHits]) -> Tuple[int, int, str]:
    if hits is None:
        return 0, 0, "-"
    found = len(hits)
    covered = sum(1 for c in hits.values() if c > 0)
    if found == 0:
        return 0, 0, "N/A"
    return found, covered, f"{(covered / found) * 100:.1f}%"


def diff_lines(a: Optional[FileHits], b: Optional[FileHits]) -> Dict[str, List[int]]:
    if a is None or b is None:
        return {}
    a_exec = set(a.keys())
    b_exec = set(b.keys())
    a_hit = {ln for ln, c in a.items() if c > 0}
    b_hit = {ln for ln, c in b.items() if c > 0}
    return {
        "exec_only_a": sorted(a_exec - b_exec),
        "exec_only_b": sorted(b_exec - a_exec),
        "hit_only_a": sorted((a_hit - b_hit) & (a_exec & b_exec)),
        "hit_only_b": sorted((b_hit - a_hit) & (a_exec & b_exec)),
    }


def main() -> int:
    parser = argparse.ArgumentParser(description="Compare bashcoat, bashcov, and kcov results")
    parser.add_argument("--out-dir", required=True, help="Directory holding tool outputs")
    parser.add_argument("--meta", required=True, help="JSON file with timing/exit codes and work roots")
    args = parser.parse_args()

    out_dir = Path(args.out_dir)
    meta = json.loads(Path(args.meta).read_text(encoding="utf-8"))
    work_roots = meta.get("work_roots", [])

    tool_data: Dict[str, ToolProfiles] = {
        "bashcoat": parse_lcov(out_dir / "bashcoat.lcov", work_roots),
        "bashcov": parse_lcov(out_dir / "bashcov.lcov", work_roots),
        "kcov": parse_kcov_cobertura(out_dir / "kcov_raw", work_roots),
    }

    # Align comparison to bashcoat's target files when available, otherwise union.
    target_files: List[str]
    if tool_data["bashcoat"]:
        target_files = sorted(tool_data["bashcoat"].keys())
    else:
        all_files: Set[str] = set()
        for profs in tool_data.values():
            all_files.update(profs.keys())
        target_files = sorted(all_files)

    lines_md: List[str] = []
    lines_md.append(f"# Coverage Comparison Report ({meta.get('preset', 'custom')})")
    lines_md.append("")
    lines_md.append("## Runtime & Exit Codes")
    lines_md.append("")
    lines_md.append("| Tool | Status | Exit Code | Duration (s) |")
    lines_md.append("| :--- | :--- | ---: | ---: |")
    for tool in ("bashcoat", "bashcov", "kcov"):
        tmeta = meta.get("tools", {}).get(tool, {})
        status = tmeta.get("status", "skipped")
        rc = tmeta.get("exit_code", "-")
        dur = tmeta.get("duration_sec", "-")
        dur_str = f"{dur:.3f}" if isinstance(dur, (int, float)) else str(dur)
        lines_md.append(f"| `{tool}` | {status} | {rc} | {dur_str} |")

    lines_md.append("")
    lines_md.append("## Per-File Coverage Comparison")
    lines_md.append("")
    lines_md.append("| File | bashcoat | bashcov | kcov |")
    lines_md.append("| :--- | ---: | ---: | ---: |")

    totals: Dict[str, Tuple[int, int]] = {t: (0, 0) for t in ("bashcoat", "bashcov", "kcov")}
    file_diffs: Dict[str, dict] = {}

    for f in target_files:
        cells = []
        for tool in ("bashcoat", "bashcov", "kcov"):
            hits = tool_data[tool].get(f)
            found, hit, pct = summarize_file(hits)
            if hits is not None:
                tf, th = totals[tool]
                totals[tool] = (tf + found, th + hit)
                cells.append(f"{hit}/{found} ({pct})")
            else:
                cells.append("-")
        lines_md.append(f"| `{f}` | {cells[0]} | {cells[1]} | {cells[2]} |")

        file_diffs[f] = {
            "vs_bashcov": diff_lines(tool_data["bashcoat"].get(f), tool_data["bashcov"].get(f)),
            "vs_kcov": diff_lines(tool_data["bashcoat"].get(f), tool_data["kcov"].get(f)),
        }

    total_cells = []
    for tool in ("bashcoat", "bashcov", "kcov"):
        tf, th = totals[tool]
        if tf == 0 and not tool_data[tool]:
            total_cells.append("-")
        elif tf == 0:
            total_cells.append("0/0 (N/A)")
        else:
            total_cells.append(f"{th}/{tf} ({(th / tf) * 100:.1f}%)")
    lines_md.append(f"| **Total** | **{total_cells[0]}** | **{total_cells[1]}** | **{total_cells[2]}** |")

    lines_md.append("")
    lines_md.append("## Line-Level Discrepancies vs bashcoat")
    lines_md.append("")
    any_diff = False
    for f in target_files:
        d = file_diffs[f]
        notes = []
        for other_label, key in (("bashcov", "vs_bashcov"), ("kcov", "vs_kcov")):
            sub = d.get(key, {})
            if not sub:
                continue
            parts = []
            if sub.get("exec_only_a"):
                parts.append(f"exec only in bashcoat={sub['exec_only_a']}")
            if sub.get("exec_only_b"):
                parts.append(f"exec only in {other_label}={sub['exec_only_b']}")
            if sub.get("hit_only_a"):
                parts.append(f"hit only in bashcoat={sub['hit_only_a']}")
            if sub.get("hit_only_b"):
                parts.append(f"hit only in {other_label}={sub['hit_only_b']}")
            if parts:
                notes.append(f"- vs `{other_label}`: " + "; ".join(parts))
        if notes:
            any_diff = True
            lines_md.append(f"### `{f}`")
            lines_md.extend(notes)
            lines_md.append("")
    if not any_diff:
        lines_md.append("No line-level discrepancies across comparable files.")

    report_md = "\n".join(lines_md) + "\n"
    (out_dir / "comparison.md").write_text(report_md, encoding="utf-8")
    (out_dir / "comparison.json").write_text(
        json.dumps(
            {
                "meta": meta,
                "totals": {k: {"found": v[0], "hit": v[1]} for k, v in totals.items()},
                "diffs": file_diffs,
            },
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )

    print(report_md)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
