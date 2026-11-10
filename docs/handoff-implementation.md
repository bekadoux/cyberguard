# CyberGuard — Go backend handoff implementation candidate

Base: `main` at `7b91f0b` (archived 10 November 2026). **Review and tests required before merge or any deployed migration.**

## Scope delivered as source code

- `00003_context_and_investigations.sql`: immutable `incident_contexts`, job-to-snapshot foreign key, atomic snapshot+initial job+submitted-event creation from the INSERT trigger, later context revisions; incident resolution timestamps, comments, append-only incident actions, persistent notifications, least-privilege grants.
- `00003_user_admin_audit.sql`: append-only administrative identity audit and restricted runtime grants.
- Go internal AI context endpoint now reads the snapshot associated with the persisted job, not mutable incident text. Historical jobs remain nullable/unbackfilled and will not have a snapshot in this new endpoint; no automatic backfill.
- Professional analyst/admin own-report projection now includes severity as required by the domain clarification. Employee projections remain restricted; Python's AI read endpoint continues to check current role through Go.
- Go incident API: claim (first claimant wins), admin assignment, restricted field updates, allowed status transitions, public/internal comments, soft archive, and professional action history.
- Reporter public answer resumes `awaiting_reporter` → `investigating`, but internal comments do not. Unassigned analysts may add **internal** comments, not professional-side public comments. The responsible analyst/admin may post public questions.
- Go session-protected admin API: create/list users, role change, block/unblock, password reset using Argon2id and `auth_version` invalidation; identity audit excludes passwords and hashes.
- Session-protected in-app notifications for incident activity. A periodic Go maintenance task closes resolved incidents seven days after `resolved_at`, recording an audit event and notification.
- New OpenAPI file `api/go-handoff.openapi.yaml` and Go unit/integration tests.

## Endpoints (Go direct paths; gateway prefixes `/api`)

| HTTP method | Path | Access |
| --- | --- | --- |
| POST | `/incidents/{id}/claim` | analyst/admin; only unassigned `submitted` |
| POST | `/incidents/{id}/assign` | admin; active analyst |
| PATCH | `/incidents/{id}/fields` | assigned analyst/admin |
| POST | `/incidents/{id}/transition` | state-machine permission check |
| POST | `/incidents/{id}/archive` | admin; reason required |
| GET/POST | `/incidents/{id}/comments` | scoped; `public`/`internal` rules |
| GET | `/incidents/{id}/history` | analyst/admin |
| GET | `/notifications` | current user |
| POST | `/notifications/{id}/read` | notification recipient |
| GET/POST | `/admin/users` | admin |
| POST | `/admin/users/{id}/role` | admin |
| POST | `/admin/users/{id}/block` | admin |
| POST | `/admin/users/{id}/password` | admin |
| GET | `/admin/audit` | admin; identity events |

All changes use existing `protectedSession` middleware, which validates current role/blocked state/auth version and refreshes before business operations. Mutations carry no browser cookie writes after commit. Errors do not expose database values.

## Important preconditions and limitations

1. **Create a new feature branch from the merged `main` before replacing any files.** Never overwrite the current main branch directly. A local stash may contain CRLF-only changes; keep it safely stored, do not reapply it to this patch.
2. **Apply migrations before restarting services**: identity through `00003`, incidents
   through `00005_snapshot_worker_safety.sql` (including `00004`'s snapshot-link grant).
   Rebuild the migration image first; bootstrap/migration never runs in API/worker startup.
3. **Do not edit applied migrations 00001/00002**. No automatic backfill is performed. Existing assessment jobs have `context_id IS NULL`; they continue to have stored results but cannot be retried using the new immutable-context endpoint until an explicitly reviewed backfill is designed.
   Migration 00005 leaves historical pending jobs untouched, excludes them from claims
   and blocks worker readiness until an explicit operator decision. Follow the
   [deployment upgrade procedure](azure-ai-deployment.md#upgrading-an-existing-deployment).
   Go/Python/frontend now agree that staff can read AI on their own reports; employee
   denial and current-account/incident checks remain enforced.
4. Resolution time remains in `incident_resolutions`, not an extra incident-row column.
   The sqlc output has been regenerated from the handwritten queries/migrations, including
   the previously missing `context_revision`; never edit generated files by hand.
5. Identity and incident stores are separate PostgreSQL instances: user assignment validation reads identity before the incident transaction, so cross-database role changes cannot be made fully atomic without an agreed architecture. Test race/revocation paths.
6. Auto-close runs every hour in the Go API (including once at startup). Dedicated scheduled maintenance would be preferable for multi-replica production deployments; current row locks prevent concurrent duplicate closures.
7. `/admin/audit` returns identity-user administration events; per-incident actions are at `/incidents/{id}/history`. No global cross-DB combined audit feed is provided.
8. Comment list, history list, notifications list, and users list have hard caps (500/500/100/500). Pagination and frontend screens remain separate follow-on work.
9. No new browser/frontend screens are bundled. Frontend remains assigned to the architect; supply them the updated OpenAPI and validate the UI jointly.
10. AI models, secret files, `.local`, `.git`, `.vs`, build caches, and production deployment configuration are not bundled or overwritten. The existing AI inference services remain unchanged.
11. Existing username/password admin calls have no dedicated request rate limiter beyond the authenticated system. Consider additional admin rate limits and HTTP audit monitoring before production.
12. **Original generation environment:** this patch could not be compiled/integration-
    tested there because only Go 1.23.2 was available and toolchain downloads were blocked.
    Subsequent integration validation with Go 1.27.3 is recorded in
    [the deployment guide](azure-ai-deployment.md#main-integration-validation--10-november-2026).
    Local checks do not establish production acceptance or real model inference.

## Migration and test sequence (on development database only)

- Create `feature/go-backend-handoff` from updated `main` and apply the **changed-files** ZIP, or extract the full-tree ZIP in a separate worktree. Keep a backup.
- Check the changed paths with `git status --short`, `git diff --stat`, and inspect SQL migration permissions.
- Rebuild migration image, run both identity/incident migrations with existing credentials; do not reset database volumes or regenerate secrets.
- Rebuild and start Go API with the existing Docker Compose overlays, preserving Python/AI settings.
- Run `make go-check` and `make go-build` with Go 1.27.3; run `make go-test-integration` against **two disposable test PostgreSQL instances** using the four TEST_* URLs (never production or current development DB).
- Test race scenarios: simultaneous claims, account blocked between auth/read, stale sessions, forbidden mutations, original context immutability, rollback on enqueue failure, visibility for employee vs analyst/admin authors, commenter assignment permissions, identity auditing, and timing of 7-day auto-close.
- For an explicit local demo fixture, run `python3 scripts/demo-incident-smoke.py` for dry-run, then `python3 scripts/demo-incident-smoke.py --apply --password-file /secure/local/employee-password-file`. Do not run this against production.
- Re-run the Python AI suite and the end-to-end scenario
  `Go → PostgreSQL → Python Worker → Kev/Qwen → assessment read`. The patched snapshot
  endpoint changes data access semantics; synthetic fixtures do not validate real models.
- Have architect review schema, grants, multi-DB authorization limitations, CSRF/CORS, proxy route exposure and workflow contract before merge.

## Follow-on questions for the team

- Admins can read action history of archived incidents by ID; ordinary incident detail remains hidden once archived. Confirm whether a global searchable archive/audit feed is needed.
- Confirm optional `ransomware` subcategory persistence, employee-facing notification presentation and exact severity/AI acceptance workflow.
- Confirm how the frontend will display and paginate history/notifications, and whether a dedicated scheduler should replace in-process auto-close for the Azure deployment.
