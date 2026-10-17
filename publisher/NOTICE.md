# Spider_XHS source notice

This directory includes a selected, byte-preserved snapshot from the user's
local `Spider_XHS-5.0.0` distribution. The source identifies its upstream as
https://github.com/cv-cat/Spider_XHS and its README advertises an MIT license.
The supplied distribution did not contain a `LICENSE` file; fetching the
upstream `5.0.0/LICENSE` and `master/LICENSE` URLs on 2026-10-07 returned 404.
No license text has been invented or attributed to the author. The retained
upstream README, source headers and `upstream.lock.json` record the provenance.
Before redistributing the bundled upstream source, obtain its actual license
and copyright notice from the source author.

`bridge.py`, `signer.cjs`, transport and tests are this workbench's adapter.
The vendored files are unchanged, including their original dependency lock.
No `.env`, Cookie files, local data, screenshots, virtual environments,
`node_modules`, or exploration artifacts were copied.
