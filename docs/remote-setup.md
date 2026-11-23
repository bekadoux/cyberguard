# Deploy CyberGuard on two new Azure VMs

Set up an **app VM** for the website, Go API, and both PostgreSQL databases,
and a separate **AI VM** for the assessment API, worker, and local models.
The Azure portal steps below prepare the hosts and network. Then
`deploy/remote-setup.sh` runs the deployment from your workstation over SSH.

Use a reviewed, committed checkout containing the installer. The workstation
needs Linux, Bash 4.4+, OpenSSH, Git, OpenSSL, curl, `getent`, and `flock`. It
does not need Docker, Go, Node.js, or Python.

## 1. Create the VMs

In the [Azure portal](https://portal.azure.com), go to **Virtual machines →
Create → Azure virtual machine**. Create two **Ubuntu 26.04 amd64** VMs with
at least **20 GiB free under `/var` on each** after OS installation. That is a
setup minimum, not a sizing recommendation for database growth or inference.

- Put both VMs on the same VNet. In each **network interface → IP configurations**,
  open the primary IP configuration and set the private IPv4 allocation to
  **Static**. Record both addresses.
- Give the app VM a static public IPv4. The AI VM does not need a public IP
  if your workstation can reach it over an approved VPN, bastion, or tunnel.
- Select **SSH public key** authentication for each non-root operator. Use
  the appropriate public key for each VM and keep the private keys on your
  workstation. Each operator must be able to run `sudo -n` for the installer.
- Verify outbound access for package, image, and build downloads. The app
  needs access to its public certificate authority for ACME; the AI VM needs
  access to Hugging Face for model preparation.

Do not run the manual CyberGuard bootstrap scripts, generate application
credentials, or initialize databases first. The installer handles those
steps and refuses to adopt an existing manual deployment.

## 2. Set the inbound rules on each VM's NSG

In the portal, open **Network security groups → the VM's NSG → Inbound
security rules → Add**. Use the separate NSGs for the two VM interfaces
and check any additional NSGs on their subnets.

| NSG | Source | Allow TCP ports |
| --- | --- | --- |
| App | Internet, for ACME HTTP validation and redirects | `80` |
| App | Intended web clients | `443` |
| App | AI VM's private IPv4 `/32` | `8443` (Go), `5432` (incident PostgreSQL) |
| AI | App VM's private IPv4 `/32` | `8443` (AI gateway) |
| Both | Approved administration source (for example, workstation public IP or bastion subnet) | `22` |

For each private service port, put a specific peer **allow** before a **deny
from all other sources**. Both must outrank Azure's default `AllowVnetInBound`
rule; lower priority numbers are evaluated first. Apply the same allow-then-deny
pattern to SSH if only the approved admin source should be able to reach it.
Retain access for certificate validation on the app's public endpoint. Check
**Effective security rules** on both NICs; traffic has to pass both the NIC
and subnet NSGs. A private bind address or UFW alone does not isolate
Docker-published ports. Keep direct Go HTTP, identity SQL, FastAPI, and model
ports off the public network.

Compare the three Docker `/28` subnets in the
[settings template](../deploy/remote/config.env.example) with your VNet, VPN,
and existing Docker networks. Change the examples if they overlap. See the
[networking runbook](azure-app-deployment.md#3-networking-and-certificates-are-explicit-prerequisites)
for the full trust boundaries.

## 3. Configure public and private DNS

**Public name:** If you own a domain, add an `A` record pointing to the app
VM's public IPv4 at its DNS provider. If Azure hosts the zone, use **DNS zones
→ your zone → Recordsets → Add**. Without a custom domain, use **Public IP
addresses → the app public IP → Configuration → DNS name label** and record
the full hostname Azure displays. This is `APP_PUBLIC_NAME`.

**Private names:** In the portal, go to **Private DNS zones → Create** and
create `cyberguard.internal` (Azure does not support a single-label zone
such as `internal`). In the new zone, open **DNS Management → Virtual
Network Links → Add** and link the VNet containing both VMs. Leave automatic
registration off and add the following `A` records under **DNS Management →
Recordsets → Add**:

| Record name | Full name | Address |
| --- | --- | --- |
| `app` | `app.cyberguard.internal` | App VM's private IPv4 |
| `incidents` | `incidents.cyberguard.internal` | App VM's private IPv4 |
| `ai` (optional) | `ai.cyberguard.internal` | AI VM's private IPv4 |

The installer reaches the AI gateway by IP but verifies the name
`ai.cyberguard.internal` in its certificate. The other two names must
resolve **inside the AI containers**, not just via `/etc/hosts` on the VM.
If your VNet uses custom DNS servers, make sure they resolve the private
zone. See [Microsoft's Private DNS portal guide](https://learn.microsoft.com/en-us/azure/dns/private-dns-getstarted-portal).

## 4. Verify SSH access from your workstation

Use an alias for each VM in `~/.ssh/config`. Replace the example hostnames,
usernames, and private key paths. The AI hostname or address must already be
reachable from the workstation through your approved admin access route.

```sshconfig
Host cyberguard-app
    HostName app-vm.example.org
    User azureuser
    IdentityFile ~/.ssh/cyberguard-app
    IdentitiesOnly yes

Host cyberguard-ai
    HostName ai-vm.example.org
    User azureuser
    IdentityFile ~/.ssh/cyberguard-ai
    IdentitiesOnly yes
```

Restrict access to the private keys and SSH configuration (for example,
`chmod 600` on each file). Before accepting a host key, verify its fingerprint
through a trusted channel. For example, in the portal use **Virtual machines →
your VM → Operations → Run command → RunShellScript** to run
`ssh-keygen -lf /etc/ssh/ssh_host_ed25519_key.pub` on each VM. Compare the
fingerprint when OpenSSH prompts. The installer rejects unknown or changed host
keys and never copies your private SSH keys or forwards your agent.

From the workstation, verify SSH, sudo, address assignments, disk space, and
private DNS:

```bash
ssh cyberguard-app 'hostname && sudo -n true && ip -br -4 addr && df -h /var'
ssh cyberguard-ai 'hostname && sudo -n true && ip -br -4 addr && df -h /var'
ssh cyberguard-ai 'getent ahostsv4 app.cyberguard.internal incidents.cyberguard.internal'
```

Both DNS names should return the app VM's private IPv4. `sudo -n` must work
without prompting; this is separate from a workstation SSH key's passphrase.
OpenSSH can prompt to unlock a protected key on the first connection, or you
can load the keys with `ssh-add` before an unattended run. The installer
reuses one SSH connection per VM. Instead of aliases, you can set each
`user@host` and private key path in `APP_SSH`/`APP_KEY` and `AI_SSH`/`AI_KEY`.

## 5. Prepare the private TLS bundle

On the workstation, put the approved CA bundle and issued server certificates
and matching keys in a protected directory **outside Git**. Include any
needed intermediate certificates in the server chains. The installer verifies
the exact DNS SANs, trust chain, matching key, and at least 24 hours of validity.

| Files | Required DNS SANs or purpose |
| --- | --- |
| `ca.crt` | Trusted CA bundle, not a signing key |
| `identity.crt`, `identity.key` | `identity-db` |
| `incident.crt`, `incident.key` | `incident-db` and `incidents.cyberguard.internal` |
| `go.crt`, `go.key` | `app.cyberguard.internal` |
| `api.crt`, `api.key` | `ai.cyberguard.internal` |
| `public.crt`, `public.key` | Public hostname, only for `PUBLIC_TLS=provided` |

Keep the CA signing key off both VMs. Server keys must be unencrypted for
unattended container startup. With the recommended `PUBLIC_TLS=acme`, public
Caddy obtains and renews the website certificate; you do not supply the last
pair. In `provided` mode, the supplied CA bundle must also verify the public
certificate from the workstation.

### Optional: issue the private certificates on your workstation

If your deployment policy permits a dedicated private CA, this Bash example
uses OpenSSL 3+ to create one and issue the required certificates. It assumes
the `cyberguard.internal` zone above. If you chose other private names, use
them in the SANs and settings, but leave the internal Docker names
`identity-db` and `incident-db` unchanged.

```bash
(
  set -euo pipefail
  umask 077
  pki=$(mktemp -d "$HOME/cyberguard-pki.XXXXXX")
  tls="$pki/tls"
  mkdir "$tls"

  openssl req -x509 -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
    -sha256 -days 365 -subj '/CN=CyberGuard Deployment CA' \
    -addext 'basicConstraints=critical,CA:TRUE,pathlen:0' \
    -addext 'keyUsage=critical,keyCertSign,cRLSign' \
    -addext 'subjectKeyIdentifier=hash' \
    -keyout "$pki/ca.key" -out "$tls/ca.crt"

  issue_server() {
    local name=$1 dns=$2 sans=$3
    openssl req -new -newkey ec -pkeyopt ec_paramgen_curve:P-256 \
      -noenc -subj "/CN=$dns" -keyout "$tls/$name.key" -out "$pki/$name.csr"
    openssl x509 -req -in "$pki/$name.csr" -CA "$tls/ca.crt" -CAkey "$pki/ca.key" \
      -CAserial "$pki/ca.srl" -CAcreateserial -sha256 -days 90 \
      -extfile <(printf '%s\n' \
        'basicConstraints=critical,CA:FALSE' \
        'keyUsage=critical,digitalSignature' \
        'extendedKeyUsage=serverAuth' \
        'subjectKeyIdentifier=hash' \
        'authorityKeyIdentifier=keyid:always' \
        "subjectAltName=$sans") \
      -out "$tls/$name.crt"
  }

  issue_server identity identity-db 'DNS:identity-db'
  issue_server incident incident-db 'DNS:incident-db,DNS:incidents.cyberguard.internal'
  issue_server go app.cyberguard.internal 'DNS:app.cyberguard.internal'
  issue_server api ai.cyberguard.internal 'DNS:ai.cyberguard.internal'
  printf '\nSet TLS_DIR=%s\n' "$tls"
)
```

OpenSSL prompts for a passphrase for the CA signing key and again to sign
each certificate. Keep `ca.key` outside `TLS_DIR` and plan to renew the
90-day server certificates before they expire. Do not copy the files to the
VMs manually; the installer transfers only the files each host needs.

## 6. Fill in the workstation settings

From the repository root, copy the settings template and get the full
commit ID of the release you intend to install:

```bash
umask 077
cp -n deploy/remote/config.env.example "$HOME/cyberguard-remote.env"
git rev-parse HEAD
```

Edit the matching lines **in place**. Do not add duplicate keys. Replace
these examples with your own values; use plain `KEY=value`, without quotes:

```dotenv
APP_SSH=cyberguard-app
APP_KEY=
AI_SSH=cyberguard-ai
AI_KEY=
REVISION=REPLACE_WITH_FULL_COMMIT_ID
APP_PUBLIC_NAME=cyberguard.example.org
APP_PRIVATE_IP=10.0.0.4
AI_PRIVATE_IP=10.0.0.5
APP_GO_TLS_NAME=app.cyberguard.internal
APP_DB_TLS_NAME=incidents.cyberguard.internal
AI_TLS_NAME=ai.cyberguard.internal
TLS_DIR=/absolute/path/to/cyberguard-pki.ABCDEF/tls
PUBLIC_TLS=acme
NETWORK_APPROVED=yes
```

- Set `APP_PUBLIC_NAME` to the public hostname only, without `https://`.
  Set `TLS_DIR` to the bundle directory, not its parent containing `ca.key`.
- Leave the key fields blank when the SSH aliases select the correct keys.
  Relative key and certificate paths are relative to the settings file.
- Keep the Docker subnet settings unless they overlap your networks.
  `AI_INFERENCE_THREADS=all` uses all CPUs reported by Docker **on the AI VM**.
  Set a positive number to cap each model's thread count and CPU quota on
  revisions that support the setting.
- For a **synthetic demo only**, set `DEMO_SEED=yes` for four sign-in accounts.
  Optionally also set `DEMO_INCIDENTS=yes` to enable **Admin → Demo data**;
  installation itself does not add incidents. For real users, leave both
  settings off and plan to use the [owner initialization procedure](azure-app-deployment.md#initialize-the-application-owner).
  The installer does not create a real-user administrator.
- Set `NETWORK_APPROVED=yes` only after you have reviewed the Azure rules.
  Keep the original settings and pinned revision for future lifecycle
  commands and reset. Editing this file after installation does not update
  the deployment.

If you instead run `bash deploy/remote-setup.sh` without a settings file, it
prompts for the choices and writes `.local/remote-setup.env`. The explicit file
above lets you run preflight separately and override the example Docker subnets.

## 7. Run preflight, then install

```bash
bash deploy/remote-setup.sh --config "$HOME/cyberguard-remote.env" preflight
```

Preflight checks SSH, VM prerequisites, DNS, the TLS bundle, and existing
deployment state without changing either deployment. It does not prove
firewall denials or public ACME issuance. Once it passes, run:

```bash
bash deploy/remote-setup.sh --config "$HOME/cyberguard-remote.env" install
```

The installer uploads only the pinned Git revision, prepares both hosts,
provisions and migrates both databases, and downloads and verifies the pinned
AI models. The app VM retains its admin and migrator credentials. Only the
restricted AI database URL and worker token are streamed to the AI VM over
SSH. **Public writes remain paused until both stacks pass their checks.**

If installation fails, fix the reported issue and rerun `install` with the
same file; completed steps are recorded. On startup, the app VM gives Caddy
up to two minutes to obtain its public ACME certificate. If HTTPS still
fails, check the public DNS and challenge ports, and read the Caddy logs:

```bash
ssh cyberguard-app 'sudo -n docker logs --tail 100 cyberguard-app-vm-web-1'
```

Don't bypass TLS verification or reset just because the certificate is
pending. If the AI stage fails, HTTPS may already work even though public
writes are still paused.

## 8. Verify the deployment and sign in

When the installer prints `Ready at https://…/login`, open that URL and run
the non-mutating checks from your workstation:

```bash
bash deploy/remote-setup.sh --config "$HOME/cyberguard-remote.env" check
```

Verify the intended allowed and denied network paths from the relevant
hosts. The checks verify connectivity and TLS, not real inference. To
verify inference, submit a new synthetic report and confirm its assessment
completes in a staff session.

If you opted into demo accounts, retrieve their initial passwords in a
private terminal. The filenames may exist even if seeding was disabled;
in that case they are not valid logins.

```bash
ssh cyberguard-app '
  dir=/etc/cyberguard/app/secrets/credentials/provisioning
  for user in admin analyst employee_a employee_b; do
    printf "%s: " "$user"
    sudo -n cat "$dir/dev_${user}_password" || exit 1
  done
'
```

Use `admin` to open **Admin → Demo data** if enabled. Each click adds up to
ten unused reports from the demo catalog. The files above contain the
initial passwords, not any replacements changed in the app.

## Routine operations

Use `bash deploy/remote-setup.sh --config "$HOME/cyberguard-remote.env"`
followed by one of these commands:

| Command | Effect |
| --- | --- |
| `status` | Show both VM managers' service status. |
| `check` | Verify public HTTPS and both sides of AI dependency readiness without creating incidents. |
| `stop` | Stop AI before the app. Retains all data, credentials, models, and certificates; does not deallocate the VMs. |
| `resume` | Start a completed install without regenerating credentials, rebuilding images, applying migrations, downloading models, or changing maintenance mode. |
| `reset` | [Permanently erase a disposable deployment](#reset-a-disposable-deployment) after typed confirmation. |

## Reset a disposable deployment

Use `reset` only when nothing on the deployment needs to be retained. If you
changed deployment settings locally, restore the original settings for this
command or pass a separate copy of them. After the reset, you can use the new
settings to deploy again.

```bash
bash deploy/remote-setup.sh --config "$HOME/cyberguard-remote.env" reset
```

The command previews both target VMs and their scoped resources, then
requires you to type `RESET`. It removes the AI project first and the app
project second. This deletes the deployment’s containers, networks, database
volumes, Caddy certificate state, installed secrets and certificates,
deployment state, and downloaded model cache. It leaves Docker images and
build caches, uploaded release checkouts, Azure resources and networking,
SSH keys, and the workstation’s settings and TLS bundle intact.

To remove old manual state instead, use `reset --legacy` with the two SSH
targets in the settings file. It only handles the documented default project
names and paths. If a manual deployment used custom paths or project names,
clean those up separately. Do not run reset alongside the VM managers or
another deployment command. If reset is interrupted, resolve the error and
rerun the same reset command; already-cleared hosts are safe to retry. Reset
does not reinstall automatically: run `preflight` and `install` when ready.

For network changes, certificate rotation, upgrades, backups, and recovery,
use the [application VM runbook](azure-app-deployment.md) and
[AI VM runbook](azure-ai-deployment.md). The workstation installer does not
rotate files or apply upgrades to retained data. `make remote-setup-test`
checks SSH orchestration, reset scope, and file/step guards locally using mock
SSH targets; it does not deploy to Azure.
