# CLI の接続設定

`ccplant client` と `ccplant admin` は、`~/.ccplant/config.yaml` から API endpoint と API token を読み込めます。

```yaml
api_endpoint: https://ccplant.example.com
api_token: your-api-token
```

token をファイルへ直接保存せず、外部コマンドから取得することもできます。たとえば GitHub CLI の認証 token を使う場合は次のように設定します。

```yaml
api_endpoint: https://ccplant.example.com
api_token_helper:
  command: gh
  args:
    - auth
    - token
```

helper の標準出力から前後の空白と改行を除いた値を token として使います。helper は shell を介さず直接実行され、30 秒でタイムアウトします。`api_token` と `api_token_helper` の両方がある場合は `api_token` を使います。

設定値の優先順位は次のとおりです。

1. `--endpoint` / `--api-key`（その flag があるコマンドのみ）
2. `AGENTAPI_PROXY_ENDPOINT` または service 用環境変数 / `AGENTAPI_KEY`
3. `~/.ccplant/config.yaml`

token を直接保存する場合は、設定ディレクトリとファイルを自分だけが読めるようにしてください。

```console
chmod 700 ~/.ccplant
chmod 600 ~/.ccplant/config.yaml
```
