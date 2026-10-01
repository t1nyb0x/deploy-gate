# deploy-gate

A small Go-based webhook server for safely triggering local deployment scripts from GitHub Webhooks.

`deploy-gate` verifies GitHub webhook signatures and executes configured local scripts based on the request path. It is designed for environments where deployments should be triggered without exposing the Docker Socket through a webhook endpoint.

## Features

- GitHub HMAC-SHA256 signature verification
- Path-based deployment routing
- Deploys only on `push` events, optionally limited to a branch
- Serialized deploys per script, with queued requests coalesced into one run
- Graceful shutdown that waits for running deploys
- Keeps the webhook secret out of scripts and logs
- Configurable script execution
- Single binary deployment
- Uses only the Go standard library
- No Docker Socket access required by `deploy-gate` itself

## Architecture

```text
GitHub Webhook
      │
      ▼
 deploy-gate
      │
      ├─ /deploy/bot       → deploy-bot.sh
      │
      └─ /deploy/dashboard → deploy-dashboard.sh
```

`deploy-gate` is responsible for:

1. Receiving webhook requests
2. Verifying GitHub signatures
3. Selecting a configured route
4. Filtering by event type and branch
5. Executing the configured script, one run at a time per script

Actual deployment logic should be implemented in the script invoked by each route.

## Requirements

- Linux
- GitHub Webhooks

Go 1.27 or later is only required when building from source. Prebuilt binaries can be distributed through GitHub Releases.

## Configuration

`deploy-gate` uses environment variables and a JSON configuration file.

### Environment variables

| Variable        | Required | Description                                           |
| --------------- | -------- | ----------------------------------------------------- |
| `DEPLOY_SECRET` | Yes      | GitHub Webhook secret used for signature verification |
| `DEPLOY_CONFIG` | Yes      | Path to the JSON configuration file                   |
| `DEPLOY_SHUTDOWN_TIMEOUT` | No | Max time to wait for running deploys on shutdown (Go duration, e.g. `2m`). Default: `30s` |
| `DEPLOY_LOG_OUTPUT_BYTES` | No | Number of trailing bytes of script output written to the log. `0` disables output logging. Default: `4096` |

Example:

```env
DEPLOY_SECRET=replace_me
DEPLOY_CONFIG=/etc/deploy-gate/config.json
```

### Config file

Example:

```json
{
  "routes": [
    {
      "path": "/deploy/bot",
      "script": "/opt/deploy-gate/scripts/deploy-bot.sh",
      "branch": "main"
    },
    {
      "path": "/deploy/dashboard",
      "script": "/opt/deploy-gate/scripts/deploy-dashboard.sh",
      "branch": "main"
    }
  ]
}
```

Each route maps an HTTP path to a local script.

The script path must be an absolute path.

| Field    | Required | Description                                                                                          |
| -------- | -------- | ---------------------------------------------------------------------------------------------------- |
| `path`   | Yes      | HTTP path to receive the webhook on. Must start with `/`                                              |
| `script` | Yes      | Absolute path of the script to run                                                                   |
| `branch` | No       | Only deploy on pushes to this branch (e.g. `main`). If omitted, pushes to any branch trigger a deploy |

Setting `branch` is strongly recommended. Specify the branch name only (`main`, not `refs/heads/main`); a value starting with `refs/` is rejected at startup. Only `push` events trigger a deploy; `ping` and other events are acknowledged but ignored, and branch deletions are always ignored.

## GitHub Webhook settings

Configure the webhook in your repository under **Settings > Webhooks**:

| Setting      | Value                                                                  |
| ------------ | ---------------------------------------------------------------------- |
| Payload URL  | `https://<your-host>/<route path>` (e.g. `https://example.com/deploy/bot`) |
| Content type | `application/json` or `application/x-www-form-urlencoded` (both supported) |
| Secret       | Same value as `DEPLOY_SECRET`                                          |
| Events       | **Just the push event** is recommended                                 |

After saving, GitHub sends a `ping` event. A `200` response with `{"status":"pong"}` confirms that the signature is valid.

## Build

```bash
go build -o bin/deploy-gate ./cmd/deploy-gate
```

## Run

```bash
DEPLOY_SECRET=replace_me \
DEPLOY_CONFIG=/etc/deploy-gate/config.json \
./bin/deploy-gate
```

The server listens on `:9000` by default.

## systemd example

```ini
[Unit]
Description=deploy-gate
After=network.target

[Service]
Type=simple
Environment=DEPLOY_SECRET=replace_me
Environment=DEPLOY_CONFIG=/etc/deploy-gate/config.json
ExecStart=/usr/local/bin/deploy-gate
Restart=always
RestartSec=3
# Send SIGTERM only to deploy-gate so it can wait for running deploys.
# The default (control-group) would also terminate the deploy script immediately.
KillMode=mixed
# Keep this longer than DEPLOY_SHUTDOWN_TIMEOUT.
TimeoutStopSec=60

[Install]
WantedBy=multi-user.target
```

### Installing the binary

Use `install` instead of `mv` or `cp` when placing or replacing the binary:

```bash
sudo install -o root -g root -m 755 deploy-gate /usr/local/bin/deploy-gate
sudo systemctl restart deploy-gate
```

- Keep the binary owned by `root`. If it is owned by the service user (`User=`), a compromised deploy script could overwrite `deploy-gate` itself
- `install` creates a new file, so it gets the SELinux label of the destination directory

### SELinux (RHEL, Rocky Linux, AlmaLinux, etc.)

`mv` keeps the SELinux label from the source directory. A binary extracted in a home directory and moved into `/usr/local/bin` keeps a label such as `user_home_t` or `admin_home_t`, and systemd fails to start it:

```text
deploy-gate.service: Failed to locate executable /usr/local/bin/deploy-gate: Permission denied
```

The binary runs fine from a shell, which makes this easy to miss. Check the label and restore it:

```bash
getenforce                                  # Enforcing means SELinux is active
ls -Z /usr/local/bin/deploy-gate            # should be bin_t
sudo restorecon -v /usr/local/bin/deploy-gate
```

## Docker

Docker can be used when the configured scripts can run inside the container.

Example:

```bash
cp compose.yml.example compose.yml
cp config.json.example config.json
echo 'DEPLOY_SECRET=replace_me' > .env
mkdir -p scripts
```

Example compose.yml:

```yaml
services:
  deploy-gate:
    image: ghcr.io/t1nyb0x/deploy-gate:latest
    container_name: deploy-gate
    restart: unless-stopped

    environment:
      DEPLOY_SECRET: ${DEPLOY_SECRET}
      DEPLOY_CONFIG: /etc/deploy-gate/config.json
      DEPLOY_SHUTDOWN_TIMEOUT: ${DEPLOY_SHUTDOWN_TIMEOUT:-30s}
      DEPLOY_LOG_OUTPUT_BYTES: ${DEPLOY_LOG_OUTPUT_BYTES:-4096}

    volumes:
      - ./config.json:/etc/deploy-gate/config.json:ro
      - ./scripts:/scripts:ro

    ports:
      - "9000:9000"

    # Keep this longer than DEPLOY_SHUTDOWN_TIMEOUT (Docker's default is 10s).
    stop_grace_period: 60s
```

Example config.json:

```json
{
  "routes": [
    {
      "path": "/deploy/example",
      "script": "/scripts/deploy-example.sh",
      "branch": "main"
    }
  ]
}
```

`deploy-gate` itself does not require Docker Socket access.

If your deployment script needs to control Docker on the host, consider running `deploy-gate` as a host-level systemd service instead of mounting the Docker Socket into the container.

## API

### POST configured route

Accepts webhook requests from GitHub.

Example:

```text
POST /deploy/bot
POST /deploy/dashboard
```

Signature header:

```http
X-Hub-Signature-256: sha256=<signature>
```

Responses:

| Status | Description                                                      |
| ------ | ---------------------------------------------------------------- |
| 202    | Accepted; the script runs in the background (`{"status":"accepted"}`) |
| 202    | A deploy of the same script is in progress; one follow-up run is queued (`{"status":"queued"}`) |
| 200    | `ping` (`{"status":"pong"}`) or ignored event/branch (`{"status":"ignored"}`) |
| 400    | Malformed push payload                                           |
| 403    | Invalid method or signature                                      |
| 503    | Shutting down; no new deploys are accepted                       |

The script result is written to the server log, not returned in the response.

### Concurrency

Each script runs at most one at a time, even when several routes point to the same script. Requests that arrive while a deploy is running are coalesced into a single follow-up run, so the latest push is always deployed without piling up runs.

### Script output and secrets

- `DEPLOY_SECRET` is removed from the environment passed to scripts. Other environment variables are passed through
- If the value of `DEPLOY_SECRET` appears in script output, it is replaced with `[REDACTED]`
- Only the last `DEPLOY_LOG_OUTPUT_BYTES` bytes of output are kept. Truncation is marked as `[... N bytes truncated ...]`
- Each output line is logged with a `[<script name>]` prefix

Other secrets used by your scripts (tokens, passwords, etc.) are not redacted. Avoid printing them, and avoid `set -x` in scripts that handle them.

## Graceful Shutdown

On `SIGTERM` or `SIGINT`, `deploy-gate`:

1. Stops accepting new webhook requests
2. Drops any queued follow-up deploy (a warning is logged)
3. Waits for running deploys to finish, up to `DEPLOY_SHUTDOWN_TIMEOUT`
4. Kills deploys still running after the timeout, including their child processes

Each script runs in its own process group, so its child processes (e.g. `docker compose`) are killed together.

Make sure the process manager waits longer than `DEPLOY_SHUTDOWN_TIMEOUT` before force-killing (`TimeoutStopSec` for systemd, `stop_grace_period` for Docker Compose). With systemd, set `KillMode=mixed` as shown above.

## Project Structure

```text
deploy-gate/
├── .github/workflows/
│   ├── test.yml          # gofmt, vet, tests, coverage
│   ├── push-image.yml    # Publish image to GHCR
│   └── release.yml       # Build release binary on tags
├── cmd/
│   └── deploy-gate/
│       └── main.go       # Startup, signal handling, graceful shutdown
├── internal/
│   ├── config/
│   │   └── config.go     # Config file loading and validation
│   ├── deploy/
│   │   ├── run.go        # Script execution (process group, output limit, redaction)
│   │   ├── serial.go     # Per-script serialization and shutdown
│   │   └── logoutput.go  # Script output logging
│   ├── signature/
│   │   └── hmac.go       # Webhook signature verification
│   └── webhook/
│       └── deploy.go     # Webhook handler (event and branch filtering)
├── scripts/
│   └── deploy-example.sh.example
├── Dockerfile
├── compose.yml.example
├── config.json.example
└── go.mod
```

Each package has `_test.go` files next to it.

## Security

`deploy-gate` does not require direct access to the Docker Socket.

Exposing the Docker Socket through a webhook endpoint effectively grants remote control over containers and, in many cases, the host system itself.

`deploy-gate` only verifies webhook signatures and executes explicitly configured local scripts. Keep scripts small, auditable, and restricted to the deployment actions they need to perform.

## License

MIT License
