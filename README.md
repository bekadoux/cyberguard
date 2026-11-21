# CyberGuard

A self-hosted application for reporting and managing cybersecurity incidents,
with optional AI-assisted analysis.

## Quick start

Try CyberGuard locally with Docker. You need **Linux or WSL2**, **Git**,
**Docker with Compose v2**, **GNU Make**, **OpenSSL** and **jq**. Docker must be
running; Go, Node.js and Python do not need to be installed on your machine.

Open a **Bash** shell and run:

```bash
git clone https://github.com/bekadoux/cyberguard.git
cd cyberguard

export DOCKER_SECRET_GID="$(id -g)"
make docker-secrets
make docker-build
make docker-db-up
make docker-bootstrap
make docker-migrate
make docker-seed-demo
make docker-up
```

Open **[http://127.0.0.1:8088/login](http://127.0.0.1:8088/login)**.

These commands are for a **fresh local installation**. The first build needs
internet access and may take several minutes. AI is configured separately;
see [Enable AI](#enable-ai).

> This local setup uses HTTP and demo accounts. Do not expose it to the internet;
> use the [server deployment guides](#deploy-on-a-server) instead.

## First sign-in

Sign in as **`admin`**. Its randomly generated password is stored locally:

```bash
cat .local/docker-secrets/dev_admin_password
```

Keep this password private. There is no shared default password or public
registration; users sign in with an account provided by their administrator.

## Enable AI

AI is optional: the application can run without it. To configure the AI service
and download its models, follow the [AI setup guide](docs/ai-backend.md).
Models require additional disk space and memory.

## Deploy on a server

For an internet-accessible installation, follow the guides in order:

1. **[Application server](docs/azure-app-deployment.md)** — deploy the application
   with HTTPS and persistent databases.
2. **[AI server](docs/azure-ai-deployment.md)** — optionally connect a separate
   server for AI processing.

The guides target existing **Ubuntu 26.04 amd64** VMs and cover configuration,
certificates, private networking and startup. They do not create cloud resources.

## Stop and restart

Run these commands from the project directory in Bash:

```bash
make docker-logs       # View recent service logs
make docker-down       # Stop and remove containers; keep data
```

To start the same local installation again:

```bash
export DOCKER_SECRET_GID="$(id -g)"
make docker-up
```

**Keep `.local/docker-secrets/` and the database volumes together.** Do not
regenerate secrets for an existing installation. `docker compose down -v` deletes
database volumes; stopping containers is not a backup.

For configuration, port changes and setup details, see the
[local Docker guide](docs/docker-setup.md). For an existing server installation,
use the deployment guides' maintenance and upgrade procedures rather than
repeating the fresh-install steps.
