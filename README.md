# deploy-gate

A small Go-based webhook server for safely triggering local deployment scripts from GitHub Webhooks.

`deploy-gate` verifies GitHub webhook signatures and executes configured local scripts based on the request path. It is designed for environments where deployments should be triggered without exposing the Docker Socket through a webhook endpoint.

## Features

- GitHub HMAC-SHA256 signature verification
- Path-based deployment routing
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
4. Executing the configured script

Actual deployment logic should be implemented in the script invoked by each route.

## Requirements

- Linux
- GitHub Webhooks

Go is only required when building from source. Prebuilt binaries can be distributed through GitHub Releases.

## Configuration

`deploy-gate` uses environment variables and a JSON configuration file.

### Environment variables

| Variable        | Required | Description                                           |
| --------------- | -------- | ----------------------------------------------------- |
| `DEPLOY_SECRET` | Yes      | GitHub Webhook secret used for signature verification |
| `DEPLOY_CONFIG` | Yes      | Path to the JSON configuration file                   |

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

[Install]
WantedBy=multi-user.target
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

    volumes:
      - ./config.json:/etc/deploy-gate/config.json:ro
      - ./scripts:/scripts:ro

    ports:
      - "9000:9000"
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

The script result is written to the server log, not returned in the response.

Each script runs at most one at a time, even when several routes point to the same script. Requests that arrive while a deploy is running are coalesced into a single follow-up run, so the latest push is always deployed without piling up runs.

## Project Structure

```text
deploy-gate/
├── cmd/
│   └── deploy-gate/
│       └── main.go
├── internal/
│   ├── config/
│   │   └── config.go
│   ├── deploy/
│   │   └── run.go
│   ├── signature/
│   │   └── hmac.go
│   └── webhook/
│       └── deploy.go
├── go.mod
└── README.md
```

## Security

`deploy-gate` does not require direct access to the Docker Socket.

Exposing the Docker Socket through a webhook endpoint effectively grants remote control over containers and, in many cases, the host system itself.

`deploy-gate` only verifies webhook signatures and executes explicitly configured local scripts. Keep scripts small, auditable, and restricted to the deployment actions they need to perform.

## License

MIT License
