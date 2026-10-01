# deploy-gate

GitHub Webhookから安全にローカルのデプロイスクリプトを実行するための、シンプルなGo製Webhookサーバーです。

`deploy-gate` はGitHub Webhookの署名を検証し、リクエストパスに応じて設定済みのローカルスクリプトを実行します。Webhook経由でDocker Socketを公開せずにデプロイを起動することを目的としています。

## 特徴

- GitHub HMAC-SHA256署名検証
- パスごとのデプロイルーティング
- `push` イベントのみデプロイ。対象ブランチの指定も可能
- スクリプトごとに直列実行し、実行中のリクエストは再実行1回にまとめる
- 実行中のデプロイを待つグレースフルシャットダウン
- Webhookのシークレットをスクリプトとログから保護
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
4. イベント種別とブランチで絞り込む
5. 対応するスクリプトを実行する（スクリプトごとに同時に1本まで）

実際のデプロイ処理は、各ルートに設定したスクリプト側で実装します。

## 動作要件

- Linux
- GitHub Webhook

Go（1.27以上）はソースからビルドする場合のみ必要です。ビルド済みバイナリを使用する場合、実行環境にGoは不要です。

## 設定

`deploy-gate` は環境変数とJSON設定ファイルで設定します。

### 環境変数

| 変数名          | 必須 | 説明                   |
| --------------- | ---- | ---------------------- |
| `DEPLOY_SECRET` | ○    | GitHub Webhook Secret  |
| `DEPLOY_CONFIG` | ○    | JSON設定ファイルのパス |
| `DEPLOY_SHUTDOWN_TIMEOUT` |  | 停止時に実行中のデプロイを待つ最大時間（Goのduration形式。例: `2m`）。デフォルト: `30s` |
| `DEPLOY_LOG_OUTPUT_BYTES` |  | ログに出力するスクリプト出力の末尾バイト数。`0` で出力をログに出さない。デフォルト: `4096` |

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

## GitHub Webhookの設定

リポジトリの **Settings > Webhooks** で以下のように設定します。

| 項目         | 値                                                                          |
| ------------ | --------------------------------------------------------------------------- |
| Payload URL  | `https://<ホスト>/<ルートのパス>`（例: `https://example.com/deploy/bot`）   |
| Content type | `application/json` または `application/x-www-form-urlencoded`（どちらも対応） |
| Secret       | `DEPLOY_SECRET` と同じ値                                                    |
| イベント     | **Just the push event** を推奨                                              |

保存するとGitHubから `ping` イベントが送信されます。`200` と `{"status":"pong"}` が返れば、署名の設定は正しく行われています。

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

### バイナリの配置

バイナリを配置・入れ替えるときは、`mv` や `cp` ではなく `install` を使ってください。

```bash
sudo install -o root -g root -m 755 deploy-gate /usr/local/bin/deploy-gate
sudo systemctl restart deploy-gate
```

- バイナリの所有者は `root` にしてください。サービスを実行するユーザー（`User=`）が所有していると、デプロイスクリプトが乗っ取られた場合に `deploy-gate` 本体まで書き換えられてしまいます
- `install` はファイルを新しく作成するため、配置先ディレクトリに合ったSELinuxラベルが付きます

### SELinux（RHEL、Rocky Linux、AlmaLinuxなど）

`mv` はSELinuxラベルを移動元のまま引き継ぎます。ホームディレクトリで展開したバイナリを `/usr/local/bin` に `mv` すると、`user_home_t` や `admin_home_t` のラベルが残り、systemdから起動できなくなります。

```text
deploy-gate.service: Failed to locate executable /usr/local/bin/deploy-gate: Permission denied
```

シェルから直接実行すると動くため、原因に気づきにくい点に注意してください。以下でラベルを確認し、元に戻します。

```bash
getenforce                                  # Enforcing ならSELinuxが有効
ls -Z /usr/local/bin/deploy-gate            # bin_t になっていればOK
sudo restorecon -v /usr/local/bin/deploy-gate
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
      DEPLOY_SHUTDOWN_TIMEOUT: ${DEPLOY_SHUTDOWN_TIMEOUT:-30s}
      DEPLOY_LOG_OUTPUT_BYTES: ${DEPLOY_LOG_OUTPUT_BYTES:-4096}

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
| 503    | 停止処理中のため、新しいデプロイを受け付けない                        |

スクリプトの実行結果はレスポンスではなくサーバーログに出力されます。

### 同時実行

同じスクリプトが同時に実行されることはありません（複数のルートが同じスクリプトを指している場合も同様です）。実行中に届いたリクエストは、終了後の再実行1回にまとめられます。実行が積み上がることはなく、最新のpushは必ずデプロイされます。

### スクリプトの出力とシークレット

- スクリプトに渡す環境変数から `DEPLOY_SECRET` を除外します。その他の環境変数はそのまま渡します
- スクリプトの出力に `DEPLOY_SECRET` の値が含まれる場合、`[REDACTED]` に置き換えます
- 出力は末尾の `DEPLOY_LOG_OUTPUT_BYTES` バイトのみ保持します。切り捨てた場合は `[... N bytes truncated ...]` と表示します
- 出力は1行ずつ、`[<スクリプト名>]` を付けてログに出力します

スクリプト内で使用するその他のシークレット（トークン、パスワードなど）は伏せ字にしません。出力しないようにし、それらを扱うスクリプトでは `set -x` を使わないでください。

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
├── .github/workflows/
│   ├── test.yml          # gofmt、vet、テスト、カバレッジ
│   ├── push-image.yml    # GHCRへのイメージ公開
│   └── release.yml       # タグ作成時のリリースバイナリ作成
├── cmd/
│   └── deploy-gate/
│       └── main.go       # 起動、シグナル処理、グレースフルシャットダウン
├── internal/
│   ├── config/
│   │   └── config.go     # 設定ファイルの読み込みと検証
│   ├── deploy/
│   │   ├── run.go        # スクリプト実行（プロセスグループ、出力上限、伏せ字化）
│   │   ├── serial.go     # スクリプトごとの直列実行と停止処理
│   │   └── logoutput.go  # スクリプト出力のログ出力
│   ├── signature/
│   │   └── hmac.go       # Webhook署名の検証
│   └── webhook/
│       └── deploy.go     # Webhookハンドラー（イベント・ブランチの絞り込み）
├── scripts/
│   └── deploy-example.sh.example
├── Dockerfile
├── compose.yml.example
├── config.json.example
└── go.mod
```

各パッケージには `_test.go` を同じディレクトリに置いています。

## セキュリティ

`deploy-gate` 自体はDocker Socketを必要としません。

Docker SocketをWebhook経由で公開すると、コンテナ操作やホストへのアクセスが可能となり、実質的にサーバーの管理権限を外部へ公開することになります。

`deploy-gate` は署名検証後、明示的に設定されたローカルスクリプトのみを実行します。スクリプトは小さく、監査しやすく、必要なデプロイ処理だけを行うようにしてください。

## ライセンス

MIT License
