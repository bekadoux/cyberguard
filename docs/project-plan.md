# CyberGuard — Project Overview and Current Plan

- **Slovak name:** Inteligentný informačný systém na správu kybernetických incidentov
- **Team:** Four university students
- **Status:** Planning snapshot, 7 November 2026

This document records the current project direction. It distinguishes the planned
scope from optional extensions and decisions still requiring validation. It is not
yet a complete specification of screens, API contracts, or incident-state transitions.

## 1. Purpose

CyberGuard is a web-based enterprise information system for reporting, investigating,
and resolving cybersecurity incidents within a company.

Employees report suspicious activity and follow the progress of their reports.
Security analysts assess the reports, investigate them, communicate with reporters,
and record the outcome. Administrators manage users and have access to all application
capabilities.

AI supports this process through structured incident triage and a focused analyst
assistant. AI outputs are advisory: they do not establish that an attack occurred,
execute response actions, or close incidents automatically.

For example, an employee reports an email asking them to confirm their password through
a link. The system suggests a phishing classification, highlights relevant reported
indicators, and helps an analyst prepare follow-up questions and investigation steps.

## 2. Scope

### 2.1 Core application

- Login and logout using server-side sessions.
- Three roles: Employee, Security Analyst, and Administrator.
- Incident submission with a title, description, and optional textual affected-system
  information; no asset inventory is necessary for the initial version.
- An employee view of their own incidents and progress.
- An analyst queue with basic filtering and an incident workspace.
- Analyst assignment, category and severity updates, and incident-status changes.
- Comments and requests for additional information from the reporter.
- Resolution recording and incident history.
- Simple in-app notifications rather than email or external messaging.
- User administration: account creation, role assignment, blocking, and password reset.
- Audit records for important administrative and incident-management actions.
- A minimal, reproducible demo-data import or seed mechanism; the exact approach is
  not yet selected.

New incidents start in `submitted` status, with unassessed category/severity left null.
Subsequent lifecycle transitions, closure/reopening rules, and notification triggers
remain to be specified. Basic dashboards may show counts and queues; advanced analytics
are not part of the initial scope.

### 2.2 Planned AI capabilities

- Structured suggestions for incident category, potential impact, and defined indicators.
- Visible uncertainty and analyst review/correction of predictions.
- An LLM assessment/investigation brief automatically queued when an incident is created.
- Non-blocking analyst review, with assessment-generation progress visible in the UI.
- Persistent assessment-job state, AI results, and analyst feedback.

The generative component is an important part of the intended direction, not merely a
distant optional chatbot. Its final model and output limits depend on CPU feasibility
tests. The ordinary incident workflow must work without successful AI inference.

### 2.3 Optional or deferred features

- Attachments and screenshot handling.
- Asset inventory beyond a simple affected-system text field.
- Investigation task management.
- PDF reports.
- Complex escalation and approval rules.
- External integrations, including outbound notifications.
- General-purpose imports beyond the minimal demo mechanism.
- Advanced analytics and reporting.
- A general-purpose chatbot or autonomous AI agent.
- BPMN documentation, currently a low-priority bonus.

## 3. Users and Permissions

### Employee

- Submit an incident.
- View their own incidents and their visible history.
- Add information and respond to analyst questions on their own incidents.
- Receive in-app updates.

### Security Analyst

- Use employee capabilities.
- View the incident queue and incident details across the company.
- Assess category, severity, and reported impact.
- Review or override AI suggestions.
- Investigate incidents, request information, and record resolutions.

### Administrator

- Use all employee and analyst capabilities.
- Manage accounts and roles, block users, and reset passwords.
- Assign or reassign incidents.
- Classify, resolve, reopen, or archive incidents, subject to normal workflow rules.
- Access the minimal demo-data mechanism and application oversight views.

The intended application permission hierarchy is:

```text
Employee permissions ⊂ Analyst permissions ⊂ Administrator permissions
```

The administrator is an application superuser, not a database or operating-system
superuser.

Removal should normally mean archiving/soft-deleting an incident, with the actor,
timestamp, and reason recorded. Administrators should not be able to erase audit
history through ordinary application functionality. All authorization is enforced on
the backend.

## 4. Technology and Architecture

### 4.1 Stack

| Component | Planned technology |
| --- | --- |
| Frontend | React, TypeScript, Vite, React Router |
| Main backend | Go with Chi; pgx, goose, sqlc, and PostgreSQL-backed SCS sessions |
| AI backend | Python with FastAPI |
| Databases | Two separate PostgreSQL instances |
| Packaging | Docker Compose |
| Hosting | A CPU-only Azure Linux VM using student credit |

JavaScript/TypeScript is limited to the frontend and its build tooling. Node.js may
be used to build the frontend, but is not needed as a deployed backend. Go can serve
the compiled frontend assets.

### 4.2 Service interaction

The frontend makes explicit requests to both backend APIs. Go handles authentication
and incident-management operations; Python exposes the AI capabilities. Python calls
Go internally to authorize requests and obtain the incident context needed for analysis.
After an incident is saved, its creation workflow schedules assessment generation on
the backend. The browser does not need to remain open for generation to finish.

```text
Browser: React + TypeScript
    |
    +--- Auth / incidents / administration ---> Go backend
    |                                              |
    |                                              +---> Identity PostgreSQL
    |                                              |
    |                                              +---> Incident PostgreSQL
    |
    +--- Triage / assessment status/results --> Python + FastAPI
                                                   |
                                                   +---> Go: authorize access
                                                   |     and obtain incident text
                                                   |
                                                   +---> Local inference
                                                   |
                                                   +---> Incident PostgreSQL
                                                         AI tables only
```

This is frontend integration with two services as well as backend-to-backend web-service
integration. Final public URLs, HTTPS termination, and routing remain deployment
decisions. A shared ingress could route browser requests to the appropriate service
without making Go orchestrate every AI operation.

### 4.3 Go backend foundation

- **HTTP:** Chi, standard-library `net/http` and `encoding/json`, and `go-chi/cors`.
- **Database access:** `pgx/v5` with separate pools for the two databases, goose SQL
  migrations, and sqlc configured for `pgx/v5` with separate generated database packages.
- **Authentication:** SCS v2 with `pgxstore`; `alexedwards/argon2id` for passwords.
- **Supporting code:** `log/slog`, environment-based typed configuration, explicit
  validation/permission checks, API response types separate from database models, and
  HTTP/database timeouts and graceful shutdown.
- **Security:** credentialed CORS with explicit origins, CSRF protection, and current
  account/role checks rather than permanently trusting session-cached privileges.
- **Workflow:** pin tools, commit generated sqlc code, coordinate migrations across
  services, and use `gofmt`, `go vet`, `go test`, and `govulncheck`. Integration tests use
  disposable PostgreSQL databases; API contracts are documented with OpenAPI.

Approved contract conventions are UUID identifiers, `snake_case` JSON fields, UTC/RFC
3339 timestamps, and role values `employee`, `analyst`, and `admin`. Use a consistent
JSON error format and fixed local API ports with explicit browser-security configuration;
the exact error envelope, port numbers, and configuration values still need specification.

The engineer's first milestone is login, incident creation/list/detail with role-based
visibility, and durable assessment scheduling, all usable while Python is offline.
Use a PostgreSQL job table rather than introducing a broker. Avoid an ORM, JWT system,
generic authorization engine, or dependency-injection framework initially.

See the [software engineer handoff](software-engineer-handoff.md) for detailed tasks,
service boundaries, and acceptance checks. Exact tool versions and API/schema details
still need agreement before implementation.

## 5. Authentication and Authorization

### 5.1 One authentication authority

Go owns authentication. Python does not implement a second login system, verify user
passwords, or directly read the identity database.

The initial authentication design uses:

- Administrator-created username/password accounts.
- Password hashing using `alexedwards/argon2id`.
- PostgreSQL-backed server-side sessions using SCS v2 and `pgxstore`.
- A random session identifier in an HttpOnly cookie.
- Secure cookies and HTTPS in deployment, with appropriate SameSite settings.
- Session expiration and invalidation on logout.
- Session invalidation after blocking an account or resetting its password; role changes
  must take effect without leaving stale privileges active.
- CSRF protection for state-changing requests, input validation, and basic login
  rate limiting.

A current-user endpoint lets React obtain the authenticated identity and role without
reading the session cookie. JWT access/refresh-token infrastructure, external identity
providers, public registration, email verification, email-based password recovery, and
MFA are outside the initial scope.

### 5.2 Authorization of Python requests

An illustrative assessment-status/result request follows this sequence:

1. The user logs in through Go and receives the session cookie.
2. React requests the assessment status or saved result for an incident from Python.
3. Python asks an internal Go endpoint to authorize the session, incident, and action.
4. Go validates the session and the user's permission to access that specific incident.
5. Go returns only the incident context needed for the authorized operation.
6. Python returns the current job state or persisted assessment; the request does not
   wait for generation to finish.

The same access checks apply when retrieving previously generated results. Python must
not trust browser-supplied roles, ownership claims, or an incident ID without an access
check. If authorization cannot be validated, access is denied.

The internal Go endpoint is protected using a server-only credential and network
restrictions. Session identifiers, passwords, and service credentials must not be logged.

Automatic background generation uses authorized service-to-service communication tied
to the saved incident, not a stored browser session cookie. Logging out or closing the
page does not cancel an already scheduled assessment. Reading its status or result still
requires the current user's incident-access check.

### 5.3 Browser requests to two services

One simple arrangement uses the same hostname with different ports. Cookies are scoped
to hosts and paths, not ports, so both trusted backend services can receive the session
cookie. Different ports are nevertheless different origins.

Cross-origin requests therefore require credentialed browser requests and an explicit
frontend-origin CORS allow-list on the APIs. CORS does not replace authorization or
CSRF protection. Different-host deployments would require revisiting cookie scope and
routing rather than assuming the same arrangement works unchanged.

## 6. Database Separation and Security

The planned split is by data sensitivity and purpose rather than one database per
backend service:

| Database | Main contents | Application access |
| --- | --- | --- |
| Identity | Users, password hashes, roles, sessions | Go only |
| Incidents | Incidents, comments, notifications, audit records, assessment jobs/results, AI predictions and feedback | Go; restricted Python access to AI tables |

Incident records refer to users through stable identifiers. Cross-database foreign keys
are not assumed; account blocking and data-retention rules must preserve historical
references. AI records similarly reference incident identifiers.

Go uses separate connections and credentials for the two databases. Python obtains
authorized incident text through Go and should not receive broad access to incident
history merely to save predictions. Database roles follow least privilege; neither
backend connects as a PostgreSQL superuser.

This separation can limit some breaches: a leaked Python database credential need not
expose password hashes, while read access to the identity database does not itself
reveal incident descriptions. It does not protect incident history after the incident
database is compromised, nor isolate both databases from a fully compromised Go process.

Database ports remain private. Local AI hosting also does not replace access controls,
network restrictions, safe logging, or secure secret management.

## 7. AI Design

### 7.1 Jev and Jev-style decision models

Jev is TypeSafe AI's proprietary, low-latency decision model. Rather than generating
free-form text, it evaluates input state against typed questions and returns structured,
probabilistic decisions suitable for classification, routing, and application logic.
TypeSafe describes Jev as trained for calibrated probabilities. Calibration on the
project's incident data still needs evaluation and must not be assumed for alternatives.

The relevant text-oriented operations are:

| Operation | Meaning | Proposed CyberGuard use |
| --- | --- | --- |
| Choice | Select among defined options | Suggest incident category |
| Score | Rate input against ordered criteria | Suggest potential impact using a defined rubric |
| Noul | Return the probability that a yes/no proposition is true | Flag explicitly reported indicators, such as credential disclosure |

Choice and Score include probability distributions and a derived confidence statistic.
Noul returns the yes probability directly. Confidence and probability are not
interchangeable, and a confident model response is not a guarantee of correctness.

The lecturer has confirmed that hosted APIs, self-hosted models, and student-trained
models are all acceptable. The preferred project direction is self-hosting because
incident descriptions may be sensitive. This means evaluating an open-weight Jev-style
alternative.

Candidates discussed so far include:

- **Kev:** a family of open decision models with pretrained checkpoints and a
  TypeSafe-compatible API.
- **jeff:** a TypeSafe-compatible implementation based on GLiFormer, with CPU support
  and an ONNX/INT8 execution option.

Neither is selected. API compatibility does not imply equal accuracy, calibration, or
CPU performance. In particular, jeff's documentation describes CPU execution as a
fallback and reports unfavorable CPU performance/cost in its tested deployment.

### 7.2 Structured triage

The domain expert and AI expert will define a compact category taxonomy, impact rubric,
and indicator questions. The model should make several narrow assessments rather than
one opaque overall judgment.

Suggestions must be distinguishable from analyst-confirmed values. Uncertain or
unsupported reports require manual assessment. Absence of evidence in a description
must not be interpreted as proof that an incident is harmless. Analyst corrections
are retained as feedback; they do not imply automatic retraining.

### 7.3 Generative investigation assistant

When a new incident is saved, the system automatically queues a background job to
generate an LLM assessment/investigation brief containing:

- A concise incident summary.
- Reported facts separated from unknowns.
- Targeted follow-up questions.
- Suggested investigation steps grounded in a small, approved playbook collection.

The workflow is asynchronous:

1. Save the incident and its queued assessment job in one incident-database transaction,
   without waiting for Python or inference.
2. Make the incident immediately available for analyst review and other normal actions.
3. Generate the assessment in the background, with bounded CPU concurrency.
4. Save the completed assessment and job state in the incident database.
5. Update the incident page to display the saved assessment when it becomes available.

Persist job state so an assessment can be tracked independently of an HTTP request or
open browser tab. The intended states are `queued`, `running`, `completed`, and `failed`,
separate from the incident's business status. While queued or running, the frontend
clearly displays that assessment generation is in progress and refreshes its status
without blocking the analyst. A failed job is shown as an AI-generation failure, not
an incident-submission failure.

Go inserts the queued job; the Python worker claims it and persists execution state and
results. The PostgreSQL job table is the durable handoff, with no separate broker needed.
The generated draft must not overwrite the analyst's comments, classification, or
resolution work. Worker claiming/recovery details, status-update transport, retry
policies, and regeneration behavior after incident edits remain implementation decisions.

The analyst reviews, edits, or discards the output. It is advisory, not verified evidence
or authorization to take action. A generated assessment is not presented as a faithful
explanation of the separate decision model's internal reasoning.

Incident text is untrusted model input. The model has no command-execution capability,
external-system access, or authority to resolve incidents.

### 7.4 CPU feasibility and evaluation

No GPU is available under the team's Azure deployment arrangement. Candidate models
must therefore be tested on CPU before their performance is promised in the plan.

The initial approach is:

- Evaluate small decision models with CPU optimizations where supported.
- Test a small, quantized instruction model, initially around 1–3B parameters, using
  a CPU-oriented runtime such as llama.cpp.
- Keep input context and generated output bounded; avoid long reasoning generation.
- Limit inference concurrency so AI work does not starve the web application or databases.
- Show queued, running, completed, and failed assessment states rather than blocking
  incident submission or analyst review on generation.
- Keep model weights available locally across restarts and validate startup behavior.

Early testing should measure classification quality, uncertainty behavior, generated
brief usefulness, factual errors, warm latency, startup time, peak RAM, and the effect
of inference on the rest of the application. A reviewed evaluation set and classification
metrics such as macro-F1 and a confusion matrix are proposed; exact datasets, thresholds,
and acceptable response times remain to be agreed.

Local CPU tests can screen candidates, but the selected models must also be validated
on the actual Azure VM. Self-hosting keeps inference within the project-controlled
Azure deployment; there must be no silent fallback sending incident data to an external
AI provider. Logging and telemetry settings need review as well.

## 8. Deployment

The target is one Azure Linux VM running Docker Compose, funded through student credit.
Kubernetes and a collection of independently managed cloud services are unnecessary
for the initial project.

The four planned application containers are:

1. Go backend, also serving the compiled frontend.
2. Python AI backend, with local inference dependencies.
3. Identity PostgreSQL instance.
4. Incident PostgreSQL instance.

The initial four-container target assumes inference can run within the Python service.
A separate model server or ingress container would change the count and must be
considered explicitly rather than added unnoticed. Final HTTPS/routing arrangements
are still open.

A VM with **4 vCPUs and 8–16 GB RAM** is a provisional benchmarking target, not a proven
minimum or a selected Azure SKU. Availability, subscription quotas, remaining credit,
and measured resource use determine the final size.

Deployment planning includes persistent database volumes, migrations, health checks,
configuration/secrets outside source control, and reproducible demo setup. Only required
application/API endpoints should be public. Database ports remain private.

The team should monitor cost and deallocate the VM when unused, remembering that disks
and other retained resources can still incur charges. Core incident-management functions
must remain available if inference is slow or unavailable.

## 9. Demo Data

Use a small, curated, reproducible set of synthetic or sanitized incident reports.
Expected categories and scenarios should be reviewed by the domain expert and AI expert.
The demo mechanism should populate ordinary incident records without requiring a
general-purpose ingestion pipeline or a separate user-impersonation feature.

No dataset has been selected. Public datasets may supply examples, but their licenses,
label mapping, quality, and sensitive content need review first. Images are not required
because attachments and image analysis are outside the initial scope. Demo data must
not contain real passwords, malware, or confidential company incident details.

## 10. Team Responsibilities

| Required role | Primary responsibilities |
| --- | --- |
| Software Architect | Architecture, stack and API/data contracts, authentication foundation, deployment, integration planning, technical risks |
| Software Engineer | Core incident workflow, application APIs and UI, persistence, integration tests; implementation shared with teammates |
| AI Expert | Python/FastAPI service, background worker, Go authorization/context client, AI persistence, model selection and CPU feasibility, evaluation, uncertainty handling, feedback integration, service tests and Dockerfile |
| Domain Expert | Incident taxonomy and business workflow, permission requirements, realistic scenarios and demo data, acceptance testing, documentation and presentation |

These are ownership areas, not isolated silos. The domain expert owns business correctness
and validation, not just presentation. The software engineer must not be left to implement
the entire Go backend and frontend alone. Concrete module ownership and frontend work
will be divided when the screen list and API contracts are agreed.

For the initial backend work, the engineer owns Go incident creation and job enqueueing;
the AI specialist owns Python job execution and assessment status/result endpoints.
They agree shared schemas/contracts with the architect and start integration with a
clearly labelled fake assessment rather than waiting for a final model.

## 11. Assignment Alignment

| Requirement | Planned evidence |
| --- | --- |
| Enterprise information system | Company incident-reporting, investigation, and resolution workflow |
| Integrated application web services | Python-to-Go authorization/context calls and AI API integration |
| Login/logout, sessions, at least two roles | Go-owned server-side sessions and three backend-enforced roles |
| Main backend supporting at least two screens | Employee incident list/detail and analyst queue/workspace backed by Go |
| Second backend in another language | Python/FastAPI alongside Go |
| Frontend integrates both backends | Separate frontend calls to incident/auth APIs and AI APIs |
| Two separate SQL databases | Identity and incident PostgreSQL instances, both actively used |
| Machine learning | Structured triage plus a planned generative analyst assistant |
| Deployment | CPU-only Azure VM and Docker Compose |

BPMN is currently a low-priority bonus. If pursued, its recorded target is at least
60 activities, two pools, at least three lanes in one pool, a subprocess, and gateways.
A possible structure is a company pool with Employee, Analyst, and Administrator lanes,
plus an external security-provider pool. Modeling an external provider would not imply
implementing an external integration in the application.

## 12. Next Decisions and Proposed Delivery Sequence

### Still to be agreed or validated

- Final incident taxonomy, severity/impact rubric, lifecycle, and closure/reopening rules.
- Exact screen inventory, API contracts, and detailed permission matrix.
- Demo import versus seed mechanism and chosen example data.
- Decision model, generative model, supported language(s), approved playbook sources,
  and inference runtime.
- Evaluation set, uncertainty thresholds, and acceptable CPU latency/resource use.
- Exact Go/tool versions, detailed database schemas, and assessment-worker claiming,
  retries, restart recovery, status updates, and regeneration after edits.
- Azure VM size, public endpoint routing, and HTTPS termination.
- Concrete implementation assignments, milestones, and presentation scenario.

### Proposed sequence

1. **Feasibility and specification:** benchmark AI candidates on CPU; agree workflows,
   screens, permissions, and service contracts.
2. **Core vertical slice:** deploy the initial stack; implement login and the incident
   submission/list/detail workflow with both databases in use.
3. **AI integration:** connect frontend AI requests, internal authorization, structured
   triage, feedback, and automatic background assessment generation with persisted
   results and visible progress.
4. **Completion and demonstration:** finish administration, comments, notifications,
   audit history, deployment checks, testing, demo data, and presentation.
5. **Extensions:** consider deferred features and BPMN only after the core is stable.

Proposed acceptance checks include preventing cross-user incident access, immediately
honoring logout and account blocking, allowing analysts to correct suggestions,
retaining incident/AI data across container recreation with persistent volumes, and
keeping incident management usable when AI requests fail. A clean checkout should
have documented deployment and demo-setup instructions.

Assessment-specific checks should verify that incident creation automatically schedules
generation, an analyst can start reviewing while it is queued/running, the UI shows
progress, and the result is persisted and displayed on completion without overwriting
the analyst's work. Closing the submitting browser must not prevent job completion.

## 13. Research References

- [TypeSafe introduction and decision primitives](https://docs.typesafe.ai/introduction)
- [TypeSafe probability and confidence documentation](https://docs.typesafe.ai/confidence)
- [Kev: open Jev-style decision models](https://github.com/jaredpalmer/kev)
- [jeff: GLiFormer-based implementation and CPU deployment notes](https://github.com/logan-markewich/jeff)

These references establish candidate approaches, not a final model selection or a
guarantee of their suitability for cybersecurity incidents.
