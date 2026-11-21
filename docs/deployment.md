# Deployment architecture

CyberGuard is packaged with Docker Compose. Caddy serves the compiled frontend and
routes API calls. Node is build-only. PostgreSQL and deployed Caddy certificate
state use persistent volumes; service startup does not provision data or download
models.

| Environment | Compose selection and service boundary |
| --- | --- |
| Local development | `compose.yaml` + `compose.dev.yaml`: Caddy, Go, and both PostgreSQL instances, with loopback host bindings and HTTP-only development settings. `compose.ai.yaml` adds optional Python and private CPU inference on the same host. |
| Azure application VM | `compose.yaml` + `deploy/app-vm/compose.yaml`: public Caddy/SPA, unexposed Go, both PostgreSQL instances, and a dedicated private Go TLS gateway. Uses neither local override. |
| Azure AI VM | Standalone `deploy/ai-vm/compose.yaml`: private Caddy, FastAPI, worker, Kev, and llama.cpp. No database, public frontend, or published model port. |

## Gateway routing

- `/api` is stripped before requests reach Go. Caddy prefixes Go's root-relative
  `Location` headers with `/api`, without changing the root cookie path.
- `/ai` is stripped before requests reach FastAPI. The selected Caddy snippet
  returns 503 with AI disabled, proxies locally with the AI overlay, or proxies to
  the AI VM over verified HTTPS. An unavailable AI upstream does not stop core workflows.
- Internal path segments are denied before proxying or SPA routing. Only explicit
  browser routes receive the SPA shell for GET/HEAD: login, account, incidents
  (including create/detail), notifications, and admin. Missing assets, source files,
  and unknown paths are not rewritten to HTML.
- The static shell and unfingerprinted files are no-store; existing fingerprinted
  assets are cached immutably. Shared local/application Caddy configuration is baked
  into the web image. Rebuild it when frontend or gateway files change.

## Deployed trust boundaries

The public app gateway terminates HTTPS using persistent ACME state or a provided
certificate. Go trusts forwarded client addresses only from the configured public
gateway IP. The browser uses the same origin and Secure session cookie for both APIs.

The AI gateway is not a public ingress: network policy must allow only the
application VM to reach it. The required reverse access is from the AI VM's
approved source address to private Go and incident PostgreSQL. Private Go permits
read requests for readiness, current user,
incident detail, worker authentication, and job snapshots. Both database instances use
verified TLS on the application VM; identity SQL is not published. Incident HBA
limits AI access to its restricted role/database over TLS.

Compose mounts scoped runtime file secrets. Azure resources, NSGs, private DNS,
certificate issuance for private endpoints, and secure transfer of AI-scoped
secrets are operator responsibilities; the VM bootstrap does not set them up.
Local overrides are not production settings.

See the [local](docker-setup.md), [application VM](azure-app-deployment.md), and
[AI VM](azure-ai-deployment.md) runbooks. Disposable deployment tests use real
services and TLS but do not establish Azure or real-model acceptance.
