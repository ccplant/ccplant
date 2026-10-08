# Session profile command wrapper and asset image

## Summary

Session profile に次の 2 項目を追加する。

<div v-pre>

```json
{
  "config": {
    "command_wrapper_template": "exec devbox run -- {{ .Command }}",
    "asset_image": "ghcr.io/example/ccplant-agent@sha256:0123..."
  }
}
```

</div>

- `command_wrapper_template` は agentapi/ACP bridge を含む、セッションの最終起動コマンドを
  POSIX shell script で包む。<code v-pre>{{ .Command }}</code> には shell quote 済みの既定コマンドを挿入する。
- `asset_image` はそのセッション Pod のメインコンテナ image を置き換える。ccplant CLI は従来どおり
  manager release image から initContainer で注入し、profile からは変更させない。

どちらも profile の選択後、allocation の作成前に解決し、direct、schedule、webhook、SlackBot、
stock runner、External Session Manager (ESM) で同じ値を使う。指定がなければ現行動作を完全に維持する。

## Goals

- profile ごとに `mise exec`、`devbox run`、計測コマンドなどを最終起動コマンドの外側へ置ける。
- profile ごとに agent、ACP adapter、言語ツールを含む任意の asset image を選べる。
- command の組み立てを一か所に集約し、agent type ごとの引数をテンプレート作者が再記述しなくてよい。
- profile CRUD 時に明らかな template エラーを返し、実行時にも fail closed にする。
- allocation、再起動、suspend/resume、ESM を通して、解決済み runtime が変化しない。

## Non-goals

- request ごとの command wrapper/image override は追加しない。
- image registry の credential を profile に保存しない。既存の Pod service account と
  `imagePullSecrets` を使う。
- Docker image の build、scan、署名、配布機能は提供しない。
- Kubernetes の `command` / `args`、securityContext、sidecar を profile から直接編集可能にしない。
- local process session でコンテナ image を利用可能にしない。

## Current state

Kubernetes session Pod のメインコンテナは `kubernetesSession.image` の asset image を使う。
この image は Claude、Codex、Pi、Cursor、ACP adapter、agentapi と各種ツールを提供する一方、
ccplant binary は含まない。ccplant CLI は manager release image から `install-ccplant-cli`
initContainer が `emptyDir` へコピーする。この分離により、asset image を変えても control plane と
session protocol の実装バージョンを manager と揃えられる。

起動コマンドは現在 2 系統で組み立てられている。

- provisioner の `buildAgentCommand` が実際に起動する command/argv を作る。
- legacy な `generate-setting` の `buildStartupConfig` も `StartupConfig` を作る。

新機能では両方が同じ renderer を利用する。template を一方にだけ適用すると direct/legacy runtime
で挙動が分かれるためである。

## API

`SessionProfileConfig` に次を追加する。

| field | type | default | meaning |
| --- | --- | --- | --- |
| `command_wrapper_template` | string | empty | 最終起動 command を包む Go template |
| `asset_image` | string | empty | Kubernetes session のメイン asset image |

例:

<div v-pre>

```json
{
  "name": "nix development shell",
  "config": {
    "params": { "agent_type": "codex-acp" },
    "command_wrapper_template": "exec nix develop --command {{ .Command }}",
    "asset_image": "ghcr.io/example/ccplant-agent@sha256:0123..."
  }
}
```

</div>

MVP では template function を公開しない。複数行の wrapper script もそのまま記述できる。
通常の例は次のとおりである。

<div v-pre>

```text
#!/bin/sh
set -eu
export PATH="/workspace/bin:$PATH"
exec env METRICS_LABEL=experimental {{ .Command }}
```

</div>

`.Command` は backend が生成した argv 全体を POSIX shell quote した文字列である。たとえば
`[]string{"/opt/ccplant/bin/ccplant", "acp-server", "--", "codex-acp"}` は各要素を quote して
連結してから挿入する。template に生の argv、environment、credential、request payload は渡さない。

### Template contract

- engine は Go `text/template`、`Option("missingkey=error")`、追加 `FuncMap` なしとする。
- root data は `struct { Command string }` のみとする。
- <code v-pre>{{ .Command }}</code> action をちょうど 1 回含める。文字列や comment 内の見かけ上の記述は数えず、
  parse tree を検査する。許可する node は top-level の text と単純な `.Command` action だけとし、
  `if`、`range`、`with`、template 定義、pipeline、変数宣言は拒否する。
- 保存時に parse、許可 node の検査、空 data による execute を行う。
- 最大長は 64 KiB、render 後も 64 KiB とする。NUL byte は拒否する。
- render 結果は `/bin/sh -c <rendered>` で実行する。wrapper は任意 script であり、profile を編集する
  権限を持つ利用者のコードとして扱う。
- exit code と signal は `exec` の有無にかかわらず shell process から provisioner が監視する。
  UI と例では signal forwarding のため末尾の <code v-pre>exec {{ .Command }}</code> を推奨する。

単なる `strings.ReplaceAll` は採用しない。template の構文エラーを保存時に検知できず、将来 data を
増やした際に escaping と互換性の境界が曖昧になるためである。

### Asset image contract

`asset_image` は OCI image reference で、tag と digest の両方を受け付ける。保存時は distribution
reference parser で構文だけを検証し、registry への pull/manifest lookup は行わない。private registry
や一時的な registry failure により profile CRUD が失敗しないためである。

custom image は次の既存 contract を満たす必要がある。

- Linux image で `/bin/sh` と `/usr/bin/tini` を含む。
- UID/GID 999 で実行でき、`/home/agentapi` と `/home/agentapi/workdir` を利用できる。
- 選択する agent type に必要な agentapi、agent、ACP adapter を `PATH` 上に含む。
- `/opt/ccplant/bin` を `PATH` に含むか、少なくとも backend が設定する
  `CCPLANT_BINARY_PATH` を尊重する。
- Pod の既存 read-only mounts、PVC、securityContext と互換である。

image の `ENTRYPOINT` と `CMD` は利用しない。現行と同じく Kubernetes container command は
`/usr/bin/tini ... agent-provisioner` に固定する。profile image から control-plane binary を選ばせず、
`CLIImage` も global runtime profile からのみ解決する。

API response、preview、session launch dry-run には secret ではない解決済み `asset_image` と、wrapper が
設定されていることを返す。通常の session response/log には wrapper 本文を複製せず、profile ID と
template の SHA-256 を記録する。

## Resolution and inheritance

profile source の既存ルールを次の scalar fields に拡張する。

- child が空なら source の `command_wrapper_template` / `asset_image` を継承する。
- child が非空なら child が置き換える。template の合成はしない。
- 継承を明示的に無効化する必要が生じた場合に備え、将来 nullable patch semantics を導入する。
  MVP の PUT-style config では空文字を「未指定」とする。

優先順位は以下とする。

```text
global runtime asset image < source profile < selected profile
```

public start request に image/wrapper override を置かないので、その上位 layer は存在しない。解決後は
`RunServerRequest.ProfileAssetImage` と `ProfileCommandWrapperTemplate` に格納する。この内部 field は
JSON で allocation に含まれるが、`POST /start` の入力には bind しない。

## Launch flow

```text
Session profile CRUD
  -> parse/validate template and image reference

POST /start / schedule / webhook / SlackBot
  -> resolve selected profile and source chain
  -> copy resolved wrapper + image into RunServerRequest
  -> create allocation
       -> local allocator: build Pod with resolved image
       -> ESM: receive the same resolved request and build Pod with resolved image
  -> build provision settings
  -> derive canonical agent command argv
  -> shell-quote argv
  -> render wrapper once
  -> start process and monitor it
```

`asset_image` は Pod 作成前に必要で、`command_wrapper_template` は provision payload の生成時に必要で
ある。両者を `SessionSettings` だけに入れる設計では Pod image を選べないため、allocation の request
にも保持する。

### Stock runners

stock Pod は image ごとに互換性が異なる。`sessionallocation.Requirements` に `AssetImage` を追加し、
stock runner の label/requirements matching に含める。空値は global image を解決してから比較する。
異なる image の stock Pod を claim してから image を変更することは禁止する。

初期リリースで任意 image の stock prewarming を提供しない場合、custom `asset_image` を持つ request は
stock を bypass して dedicated Pod を作成する。この方が誤った image reuse より安全で、後から image
digest 単位の pool を追加できる。

### External Session Manager

ESM は parent で認可・profile 解決済みの request を受け取り、`asset_image` をそのまま使用する。
ESM 自身の default image で上書きしない。ただし ESM/cluster operator は registry allowlist や admission
policy によって拒否でき、allocation completion error に stable code `asset_image_rejected` を返す。

runtime profile の `kubernetes.image` には追加しない。これは manager 全体の operator-owned default で、
tenant profile が選ぶ workload image とは更新主体が異なるためである。

### Restart, suspend, and resume

- process restart は保存済み `SessionSettings` の wrapper と canonical command を再利用する。
- Pod 再作成、suspend/resume、context handoff は session 作成時に解決した image reference と wrapper を
  session metadata に保存し、profile の最新版を暗黙に再解決しない。
- ユーザーが設定を再適用する明示的 restart 機能では、既存の profile refresh semantics に従い最新版へ
  更新する。
- tag 指定の image は再 pull 時に内容が変わり得る。再現性が必要な profile には UI で digest pin を
  推奨するが、要求どおり tag 自体は禁止しない。

## Implementation plan

### Domain and API

1. `entities.SessionProfileConfig` に 2 field と accessor を追加する。
2. controller request/response、Kubernetes/libSQL repositories、OpenAPI schema、frontend type を更新する。
3. `validateSessionProfileConfig` から共通 template validator と OCI reference validator を呼ぶ。
4. source merge に scalar inheritance を追加し、cycle/access control は既存処理を再利用する。
5. preview/dry-run response に `asset_image` と `command_wrapper_configured` を追加する。

### Allocation and Pod construction

1. `LaunchRequest` から `RunServerRequest` へ解決済み 2 field を渡す。
2. allocation serialization と requirements に image identity を追加する。
3. Pod builder で `resolvedAssetImage(req, globalConfig)` を一度だけ呼び、main container と metadata annotation
   `agentapi.proxy/asset-image` に適用する。
4. CLI initContainer、network filter、DinD、SCIA の image は変更しない。
5. ESM allocation schema と version compatibility を更新する。old ESM が field を無視して誤った image を
   使わないよう、custom image request は capability `profile_asset_image_v1` を持たない ESM に割り当てない。

### Command rendering

1. agent type から canonical `[]string` を作る処理を shared package に移す。
2. `ShellJoin(argv)` は POSIX single-quote escaping を行う。環境変数展開を意図して argv を裸で連結しない。
3. `RenderCommandWrapper(template, argv)` を shared package に置き、validator と同じ parser を使う。
4. provisioner と legacy `generate-setting` の両方で renderer を使用する。
5. rendered script は session-private directory に mode `0700` で atomic write し、`/bin/sh <path>` を
   argv 形式で起動する。script 本文を process argv や log に出さない。

`StartupConfig` は移行期間中、次の後方互換 field を持つ。

```go
type StartupConfig struct {
    Command                []string `json:"command,omitempty"`
    Args                   []string `json:"args,omitempty"`
    PreScript              string   `json:"pre_script,omitempty"`
    CommandWrapperTemplate string   `json:"command_wrapper_template,omitempty"`
}
```

`PreScript` は既存の internal setup 用であり、profile wrapper と結合しない。実行順は
`PreScript -> render済み wrapper -> canonical command` とし、profile は generated setup script を
上書きできない。

### UI

Session profile editor の runtime/advanced section に以下を追加する。

- asset image の single-line input。default image と contract documentation へのリンクを表示する。
- command wrapper の monospace editor。<code v-pre>{{ .Command }}</code> の挿入ボタンと最小例を表示する。
- client-side では必須 placeholder、NUL、長さだけを検査し、Go template の最終判定は API に任せる。
- profile card には `custom image` / `command wrapper` badge のみを表示し、script 本文は表示しない。
- digest 未固定の image tag には警告を表示する。

## Security and operations

任意 wrapper と任意 image はどちらも session 内での任意コード実行である。通常の session agent も任意
command を実行できるため新しい tenant privilege ではないが、image は process 起動前に実行され、image
filesystem 全体を供給する。したがって次を守る。

- profile ownership/Team membership の既存認可を利用し、launch 時にも再検証する。
- Pod service account は最小権限のままにし、hostPath、privileged、capability を profile から変更不可とする。
- operator option として image registry/repository allowlist を用意する。未設定は全 OCI reference を許可する。
- image credential を API、profile Secret、event、log に含めない。
- wrapper 本文、rendered command、environment を通常ログへ出さない。
- audit event に actor、profile ID、image reference、template digest、session ID、ESM ID を記録する。
- image pull error と wrapper validation/start error を区別し、secret を含まない stable error code を返す。

Kubernetes admission controller による署名検証や digest 強制は本機能の外側でも適用できる。backend 側の
allowlist は早い feedback のためであり、cluster policy の代替ではない。

## Failure behavior

| phase | failure | behavior |
| --- | --- | --- |
| profile save | template parse/placeholder/image syntax error | `400 invalid_session_profile` |
| allocation | ESM capability/allowlist mismatch | 対象 manager を除外、明示 manager なら `422` |
| Pod start | image pull/auth/architecture error | session を failed にし Kubernetes reason を redacted して返す |
| provision | wrapper render error | agent を起動せず session を failed にする |
| runtime | wrapper exits before server ready | 現行の process exit/health failure として扱う |

template error 時に wrapper を無視して canonical command を起動する fallback は行わない。profile 作者の
意図と異なる環境で agent を動かさないためである。

## Tests

- domain/controller: valid template、placeholder なし/複数、unknown field、oversize、NUL、invalid image。
- repository: Kubernetes Secret と libSQL の round trip、旧 record の zero-value compatibility。
- resolution: source inheritance/override、tenant isolation、cycle detection。
- renderer: 全 agent type、空白・quote・newline を含む argv、script size、本文が log に出ないこと。
- launch: direct/schedule/webhook/SlackBot が同じ resolved fields を渡すこと。
- workload: main container だけ image が変わり、CLI/network/DinD/SCIA image は変わらないこと。
- stock: image 不一致を claim しないこと、custom image の bypass。
- ESM: capability negotiation、allocation JSON round trip、拒否 error。
- lifecycle: process restart と Pod resume が保存済み wrapper/image を維持すること。
- frontend: editor load/save、validation、badge、digest warning。

## Rollout

1. schema と serialization を追加する。field 未指定時は現行挙動のままにする。
2. renderer を導入するが、profile field は UI でまだ公開しない。
3. ESM capability と allocation propagation を展開する。
4. backend feature flag `session_profile_runtime_customization` の下で API と UI を公開する。
5. image pull failure、startup failure、custom image session 数を観測する。
6. 十分な ESM 更新率を確認後に feature flag を default on にする。

rollback は UI/API で新規設定を止めても、既存 session の metadata を読み続ける。保存済み profile field を
削除したり無視したりせず、旧 session の restart/resume を維持する。

## Decisions

- wrapper 対象は agent executable だけでなく、HTTP/ACP bridge を含む最終 command 全体とする。
  これにより agent type ごとの内部構造を public template contract にしない。
- image は main asset image のみを置換し、manager/CLI/sidecar image は operator 管理のままにする。
- `.Command` は shell-escaped string とし、生 argv や template helper は MVP で公開しない。
- custom image は stock Pod の claim 条件に含め、安全に一致できない場合は dedicated Pod へ fallback する。
- 解決済み値を session/allocation に保存し、profile 編集によって実行中・resume 後の session が暗黙に変わらない
  ようにする。
