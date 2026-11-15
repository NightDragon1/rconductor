#!/usr/bin/env python3
# SPDX-FileCopyrightText: 2026 NightDragon · ShadowGhost.NET
# SPDX-License-Identifier: AGPL-3.0-or-later
#
# Lists what a change does to the lockfiles (PRJ-005, SEC-013), for the
# pull request text: packages that are new with name@version and origin,
# and entries whose origin or checksum is not one the base had for the same
# name@version. The lockfiles are the known ones plus every changed
# go.sum and package-lock.json. A lockfile it cannot read fails the run.
# Usage: scripts/lockfile-packages.py <base-ref> [<head-ref>]
# The base is the merge base, e.g. $(git merge-base origin/main HEAD).
import json
import os
import subprocess
import sys

KNOWN = ["web/package-lock.json", "go.sum", "tools/go.sum"]
NPM_NAME = "package-lock.json"
GOSUM_NAME = "go.sum"


def git(*args):
    return subprocess.run(["git", *args], capture_output=True, text=True)


def show(ref, path):
    """The file at ref; a file missing there counts as empty."""
    r = git("show", f"{ref}:{path}")
    return r.stdout if r.returncode == 0 else ""


def check_ref(ref):
    if git("rev-parse", "--verify", "--quiet", f"{ref}^{{commit}}").returncode != 0:
        sys.exit(f"unknown ref: {ref}")


def lockfiles(base, head):
    """The known lockfiles and every changed one, in a fixed order."""
    r = git("diff", "--name-only", "--no-renames", base, head)
    if r.returncode != 0:
        sys.exit(f"git diff failed: {r.stderr.strip()}")
    changed = [
        p for p in r.stdout.splitlines() if os.path.basename(p) in (NPM_NAME, GOSUM_NAME)
    ]
    return KNOWN + sorted(set(changed) - set(KNOWN))


def npm(ref, path):
    """key -> (label, name, version, resolved, integrity); fails closed."""
    text = show(ref, path)
    if not text.strip():
        return {}
    try:
        doc = json.loads(text)
    except json.JSONDecodeError as e:
        sys.exit(f"{path} at {ref}: not JSON ({e})")
    pkgs = doc.get("packages") if isinstance(doc, dict) else None
    version = doc.get("lockfileVersion", 0) if isinstance(doc, dict) else 0
    if not isinstance(pkgs, dict) or not isinstance(version, int) or version < 2:
        sys.exit(f"{path} at {ref}: no lockfileVersion 2 or 3 with packages")
    out = {}
    for key, p in pkgs.items():
        if "node_modules/" not in key:
            continue  # the root project and workspace folders
        alias = key.rsplit("node_modules/", 1)[1]
        name = p.get("name") or alias
        label = alias if name == alias else f"{alias} (alias of {name})"
        out[key] = (label, name, p.get("version", "?"), p.get("resolved", "?"), p.get("integrity", "?"))
    return out


def gosum(ref, path):
    """(module, version) -> hash; versions of /go.mod lines keep the suffix."""
    out = {}
    for line in show(ref, path).splitlines():
        parts = line.split()
        if len(parts) == 3:
            out[(parts[0], parts[1])] = parts[2]
    return out


def section(title, lines):
    print(f"{title}:")
    lines = sorted(set(lines))
    for line in lines:
        print(f"- {line}")
    if not lines:
        print("none")


def report_npm(path, base, head):
    old, new = npm(base, path), npm(head, path)
    # Every origin the base had for a name@version, under any key.
    seen = {}
    for _, name, ver, res, integ in old.values():
        seen.setdefault((name, ver), set()).add((res, integ))
    added, changed = [], []
    for key, (label, name, ver, res, integ) in new.items():
        origins = seen.get((name, ver))
        if origins is None:
            added.append(f"{label}@{ver} ({res})")
            continue
        if (res, integ) in origins:
            continue  # the same package as somewhere in the base
        prev = old.get(key)
        if prev and prev[1:3] == (name, ver):
            for field, was, now in (("resolved", prev[3], res), ("integrity", prev[4], integ)):
                if was != now:
                    changed.append(f"{label}@{ver} ({key}) {field}: {was} -> {now}")
        else:
            changed.append(f"{label}@{ver} ({key}) origin not in the base: resolved {res}, integrity {integ}")
    print(f"## {path}")
    section("New packages", added)
    section("Changed origin or checksum", changed)


def report_gosum(path, base, head):
    old, new = gosum(base, path), gosum(head, path)
    added = [
        f"{mod}@{ver} (proxy.golang.org, {mod})"
        for mod, ver in new
        if not ver.endswith("/go.mod") and (mod, ver) not in old
    ]
    changed = [
        f"{mod}@{ver} hash: {old[(mod, ver)]} -> {new[(mod, ver)]}"
        for mod, ver in set(old) & set(new)
        if old[(mod, ver)] != new[(mod, ver)]
    ]
    print(f"## {path}")
    section("New packages", added)
    section("Changed origin or checksum", changed)


def main():
    if len(sys.argv) not in (2, 3):
        sys.exit("usage: scripts/lockfile-packages.py <base-ref> [<head-ref>]")
    base = sys.argv[1]
    head = sys.argv[2] if len(sys.argv) == 3 else "HEAD"
    os.chdir(os.path.join(os.path.dirname(os.path.abspath(__file__)), ".."))
    check_ref(base)
    check_ref(head)
    for i, path in enumerate(lockfiles(base, head)):
        if i:
            print()
        if os.path.basename(path) == NPM_NAME:
            report_npm(path, base, head)
        else:
            report_gosum(path, base, head)


if __name__ == "__main__":
    main()
