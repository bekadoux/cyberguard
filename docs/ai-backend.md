# Python AI service

The optional AI stack lives under [`backend/python/`](../backend/python). FastAPI
serves stored assessments; a separate worker polls PostgreSQL for jobs. Neither is
required for login, incident submission, or investigation. They never use identity
database credentials or run migrations.

## Assessment read path

`GET /ai/incidents/{id}/assessment` returns the initial job's status and its stored
result when completed. See [AI OpenAPI](../api/ai-assessment.openapi.yaml).
FastAPI forwards the browser's Cookie header to Go's `/auth/me` and
`/incidents/{id}` checks before accessing AI storage. Current analysts and admins
can read visible assessments, including on their own reports; employees cannot.
Authorization failure prevents the read. `/ai/readyz` checks AI database
connectivity, not worker or model readiness.

## Worker and persistence

1. An incident insert atomically queues one `initial_assessment` job tied to an
   immutable snapshot of the original report. Edits do not change the job's input
   or enqueue another assessment.
2. The worker authenticates to private Go before claiming. In managed Compose it
   also checks dependencies and `ai_worker_ready()`. It claims one job at a time
   with a renewable lease, random claim token, and bounded lease-recovery attempts.
3. The worker requests the saved job snapshot from private Go using its service
   credential, not a user cookie. If Go rejects that credential after a claim, the
   worker backs off and leaves the claim under its existing lease.
4. PostgreSQL rejects stale/expired claims and atomically writes the result and
   marks the job `completed`. Execution failures are terminal and expose safe
   error codes. The worker never changes human-owned incident fields.

Jobs have independent `queued`, `running`, `completed`, and `failed` states. Pending
legacy jobs without snapshots block worker readiness; no automatic backfill,
retirement, or retry for failed jobs is supplied. See the
[upgrade runbook](azure-ai-deployment.md#upgrading-an-existing-deployment).

## Models and output

- **Kev-0.8B**, a local Jev-style model, produces category probabilities, a 0–3
  potential impact score, and the eight reported indicators from the domain rubric:
  link clicked, credentials entered, attachment opened, payment made,
  data sent out, unusual device behavior, multiple affected, and ongoing activity.
  A low indicator score means "not explicitly stated", not "did not happen".
  Older stored assessments can still contain the previous four signal codes.
  The category suggestion is omitted when `other` wins or the top probability is
  below the provisional `0.60` threshold. An indicator highlight cutoff is not
  yet validated; scores remain advisory pending labelled EN/SK evaluation.
  Scores are not calibrated risk estimates or confirmed severity.
- **Qwen2.5-1.5B-Instruct, Q4_K_M GGUF**, via CPU llama.cpp, produces a summary,
  reported facts, unknowns, and follow-up questions. The worker measures the
  templated prompt against the 2,048-token context and reserves 400 output tokens.
  It treats incident text as untrusted input. The model cannot call tools;
  the worker takes up to four suggested steps from [checked-in playbooks](playbooks.md),
  not from model-generated actions.
- Inference runs on private networks with explicitly prepared, read-only model
  files. There is no hosted fallback. `AI_MODE=fake` is a labelled integration mode,
  not a fallback for production or failed inference.

Staff manually refresh assessment status. Assigned analysts or admins can record
one human accept/correct/reject decision per real assessment on a non-terminal
incident. Go persists the review separately from the immutable output. Acceptance
or correction updates the confirmed category; rejection leaves it unchanged.
Severity is a separate human action.

See [local AI setup](docker-setup.md#optional-local-ai) or the
[AI VM runbook](azure-ai-deployment.md) for model preparation. In a Python 3.13
environment with `backend/python/requirements-dev.txt` installed, run
`PYTHONPATH=backend/python python -m pytest -q backend/python/tests` from the root.
Those tests use mocked HTTP services; disposable smoke checks exercise the real
service and database wiring with synthetic model output. Neither establishes real
inference quality or capacity.
