"""Verify the pinned Spider_XHS source and install its isolated publisher runtime."""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import venv

ROOT = Path(__file__).resolve().parent


def run(arguments: list[str]) -> None:
    options = {"creationflags": subprocess.CREATE_NO_WINDOW} if os.name == "nt" else {}
    subprocess.run(arguments, check=True, **options)


def prepare(source_only: bool = False) -> None:
    lock = json.loads((ROOT / "upstream.lock.json").read_text(encoding="utf-8"))
    source = ROOT / "vendor" / "Spider_XHS"
    files = lock.get("files")
    if not isinstance(files, dict) or not files:
        raise RuntimeError("Publisher source checksum manifest is missing")
    for relative, expected in files.items():
        target = (source / relative).resolve()
        if not target.is_relative_to(source.resolve()) or not target.is_file():
            raise RuntimeError("Publisher source is incomplete or has an unsafe path")
        if hashlib.sha256(target.read_bytes()).hexdigest() != expected:
            raise RuntimeError(f"Publisher source checksum mismatch: {relative}")
    if source_only:
        print("Pinned publisher source checksums verified.", flush=True)
        return
    if sys.version_info < (3, 12):
        raise RuntimeError("Python 3.12 or newer is required")
    environment = ROOT / ".venv"
    python = environment / ("Scripts/python.exe" if os.name == "nt" else "bin/python")
    if not python.is_file():
        venv.create(environment, with_pip=True)
    run([str(python), "-m", "ensurepip", "--upgrade"])
    run([str(python), "-m", "pip", "install", "-r", str(ROOT / "requirements.txt")])
    npm = shutil.which("npm.cmd" if os.name == "nt" else "npm")
    if not npm:
        raise RuntimeError("Node.js/npm is required for the local Spider_XHS signing runtime")
    command = [npm, "ci", "--prefix", str(source), "--ignore-scripts", "--no-audit", "--no-fund"]
    if os.name == "nt":
        command = [os.environ.get("COMSPEC", "cmd.exe"), "/d", "/c", *command]
    run(command)
    run([str(python), "-c", "import requests, curl_cffi, execjs, loguru, qrcode, cv2; print('Publisher Python dependencies ready.')"])
    print("Publisher runtime ready. The workbench starts it when login/publishing is requested.", flush=True)


if __name__ == "__main__":
    parser = argparse.ArgumentParser()
    parser.add_argument("--source-only", action="store_true")
    arguments = parser.parse_args()
    try:
        prepare(arguments.source_only)
    except Exception as error:
        print(f"Publisher setup failed: {error}", file=sys.stderr)
        sys.exit(1)
