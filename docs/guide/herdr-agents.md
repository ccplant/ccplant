# Pod内のHerdrエージェントと対話する

ACPの親エージェントとは別に、同じPod内のHerdrで起動した既存TUIを、セッション画面の **Pod内エージェント** から選択できます。新しいClaude/Piプロセスで履歴をresumeする機能ではありません。

## 有効化

この初期実装は **ccplant ACP runtime** 用です。セッション作成時の環境変数（または個人profileのenvironment）に次を指定します。

```json
{ "AGENTAPI_HERDR_ENABLED": "1" }
```

環境変数を省略したruntime、旧runtime image、従来のcoder/agentapiサーバーは404を返します。共有リンクからは利用できません。Herdr自体のインストール・起動は行いません。

**セッションごとにOSユーザー/HOMEが隔離されたPodでのみ有効化してください。** runtimeのOSユーザーが所有するHerdr socketが対象です。複数利用者のHerdrを同じHOMEに置くnative/shared-host構成で有効化してはいけません。

## 操作

1. セッション画面で「Pod内エージェント」を開く。
2. Herdr server名、pane/terminal、AI名、状態から対象を選ぶ。
3. 出力を確認し、他の人や管理AIと操作を調整する。
4. 入力とEnter送信の確認にチェックし、メッセージを送る。

開いている間だけ、可視ページで3秒間隔に一覧・選択先を更新します。出力はHTML/Markdownとして解釈しません。本文や入力をlocalStorageやアダプターのログへ保存しません。ただし、**元のエージェントやHerdrは通常どおり自身の会話・端末履歴を保存します**。

入力にはsession:createと対象ccplant sessionへのアクセス権、読み取りにはsession:readとアクセス権が必要です。任意socket、任意RPC、shell起動、kill、raw key、許可ダイアログの自動承認は公開しません。

## Herdrとherdr-core

| | 通常版Herdr | herdr-core (`hc`, agent image標準) |
|---|---|---|
| 発見 | config配下の既存`herdr.sock` | runtime/state配下の既存client socket、明示`HC_SOCKET` |
| 一覧 | `pane.list`の検出済みAI | `terminal.list`の既知AI app名またはforeground名 |
| 出力 | 最大150行のrecent-unwrapped | screenのみ、`lines:0`（TUIをスクロールしない） |
| 入力 | `pane.send_input`のtext＋Enter | `terminal.send`、`if:idle`、forceなし |

両者は別APIです。`hc run`は新規プロセス起動なので、`herdr pane run`の代わりには使用しません。接続するためにCLIを実行してserverを自動起動することもありません。リモートmachineのterminalは除外します。検出不能なAIやstatus未報告のプログラムをidleとして扱いません。

## 制限と安全性

- 出力は構造化された会話履歴ではなく、ターミナル画面です。特にhcは過去の全履歴を取得しません。
- working・blocked・unknown状態への送信は拒否します。許可待ちの操作や完全なブラウザterminal attachは対象外です。
- hcは`idle`かつ既知AIを直接起動した非keep terminalだけが入力対象です。`done`、shell/kb wrapper経由、keep/recovery対象は表示専用です。待機中にshellへ置き換わる対象には送信しません。通常Herdrではnative session metadataも必要です。
- ターゲットfingerprintを送信直前に再検証します。ただし、通常版Herdrの検証と入力は別RPCです。エージェント終了とのTOCTOUや、他クライアント/管理AIとの排他を完全には保証できません。**終端がshellに戻れば入力がコマンドになる危険**があります。
- 同一runtimeのWeb送信は1件ずつ処理します。他のHerdrクライアントはロックしません。
- hcは人間の未送信draftがあると待機する場合があります。ブラウザ切断後もruntimeは配送結果を監視し、完了まで追加入力を拒否します。timeout等で結果を確定できなければ入力をfail-closedで停止します。送信前に`$XDG_STATE_HOME/ccplant/herdr-input.pending`（既定`~/.local/state/ccplant/`）へ0600の空markerを作り、確実な応答後だけ削除するため、**bridge/runtime再起動でも解除されません**。自動再送・自動復旧はしません。
- 不明配送の復旧は、管理者が**対象terminalを確実に終了して旧送信を配送不能にするか、hc/holderを含むPod全体を破棄した後**に限ります。その後、残っているmarkerを管理者が削除してruntimeを再起動します。単なるbridge再起動や「75秒経過」は取消の証拠になりません。markerにはKB・メッセージ・秘密情報を保存しません。
- 同じnative agent ID/terminalを使い回す再起動やsessionの保存・復元は、完全なgeneration検知を保証しません。
- Unix socket通信はサイズ・時間制限付きです。stale serverがあっても残りのserverを列挙し、接続失敗数を表示します。
- APIは`/{sessionId}/herdr/{agents,output,input}`。OpenAPIは`backend/spec/openapi.json`を参照してください。

## ローカル確認

```sh
cd backend
CGO_ENABLED=1 go test -race ./pkg/herdr ./pkg/acp/bridge ./internal/interfaces/controllers

# 手動browser smoke専用fixture（loopbackのみ。Ctrl+Cで停止）
CCPLANT_HERDR_BROWSER_TEST=1 go test ./internal/interfaces/controllers \
  -run '^TestHerdrLocalBrowser$' -v -timeout 45m
```

別terminalでfrontendを次のように起動し、`http://127.0.0.1:3118/sessions/herdr-local`を開きます。

```sh
cd frontend
AGENTAPI_PROXY_URL=http://127.0.0.1:18381 bun run dev --hostname 127.0.0.1 --port 3118
```

frontendの通常認証も通すには、テスト専用の`COOKIE_ENCRYPTION_SECRET`（64桁hex）を設定し、隔離したlocal hostnameでダミーAPI key `ccplant-local-herdr-smoke-only` によりログインします。実際のGitHub/API credentialは使用しません。既存のログインcookieを共有するhostnameを避けてください。

このfixtureはccplantのsession proxy controller、ACP bridgeのHerdr routes、実際のHerdr socketを使用します。fixtureなのは認証済みowner identity、logical session lookup、親ACP metadataです（親モデルは起動せず、入れ子のAIは実プロセスです）。frontendのCookie/BFF経路はダミーtokenで通します。**認証基盤・Kubernetes provisioningを通す製品E2Eではありません。** テスト用AI paneだけを選び、実KBや秘密を送信しないでください。

### このPRでの実施記録

ローカルのNext.js画面をChromiumで操作し、Herdr 0.7.4内の独立したClaude Code 2.1.323（`--model haiku`、表示Haiku 5.5）2体で確認しました。

| 操作 | 結果 |
|---|---|
| 一覧からテストAI Aを選択 | 既存TUI画面を表示 |
| Webの入力欄から、分割した合成文字列の連結を質問 | Aの回答`WEB_HERDR_ROUNDTRIP_7319`がWeb出力へ反映 |
| 別のテストAI Bへ切り替え | Aの出力を消去し、入力同意のチェックを解除 |
| WebからBへ別の連結問題を送信 | Bの回答`WEB_SECOND_AGENT_8462`が反映 |
| Aへ戻る | Aの元の出力を再表示。Bの回答は混入なし |
| native transcriptをメタデータ検証 | 各々のassistant応答を確認。両方ともtool call 0、相手向けの回答なし |

質問文には完成形の識別子を入れていません。単に送信した文字列のechoを拾ったのではなく、実モデルのassistant応答を確認しています。Aは起動時と往復後で同じnative session IDを保持し、resume用の別プロセスは起動していません。KB本文・実credentialは使用していません。

関連Go 3パッケージのraceテスト、frontend関連48テスト、追加frontendファイルのESLint、差分のGo lintは通過しています。全体lintには基点でも再現する既存30件があり、TypeScript全体チェックには未変更の`vitest.config.ts`のVite plugin型不整合が残ります。**hc実機の75秒超の保留／強制終了、Kubernetes経由の製品E2E、マージ、デプロイは未実施**です。