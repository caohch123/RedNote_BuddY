# User workspace and administration

The same frontend and backend provide two separate entry points:

| Entry | Default URL | Purpose |
| --- | --- | --- |
| User workspace | http://127.0.0.1:8080/ | Collection, writing, image/video workflows, media, publication, and personal usage |
| Administration | http://127.0.0.1:8080/admin | Accounts, shared models, usage across users, tasks, and audit events |

Both sessions can coexist in one browser. An administrator account does not have a creative workspace.

## Initial accounts

The first backend startup creates `user` and `admin`, each with a randomly generated temporary password. Credentials are delivered locally in `data/initial-accounts.txt`, which is ignored by Git. Both accounts must change their temporary password before entering the application. Passwords require at least 12 characters and at most 72 UTF-8 bytes.

Restarts preserve accounts and passwords. Administrators create further accounts and receive their temporary passwords once in the result dialog. Password resets revoke previous sessions and require another password change. Keep the initial credential file private; you may remove it after replacing the temporary passwords.

## Data and permissions

The initial `user` owns the original `data/workbench.db`, assets, and Xiaohongshu account state, without moving existing files. Further members receive separate databases and media under `data/accounts/<user-id>/`. Content, tasks, collection cookies, publisher sessions, and usage ledgers are isolated.

Administrators inspect task status and usage without entering a member's content APIs. Model configuration, executable paths, and estimation rates are maintained centrally; members manage their passwords, collection cookies, and Xiaohongshu login. Keys and credential snapshots are excluded from public responses.

Back up the entire `data/` directory: identity, session hashes, quota counters, and audit records live in `data/identity.db`, while member databases and media are separate. The original business database alone is no longer a complete backup.

## Administration

The console includes daily overview, account creation and status, password resets, daily model request limits, concurrency, shared model configuration, usage charts and CSV exports, task management, and audit history. Saved keys are masked; an empty input preserves an existing key, and clearing it requires an explicit action.

Only unstarted queued tasks can be cancelled safely. Publication and unknown-submission tasks require user verification and cannot be blindly replayed by an administrator. Disabling an account revokes sessions, cancels local work, and pauses new queue claims. External tasks already accepted by a provider may continue running and incur charges.

Usage records show reported Tokens, input/output details, missing measurements, and image/video quantities. Costs are estimates based on administrator-supplied rates, not provider invoices. No historical or missing Token counts are inferred from text length. See [billing](billing.md).

## Limits and sessions

Daily quotas count model request attempts by the Asia/Shanghai calendar day; `0` means unlimited. A reservation is persisted before a model request. Failed connections and unknown responses still consume an attempt, while concurrency waiting, invalid parameters, and missing model configuration do not. One task can make several model requests. Concurrency is configurable from 1 to 8.

Guards cover text, vision, OCR, ASR, image generation, and video submission. Connection checks, polling existing video tasks, and media downloads are not new model requests. This release provides request limits, not prepaid balances, strict Token budgets, or payment processing.

Passwords use bcrypt. Separate user/admin cookies are HttpOnly and SameSite=Strict, with token hashes stored server-side. Mutations require session CSRF. Sessions rotate after login and password changes, expire after 12 hours or 30 minutes of inactivity, and login failures are rate limited.

This delivery remains a loopback-only application with exact Host/Origin checks. Public hosting additionally requires HTTPS, production deployment and secret management, rate limiting, monitoring, and tested recovery procedures.

## API entry points

Initialize the appropriate realm with `GET /api/v1/session?realm=user` or `realm=admin`, retain its cookie, and send the returned `csrf_token` as `X-CSRF-Token` for login and other mutations.

Member identity endpoints are `/auth/me`, `/auth/login`, `/auth/logout`, and `/auth/password`, under `/api/v1`. Administrator equivalents use `/admin/auth`. Password changes accept `{current_password,new_password}`.

Administrator resource endpoints are `/admin/users`, `/admin/users/:id/reset-password`, `/admin/overview`, `/admin/models`, `/admin/billing`, `/admin/billing/export`, `/admin/billing/prices`, `/admin/jobs`, `/admin/users/:id/jobs/:job/retry`, `/admin/users/:id/jobs/:job/cancel`, and `/admin/audit`. Usage supports `user_id`; audit returns the latest 500 events.

Existing member resource APIs now require an authenticated member session. Member `PATCH /settings` accepts only `{cookie}`; an explicit empty string clears it. Shared configuration uses administrator endpoints. Public `/health` returns only a minimal liveness response. See the [Chinese account/API guide](accounts-and-admin.md) for the full endpoint table.
