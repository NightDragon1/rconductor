#!/usr/bin/env python3
# SPDX-License-Identifier: AGPL-3.0-or-later
"""Create the GitHub milestones and issues of R1 from docs/ (Plan R1, 3).

One milestone per row of docs/07-plan-r1.md and one issue per requirement,
titled "<ID> <short title>" with description, acceptance criterion and test
type from docs/01 and docs/05. Existing milestones and issues (same title)
are skipped, so the script can run again.

Usage:
  scripts/gh_plan.py                     print the plan, change nothing
  scripts/gh_plan.py --apply OWNER/REPO  create what is missing (needs gh)
"""
import json
import os
import re
import subprocess
import sys

root = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
apply_repo = sys.argv[2] if len(sys.argv) > 2 and sys.argv[1] == "--apply" else None


def rows(path):
    for line in open(f"{root}/docs/{path}", encoding="utf-8"):
        if line.startswith("| ") and not line.startswith("| ---"):
            yield [c.strip() for c in line.strip().strip("|").split(" | ")]


def unescape(s):
    return s.replace("\\_", "_").replace("\\<", "<").replace("\\*", "*")


reqs = {}
for c in rows("01-gesamtkonzept.md"):
    if len(c) == 7 and re.fullmatch(r"[A-Z]+-\d{3}", c[0]):
        reqs[c[0]] = dict(id=c[0], title=unescape(c[1]), desc=unescape(c[2]), comp=c[3], prio=c[4], target=c[5], rev=c[6])

accept = {}
for c in rows("05-abnahme-r1.md"):
    if len(c) == 3 and re.fullmatch(r"[A-Z]+-\d{3}", c[0]):
        accept[c[0]] = (unescape(c[1]), c[2])

milestones = []
for c in rows("07-plan-r1.md"):
    m = re.match(r"(M\d) (.+)", c[0])
    if not m:
        continue
    ids = []
    for part in c[2].split(", "):
        r = re.fullmatch(r"([A-Z]+)-(\d{3}) bis \1-(\d{3})", part)
        if r:
            ids += [f"{r[1]}-{n:03d}" for n in range(int(r[2]), int(r[3]) + 1)]
        else:
            ids.append(part)
    milestones.append(dict(key=m[1], title=f"{m[1]} {m[2]}", desc=f"{c[1]}. Setzt voraus: {c[3]}.", ids=ids))

notes = {
    "NFR-001": "Teilweise in M0 (Web-Port, Konfiguration per Env); Abschluss mit dem Agent-Port 8443 in M3 (Entscheidung A.1).",
    "NFR-016": "Mechanismus und Unit-Tests in M0; Abnahme mit dem Testlauf aller Ereigniscodes in M5 (Entscheidung A.2).",
    "SEC-012": "Mechanismus und Unit-Tests in M0; Abnahme mit dem Testlauf in M5 (Entscheidung A.2).",
}

issues = []
seen = set()
for m in milestones:
    for rid in m["ids"]:
        r = reqs[rid]
        assert r["target"] == "R1", rid
        assert rid in accept, rid
        assert rid not in seen, rid
        seen.add(rid)
        crit, kind = accept[rid]
        body = (
            f"**{rid}** (Rev {r['rev']}): {r['desc']}\n\n"
            f"Komponente: {r['comp']} · Priorität: {r['prio']} · Target: {r['target']}\n\n"
            f"### Abnahmekriterium\n\n{crit}\n\nPrüfart: {kind}\n\n"
        )
        if rid in notes:
            body += f"### Hinweis\n\n{notes[rid]}\n\n"
        body += (
            "### Fundstellen\n\n"
            "- docs/01-gesamtkonzept.md, Kapitel 2\n"
            "- docs/05-abnahme-r1.md\n"
            f"- docs/07-plan-r1.md, {m['title']}\n"
        )
        issues.append(dict(title=f"{rid} {r['title']}", milestone=m["title"], body=body))

r1 = {k for k, v in reqs.items() if v["target"] == "R1"}
missing = sorted(r1 - seen)

if not apply_repo:
    print(json.dumps(dict(
        milestones=[(m["title"], len(m["ids"])) for m in milestones],
        issues=len(issues), r1=len(r1), missing=missing,
        sample=issues[0],
    ), ensure_ascii=False, indent=1))
    sys.exit(0)


def gh(*args, input=None):
    return subprocess.run(["gh", *args], check=True, capture_output=True, text=True, input=input).stdout


existing_ms = {m["title"] for m in json.loads(gh("api", f"repos/{apply_repo}/milestones?state=all&per_page=100"))}
for m in milestones:
    if m["title"] not in existing_ms:
        gh("api", f"repos/{apply_repo}/milestones", "-f", f"title={m['title']}", "-f", f"description={m['desc']}")
        print("milestone", m["title"])

existing = set(json.loads(gh("issue", "list", "-R", apply_repo, "--state", "all", "--limit", "500", "--json", "title", "--jq", "[.[].title]")))
for i in issues:
    if i["title"] in existing:
        continue
    gh("issue", "create", "-R", apply_repo, "--title", i["title"], "--milestone", i["milestone"], "--body-file", "-", input=i["body"])
    print("issue", i["title"])
