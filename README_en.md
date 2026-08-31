<div align="center">
  <img src="./Logo.png" alt="SimpleWebShell-RemoteUpdate" width="100" />
  <h2>SimpleWebShell-RemoteUpdate</h2>
  <h3>Non-Invasive Remote Update Workflows over SimpleWebShell</h3>
</div>

### 1. Introduction

- A standalone Go CLI that uses an existing [SimpleWebShell](../SimpleWebShell/) HTTP API for initial installation, version updates, symlink switching, health checks, automatic rollback and release cleanup.
- **Non-invasive**: it does not modify SimpleWebShell, install a resident agent, or require SSH. It only calls the existing `/post`, `/file_send` and Session APIs.
- Uses a fixed release layout and atomic symlink workflow for auditable deployment to Linux edge devices, Raspberry Pi, Jetson and regular servers.
- Pass the key through `SIMPLEWEBSHELL_KEY` to avoid storing it in config or shell history.

### 2. Fixed Workflow

```text
<app.root>/
├── current  -> releases/<current>[/install_subdir]
├── previous -> releases/<previous>[/install_subdir]
├── releases/
├── incoming/
└── .remoteupdate-app
```

Each `update` performs: probe and auth; initialize directories; stream upload; local and remote SHA-256 verification; extract into a new immutable release; optional pre-switch command; atomic `current` switch while saving `previous`; optional post-switch and health check; automatic rollback on failure; inactive release cleanup.

### 3. Commands

| Command | Purpose |
|---------|---------|
| `probe` | Verify SimpleWebShell connectivity and key |
| `init` | Initialize the remote layout without switching |
| `install` | Initial install; refuses to overwrite an existing current link |
| `update` | Upload, verify, install and activate a release |
| `status` | Print current / previous / releases as JSON |
| `rollback` | Roll back to previous or a named release |
| `cleanup` | Remove old inactive releases |

### 4. Quick Start

```bash
make build
cp remoteupdate.example.json remoteupdate.json
export SIMPLEWEBSHELL_KEY='your-key'

./simplewebshell-remoteupdate -config remoteupdate.json probe
./simplewebshell-remoteupdate -config remoteupdate.json install -artifact ./app-v1.tar.gz -release v1
./simplewebshell-remoteupdate -config remoteupdate.json update -artifact ./app-v2.tar.gz -release v2
./simplewebshell-remoteupdate -config remoteupdate.json status
./simplewebshell-remoteupdate -config remoteupdate.json rollback
```

Review commands without changing the remote host:

```bash
./simplewebshell-remoteupdate -config remoteupdate.json -dry-run update \
  -artifact ./app-v2.tar.gz -release v2
```

### 5. Configuration

See `remoteupdate.example.json`.

| Setting | Description |
|---------|-------------|
| `remote.url` | Existing SimpleWebShell URL |
| `remote.timeout` | Per-HTTP-operation timeout, default `10m` |
| `remote.session_enabled` | Create and delete an isolated temporary Session |
| `app.root` | Absolute remote application root |
| `artifact_type` | `tar.gz`, `tgz`, `tar`, `zip`, or `file` |
| `install_subdir` | Safe relative directory targeted by current after extraction |
| `pre_switch` | Command before symlink switch |
| `post_switch` | Start/restart command after switch |
| `health_check` | Non-zero exit causes activation failure |
| `rollback` | Recovery command after restoring the old link |
| `keep_releases` | Number of inactive releases to retain |
| `require_health_check` | Require a configured health check |
| `disable_auto_rollback` | Rollback is on by default; set true to disable |

### 6. Service Integration

Point systemd at the stable symlink:

```ini
ExecStart=/opt/my-service/current/my-service
```

Then configure `post_switch` and `rollback` to restart the service, and `health_check` to verify it.

### 7. Docker

```bash
docker build -t simplewebshell-remoteupdate:latest .
docker run --rm \
  -e SIMPLEWEBSHELL_KEY="$SIMPLEWEBSHELL_KEY" \
  -v "$PWD:/work" -w /work \
  simplewebshell-remoteupdate:latest \
  -config remoteupdate.json status
```

No privileged mode is required. The container only runs the local CLI and reaches SimpleWebShell over the network.

### 8. Security and Scope

- Use only on authorized hosts; this tool has the same remote command authority as the SimpleWebShell key.
- Prefer VPN/private networks or an HTTPS reverse proxy. Do not expose an unencrypted WebShell directly to the Internet.
- Release names are restricted and all remote paths are POSIX-shell quoted.
- Lifecycle commands are trusted administrator configuration.
- The symlink workflow targets Unix/Linux, not Windows.
- SimpleWebShell currently limits a single command to 60 seconds. Use a service manager such as systemd for long-running processes.

### 9. Directory Structure

```text
SimpleWebShell-RemoteUpdate/
├── main.go
├── internal/config/
├── internal/webshell/
├── internal/workflow/
├── remoteupdate.example.json
├── Dockerfile
├── Makefile
├── LICENSE
├── README.md
└── README_en.md
```

### 10. License

Apache License 2.0
