# deploy-gate

GitHub Webhookから安全にローカルのデプロイスクリプトを実行するための、シンプルなGo製Webhookサーバーです。

`deploy-gate` はGitHub Webhookの署名を検証し、リクエストパスに応じて設定済みのローカルスクリプトを実行します。Webhook経由でDocker Socketを公開せずにデプロイを起動することを目的としています。

## 特徴

- GitHub HMAC-SHA256署名検証
- パスごとのデプロイルーティング
- 設定ファイルによるスクリプト指定
- 単一バイナリで動作
- 標準ライブラリのみ使用
- `deploy-gate` 自体はDocker Socket不要

## アーキテクチャ

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

`deploy-gate` の責務は以下です。

1. Webhookを受信する
2. GitHub署名を検証する
3. 設定済みのルートを選択する
4. 対応するスクリプトを実行する

実際のデプロイ処理は、各ルートに設定したスクリプト側で実装します。

## 動作要件

- Linux
- GitHub Webhook

Goはソースからビルドする場合のみ必要です。ビルド済みバイナリを使用する場合、実行環境にGoは不要です。

## 設定

`deploy-gate` は環境変数とJSON設定ファイルで設定します。

### 環境変数

| 変数名          | 必須 | 説明                   |
| --------------- | ---- | ---------------------- |
| `DEPLOY_SECRET` | ○    | GitHub Webhook Secret  |
| `DEPLOY_CONFIG` | ○    | JSON設定ファイルのパス |
| `DEPLOY_SHUTDOWN_TIMEOUT` |  | 停止時に実行中のデプロイを待つ最大時間（Goのduration形式。例: `2m`）。デフォルト: `30s` |

例:

```env
DEPLOY_SECRET=replace_me
DEPLOY_CONFIG=/etc/deploy-gate/config.json
```

### 設定ファイル

例:

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

各ルートで、HTTPパスと実行するローカルスクリプトを対応付けます。

スクリプトのパスは絶対パスで指定する必要があります。

| フィールド | 必須 | 説明                                                                              |
| ---------- | ---- | --------------------------------------------------------------------------------- |
| `path`     | ○    | Webhookを受け付けるHTTPパス。`/` で始める                                         |
| `script`   | ○    | 実行するスクリプトの絶対パス                                                      |
| `branch`   |      | このブランチへのpushのみデプロイする（例: `main`）。省略時は全ブランチでデプロイ |

`branch` の指定を強く推奨します。値はブランチ名のみを指定してください（`refs/heads/main` ではなく `main`）。`refs/` で始まる値は起動時にエラーになります。デプロイされるのは `push` イベントのみです。`ping` やその他のイベントは受け付けますが無視し、ブランチ削除のpushも常に無視します。

## ビルド

```bash
go build -o bin/deploy-gate ./cmd/deploy-gate
```

## 実行

```bash
DEPLOY_SECRET=replace_me \
DEPLOY_CONFIG=/etc/deploy-gate/config.json \
./bin/deploy-gate
```

起動後は `:9000` で待ち受けます。

## systemd設定例

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
# SIGTERMをdeploy-gate本体にのみ送り、実行中のデプロイを待てるようにする。
# デフォルト（control-group）ではデプロイスクリプトも即座に終了してしまう。
KillMode=mixed
# DEPLOY_SHUTDOWN_TIMEOUT より長くする
TimeoutStopSec=60

[Install]
WantedBy=multi-user.target
```

## Docker

設定したスクリプトがコンテナ内で完結する場合、Dockerで実行できます。

例:

```bash
cp compose.yml.example compose.yml
cp config.json.example config.json
echo 'DEPLOY_SECRET=replace_me' > .env
mkdir -p scripts
```

compose.yml の例:

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

    # DEPLOY_SHUTDOWN_TIMEOUT より長くする（Dockerのデフォルトは10s）
    stop_grace_period: 60s
```

config.json の例:

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

`deploy-gate` 自体はDocker Socketを必要としません。

デプロイスクリプトからホストのDockerを操作したい場合は、Docker Socketをコンテナへマウントするより、`deploy-gate` をsystemdサービスとしてホスト上で実行する構成を検討してください。

## API

### POST 設定済みルート

GitHub Webhookから送信されるリクエストを受け付けます。

例:

```text
POST /deploy/bot
POST /deploy/dashboard
```

署名ヘッダ:

```http
X-Hub-Signature-256: sha256=<signature>
```

レスポンス:

| Status | Description                                                           |
| ------ | --------------------------------------------------------------------- |
| 202    | 受付済み。スクリプトはバックグラウンドで実行（`{"status":"accepted"}`） |
| 202    | 同じスクリプトが実行中のため、終了後の再実行を1回予約（`{"status":"queued"}`） |
| 200    | `ping`（`{"status":"pong"}`）、または対象外のイベント・ブランチ（`{"status":"ignored"}`） |
| 400    | pushペイロードが不正                                                  |
| 403    | メソッド不正または署名不正                                            |

スクリプトの実行結果はレスポンスではなくサーバーログに出力されます。

同じスクリプトが同時に実行されることはありません（複数のルートが同じスクリプトを指している場合も同様です）。実行中に届いたリクエストは、終了後の再実行1回にまとめられます。実行が積み上がることはなく、最新のpushは必ずデプロイされます。

## グレースフルシャットダウン

`SIGTERM` または `SIGINT` を受け取ると、`deploy-gate` は以下の順に停止します。

1. 新しいWebhookリクエストの受け付けを停止する
2. 予約済みの再実行を破棄する（警告ログを出力）
3. 実行中のデプロイの完了を `DEPLOY_SHUTDOWN_TIMEOUT` まで待つ
4. タイムアウト後も実行中のデプロイは、子プロセスごと強制終了する

スクリプトは独立したプロセスグループで実行されるため、スクリプトから起動した子プロセス（`docker compose` など）もまとめて終了します。

プロセスマネージャーが強制終了するまでの待ち時間は、`DEPLOY_SHUTDOWN_TIMEOUT` より長く設定してください（systemdは `TimeoutStopSec`、Docker Composeは `stop_grace_period`）。systemdでは上記の例のとおり `KillMode=mixed` を設定してください。

## プロジェクト構成

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

## セキュリティ

`deploy-gate` 自体はDocker Socketを必要としません。

Docker SocketをWebhook経由で公開すると、コンテナ操作やホストへのアクセスが可能となり、実質的にサーバーの管理権限を外部へ公開することになります。

`deploy-gate` は署名検証後、明示的に設定されたローカルスクリプトのみを実行します。スクリプトは小さく、監査しやすく、必要なデプロイ処理だけを行うようにしてください。

## ライセンス

MIT License
