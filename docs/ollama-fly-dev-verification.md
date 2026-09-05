# Fly 開発環境での Ollama 検証

2026-09-05 UTC に PR #229 の実装コミット `c13de65ab4e0aa06f87ac1b45a6973d19d1d7fb4` をデプロイして検証した。

- UI: https://dev.ccplant.com
- API: https://ccplant-api-dev.fly.dev
- デプロイ: https://github.com/ccplant/ccplant-deploy/actions/runs/33934398133 （全ジョブ成功）
- API image: `ghcr.io/ccplant/ccplant-api@sha256:c9485cd657dbb4429b8342ffadb078422fec6c61696c70fbd469be09281584db`
- 開発ランナー: namespace `ccplant-session-dev`、Helm release `ccplant-session`、revision 29、chart `0.3.0-dev.ccplant.c13de65`。セッションイメージも同じコミットに更新。
- イメージ内 CLI: `codex-cli 0.153.4`、`Claude Code 2.1.12`。

既存ユーザーの設定を変更せず、開発用 bootstrap ユーザーの一時設定でテストした。Ollama Cloud の既存 API キーを使用し、実値は記録していない。

| 検証 | 結果 |
| --- | --- |
| Ollama `https://ollama.com/v1/responses` | HTTP 200、指定した確認文字列を返した |
| Ollama `https://ollama.com/v1/messages` | HTTP 200、指定した確認文字列を返した |
| UI のログインと設定取得 | 成功。デプロイされた Codex 設定ページの JS に互換 API フォームを確認 |
| 認証設定の保存・再取得 | 両方式を保存でき、API キー実値を返さず `has_api_key: true` を返した |
| Codex + API キー | `CCPLANT_CODEX_OLLAMA_OK` を返した |
| Claude ACP + Bearer | `CCPLANT_CLAUDE_OLLAMA_OK` を返した |
| プロファイル上書き | 設定の `gpt-oss:120b` に対し、両エージェントの起動モデルが `gpt-oss:20b` になった |
| デフォルト継承 | プロファイルなしの Codex が `gpt-oss:120b` で起動し `CCPLANT_DEFAULT_MODEL_OK` を返した |
| ツール実行 | 両エージェントが `/tmp` に確認用ファイルを作成して読み取った。Pod 内からも内容を照合した |
| 非選択の認証 | Codex の `auth.json` と Claude の `.credentials.json` が検証 Pod に存在しないことを確認 |

継続メッセージは UI と同じ `/{sessionId}/rpc` 経路、結果は `/{sessionId}/messages` で確認した。Claude ACP の `/session` はモデル名を別名 `opus` と表示したが、起動時の接続記録と別名の設定先は `gpt-oss:20b` だった。

## 制限と観測された問題

- Codex は独自モデルの metadata が未登録という警告を表示した。`gpt-oss:20b` のツール実行後の文章には制御文字列に似た余計な出力もあった。
- Claude の `gpt-oss:20b` は Read ツールに不要な引数を付けて失敗した後、自分で修正して読み取りに成功した。接続確認は、モデルの出力品質や全ツールの互換性を保証するものではない。
- Claude の `x-api-key` 方式、ローカル Ollama、再開、長時間実行は今回の実機検証に含めていない。今回の Claude 実機検証は Bearer 方式。
- 既存のトップレベル `/acp` 経路は外部ランナーのセッションに対して `session not found` を返した。UI が利用するセッション別 `/rpc` は正常だった。
- 公開セッション削除 API から manager への処理は HTTP 401 になったため、認証済みの private session-manager API で検証用の実行環境を削除した。公開 API の3件の記録には `delete_failed` が残る。対象 ID は `92dab872-ab3d-4319-804e-940a3c41eef9`、`bb518b57-f00d-4257-84c0-53bcdf5ee4d7`、`e73fa7ec-42b4-48ef-9286-8ef14ac90f5f`。

一時設定、プロファイル、pool binding は削除済み。ランナー上限を元の3に戻し、検証用 Pod がなく既存の3セッションだけが残っていることを確認した。API、UI、開発ランナーの更新は維持している。

## その後の開発環境の復旧

同日、管理命令の転送先 `sessionManager.apiUrl` が親 API を指していたことを特定し、`http://127.0.0.1:8080` に修正した（Helm revision 30）。上記3件の削除失敗記録は再試行により解消済み。既存セッション3件を維持して新規起動できるよう、pool 上限は4に変更し idle runner 1件を確認した。

また、公開セッション ID を manager の削除対象にしていた処理を、実際の `RemoteSessionID`（割り当てたランナー ID）に修正した。公開 ID 宛ての削除は 404 になり、実際の Pod の削除を後続の orphan reconciliation に依存していた。回帰テストでは削除先 ID、完了確認までの記録保持、完了後の記録削除を確認する。

修正コミット `b8b03d2cd2f50c3434022f390735a76285ffdcec` の API image `sha256:21f4e235e37fdadfcdbd6fd9a9d736184022a64ceab97d88b84876a0fabf4498` を Fly dev に適用し、health のバージョンを確認した。新規セッション `fb0a5184-22d7-4776-bb19-872516d826a6` は Ollama から `DEV_RECOVERY_OK` を返し、公開 DELETE が 202 を返した後、manager が割当 ID `c594425f-9f1f-4285-bec9-28990db50982` を直接削除した。検索結果と Kubernetes Deployment の両方から消えたことを確認済み。一時ユーザー設定と pool binding も削除済み。

最終観測では、既存ランナー3件も 01:31:59〜01:32:59 UTC に orphan reconciliation で削除されていた。今回それらへの削除要求は送っていないが、親の登録から外れた原因は取得できたログだけでは確定できず、ユーザー操作との照合が必要。したがって「既存3セッションを維持」は復旧途中の状態であり、最終状態ではない。01:34:54 UTC に後続の待機ランナー1件が Ready になった。
