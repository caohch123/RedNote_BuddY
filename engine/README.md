# XHS-Downloader 2.8 collector service

This service reuses JoeanAmier's original `POST /xhs/detail` handler from the immutable 2.8 commit in `upstream.lock.json`. The source ZIP SHA-256 is checked before installation; `prepare.py` verifies each extracted file on every run and installs all transitive versions from its original `uv.lock`, using pinned uv 0.12.23.

Requirements: Python 3.12 or newer and internet access for initial dependency installation.

```powershell
python engine/prepare.py
engine/.venv/Scripts/python.exe engine/server.py --data-dir data/engine --port 5556
```

`GET http://127.0.0.1:5556/health` reports the pinned version. The collector accepts `{url,cookie,download:false,check_record:false}`. It runs on loopback, rejects direct browser requests, disables media downloading, never echoes upstream `params`, and only returns an allowlist of note metadata. Cookie values are not logged or stored by this wrapper. Upstream diagnostic printing is disabled; exceptions are translated into fixed errors.

The wrapper substitutes the upstream Volume constant at import time so all engine databases and temporary data live under `--data-dir`. The downloaded upstream source stays unmodified on disk. Its request method is replaced in memory to enable TLS verification, verify public DNS addresses, pin DNS resolution and validate every redirect to supported XHS domains. Media downloads and deduplication belong to the Go service. No existing GUI executable or desktop settings are used.

Source: [JoeanAmier/XHS-Downloader](https://github.com/JoeanAmier/XHS-Downloader/tree/47840a1bee8438324ff10753c4291148c46071c8). License: **GNU GPL v3.0**. The complete original LICENSE and original source remain in `vendor/`. The wrapper is GPL-3.0 as well; retain this notice and source when distributing the collector. Upstream direct requirements are pinned in its `requirements.txt`; reproducible installation uses the stricter transitive `uv.lock` with hashes.

The separate Go API communicates over localhost HTTP and normalizes metadata; the wrapper never supplies invented notes when a remote collection fails. Platform access, Cookie validity and external network behavior cannot be guaranteed by a successful local health check.
