"""Prepare the immutable GPL-3.0 XHS source and an isolated, frozen environment."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import urllib.request
import venv
import zipfile

ROOT = Path(__file__).resolve().parent


def prepare(source_only: bool = False) -> None:
    lock = json.loads((ROOT / "upstream.lock.json").read_text(encoding="utf-8"))
    source = ROOT / lock["source_directory"]
    archive = ROOT / "downloads" / "upstream.zip"
    archive.parent.mkdir(parents=True, exist_ok=True)
    if not archive.exists() or hashlib.sha256(archive.read_bytes()).hexdigest() != lock["archive_sha256"]:
        print(f"Downloading XHS-Downloader {lock['version']} @ {lock['commit']}", flush=True)
        req = urllib.request.Request(lock["archive_url"], headers={"User-Agent": "XHS-Workbench-Setup/1.0"})
        with urllib.request.urlopen(req, timeout=90) as response:
            data = response.read(32 * 1024 * 1024)
        if hashlib.sha256(data).hexdigest() != lock["archive_sha256"]:
            raise RuntimeError("Upstream archive checksum mismatch; no source was installed")
        archive.write_bytes(data)
    # Verify every source file against the immutable archive on every setup. This
    # avoids silently trusting an earlier edited source or changing dependency lock.
    with zipfile.ZipFile(archive) as bundle:
        for item in bundle.infolist():
            target = ROOT / "vendor" / item.filename
            if not target.resolve().is_relative_to((ROOT / "vendor").resolve()):
                raise RuntimeError("Unsafe path in upstream archive")
            if item.is_dir():
                target.mkdir(parents=True, exist_ok=True)
            else:
                data = bundle.read(item)
                target.parent.mkdir(parents=True, exist_ok=True)
                if not target.exists() or target.read_bytes() != data:
                    target.write_bytes(data)
    if not (source / "LICENSE").exists() or not (source / "uv.lock").exists():
        raise RuntimeError("Upstream source is missing its license or dependency lock")
    if source_only:
        return
    if sys.version_info < (3, 12):
        raise RuntimeError("Python 3.12 or newer is required; pass --source-only to prepare source only")
    env_path = ROOT / ".venv"
    interpreter = env_path / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
    if not interpreter.exists():
        venv.create(env_path, with_pip=True)
    # Restore pip if an interrupted previous environment synchronization removed
    # bootstrap packages before completing the dependency installation.
    subprocess.run([str(interpreter), "-m", "ensurepip", "--upgrade"], check=True)
    subprocess.run([str(interpreter), "-m", "pip", "install", f"uv=={lock['uv_version']}"], check=True)
    env = dict(os.environ, VIRTUAL_ENV=str(env_path), UV_PROJECT_ENVIRONMENT=str(env_path))
    # The upstream .python-version prefers 3.12. Explicitly retain the chosen
    # >=3.12 interpreter: replacing a running venv on Windows locks its Scripts
    # directory. --inexact retains the pinned uv bootstrap executable as well.
    subprocess.run([str(interpreter), "-m", "uv", "sync", "--directory", str(source), "--python", str(interpreter), "--frozen", "--no-install-project", "--active", "--inexact"], env=env, check=True)
    print("XHS service source and frozen Python dependencies are ready.", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-only", action="store_true")
    args = parser.parse_args()
    try:
        prepare(args.source_only)
    except Exception as error:
        print(f"Engine setup failed: {error}", file=sys.stderr)
        sys.exit(1)
