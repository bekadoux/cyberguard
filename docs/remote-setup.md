# Two-VM Azure setup from a workstation

`deploy/remote-setup.sh` coordinates the existing Azure application and AI
deployments over SSH. It does not replace either VM manager, create VMs, edit
DNS/firewall policy, issue private certificates, or adopt a manual deployment.

## Before you start

- Use a reviewed, committed checkout containing the installer. The workstation
  needs Linux, Bash 4.4+, OpenSSH, Git, OpenSSL, curl, `getent`, and `flock`. It
  does not need Docker, Go, Node.js, or Python.
- Prepare two distinct Ubuntu 26.04 amd64 Azure VMs with at least 20 GiB free
  under `/var` on each. The installer uses their usual non-root operators with
  working `sudo -n` (non-interactive sudo). It does not edit sudoers or SSH
  configuration. Use fresh CyberGuard hosts; it refuses to adopt an existing
  installation or recreate a missing provisioned database volume.
- Configure the [required DNS and network policy](azure-app-deployment.md#3-networking-and-certificates-are-explicit-prerequisites).
  The assisted path uses the standard ports: public application `80`/`443`,
  private application Go `8443`, private incident PostgreSQL `5432`, and
  private AI gateway `8443`. Keep each private listener limited to its approved
  peer VM. The application Docker subnets must not overlap your networks.
- Prepare an approved certificate bundle on the workstation, outside Git.
  The installer checks exact DNS SANs, trust chains, matching keys, and at
  least 24 hours of leaf validity. It expects these filenames:

  | Files | Required SANs or purpose |
  | --- | --- |
  | `ca.crt` | Trusted CA bundle for the private endpoints |
  | `identity.crt`, `identity.key` | `DNS:identity-db` |
  | `incident.crt`, `incident.key` | `DNS:incident-db` and the private incident database name |
  | `go.crt`, `go.key` | Private application Go name |
  | `api.crt`, `api.key` | Private AI gateway name |
  | `public.crt`, `public.key` | Public hostname; only for `PUBLIC_TLS=provided` |

  Server keys must be unencrypted for unattended container startup. **This is
  separate from workstation SSH keys, which may have passphrases.** Keep the
  CA signing key off both VMs. For normal internet-facing deployments,
  `PUBLIC_TLS=acme` lets public Caddy obtain and renew its own certificate.
  Provided mode uses the supplied CA bundle for the workstation HTTPS check
  as well.

### SSH keys

Verify each VM's SSH host key out of band and add it to your usual known-hosts
configuration before running setup. The installer rejects unknown or changed
host keys. It uses public-key authentication and never copies identity files,
prompts for their passphrases itself, or forwards your SSH agent.

You can use two existing SSH aliases, or define them like this:

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

Alternatively, enter each `user@host` and its own key path at setup
(`APP_KEY` and `AI_KEY` in a settings file). This also works for two
passphrase-free keys. In an interactive terminal, OpenSSH can unlock each
protected key when opening its connection. The installer reuses one connection
per VM for subsequent commands and transfers. For unattended use, load
protected keys into your workstation agent with `ssh-add` first; a batch run
fails rather than waiting for input.

## Install and operate

From the repository root on your workstation:

```bash
bash deploy/remote-setup.sh
```

The first run prompts for the two SSH endpoints, private IPs, DNS names,
certificate directory, the AI inference thread limit, and approval of the network
policy. It saves literal, non-secret settings in `.local/remote-setup.env`,
including the Git revision.
For unattended use or to override the default Docker subnets, make a private
copy of [`deploy/remote/config.env.example`](../deploy/remote/config.env.example)
and run `bash deploy/remote-setup.sh --config /path/to/settings.env install`.
Set the full `REVISION` from `git rev-parse HEAD` to keep future commands pinned
even if your checkout changes. Relative certificate and key paths in the
settings file are relative to that file.

| Command after `bash deploy/remote-setup.sh` | Effect |
| --- | --- |
| `preflight` | Verify saved inputs, certificate files, SSH, basic VM requirements and DNS without changing VM deployment state. |
| `install` | Upload only the selected Git revision, prepare both hosts, provision app databases and the restricted AI role, and install the pinned models. Rerun this to continue a failed first install. |
| `resume` | Start a completed install without regenerating credentials, rebuilding images, migrating, downloading models, or changing maintenance mode. |
| `status` | Show both VM managers' service status. |
| `check` | Verify public HTTPS and both sides of AI dependency readiness without creating test incidents. |
| `stop` | Stop the AI VM stack before the app VM stack. Retain all data, state, credentials, models, and certificates; do not deallocate either VM. |

Use the same `--config FILE` before any subcommand if you used a custom file.
The initial install leaves public writes paused until both stacks pass their
checks. If the AI stage fails, the application HTTPS endpoint can still come
up, but the installer does not complete initial cutover. Ordinary starts do
not turn off an operator's maintenance pause. Existing VM receipts protect
finished steps and tie the install to the same two hosts, operators, topology,
and committed revision. The uploaded checkout is kept at a stable path under
each operator's `~/.local/share/cyberguard-remote/releases/`.

The installer copies only the required TLS files for each host. It streams the
restricted AI database URL and shared worker token from the application VM to
the AI VM over SSH without printing them or writing workstation copies.
Application admin and migrator credentials stay on the application VM.
`DEMO_SEED=yes` explicitly enables the four development accounts for a
synthetic demo; their initial passwords are in the protected application VM
provisioning directory. The installer does not provision a real-user
administrator account or print any passwords. If you choose to seed the accounts,
the installer also offers to enable **Admin → Add 10 demo incidents**. It defaults
to No and saves the choice. For an unattended fresh install, set
`DEMO_INCIDENTS=yes` alongside `DEMO_SEED=yes`. The installer only enables the
button; it does not add any incidents. Do not edit the setting for an existing
remote install: the receipts bind it to its original configuration.

`AI_INFERENCE_THREADS=all` is the default for revisions that support the setting.
It uses all CPUs reported by Docker **on the AI VM**, not the workstation. Enter
a positive integer to limit each model's thread count and CPU quota to at most
that number and leave room for other services. A numeric limit requires a
supporting revision; older pinned releases retain their original defaults.
The choice is for fresh installs. For an installed deployment, follow the
[AI VM upgrade runbook](azure-ai-deployment.md#upgrading-an-existing-deployment)
instead of editing the workstation settings and rerunning the installer.

Preflight does not prove Azure NSG denial or public certificate issuance.
The final checks verify real connectivity with TLS, including from the AI
runtime, but don't prove inference. For acceptance, test allowed and denied
network paths from the relevant hosts, then submit a new synthetic incident
and check its real assessment as staff.

For network changes, certificate rotation, upgrades, backups, and recovery,
use the [application VM runbook](azure-app-deployment.md) and
[AI VM runbook](azure-ai-deployment.md). The workstation installer does not
rotate files, apply upgrades to retained data, or provide a destructive
teardown command. `make remote-setup-test` checks SSH orchestration and the
file/step guards locally using mock SSH targets; it does not deploy to Azure.
