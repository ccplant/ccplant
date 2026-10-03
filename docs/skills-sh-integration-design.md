# skills.sh スキル統合設計

Status: proposed（設計のみ）

## 1. 結論

skills.sh で配布される Agent Skill を Personal / Team Settings に取り込み、
セッション起動時に確定した `agent_type` に対応する探索先だけへ配置する。

両対応は可能である。両者とも YAML front matter を持つ `SKILL.md` と同じ基本ディレクトリ構造を
採用しており、Claude Code は `~/.claude/skills/<name>/SKILL.md`、Codex は
`~/.codex/skills/<name>/SKILL.md` を探索する。スキル本文をエージェント別形式へ変換せず、検証済みの
同じ bundle を利用できる。ただし一つの session で両方へ無条件にコピーせず、Claude session なら
Claude Code、Codex session なら Codex の探索先だけへ公開する。

初期版では、skills.sh の URL または `owner/repository` 形式で公開 GitHub repository を指定し、
repository 内の一つ以上のスキルを選んでインストールする。インストール時に commit SHA と bundle の
digest を固定する。セッション起動のたびに `npx skills add` を実行したり、既定 branch の最新版を
取得したりしない。

## 2. 背景と既存実装

skills.sh の CLI は `npx skills add <package>` と `--agent`、`--skill`、`--global`、`--copy` を提供し、
Claude Code と Codex を対象エージェントとして扱っている。skills.sh 自体は discovery と配布元への
入口であり、スキルの実体は主に Git repository 内の `SKILL.md` と補助ファイルである。

ccplant には次の関連機能が既にある。

- Personal / Team Settings の marketplace と enabled plugin をセッション設定へマージする。
- startup 時に Claude marketplace を clone し、Claude Code plugin をインストールする。
- marketplace repository の `plugins/*/skills/*` を `~/.codex/skills` へコピーする。
- session profile の files と user-managed files を provisioner が home directory へ配置する。
- 設定全体の再読み込み設計では skills / plugins も再生成対象としている。

既存の `syncCodexSkills` は移行期間の互換処理として残せるが、新機能の保存モデルにはしない。現在の
実装は直下の通常ファイルだけをコピーするため、`references/`、`scripts/`、`assets/` を持つ一般的な
Agent Skill を完全には複製できない。また、異なる marketplace 間の同名 skill を後勝ちで上書きする。
skills.sh 統合では再帰的な bundle、版固定、衝突検出を一つの materializer で扱う。

## 3. UX

Settings の Personal / Team scope に `Skills` ページを追加する。

```text
Settings
├── Agents
├── AI Providers
├── Environment Variables
├── Files
├── Skills                 <- new
├── MCP Servers
├── Marketplaces
└── Plugins
```

「Skills を追加」で次を入力する。

```text
Source:  https://skills.sh/<owner>/<repository>/<skill>
         または owner/repository
Version: branch / tag / commit（省略時は default branch の現在の commit）
Skills:        [x] pr-review  [ ] frontend-design
Compatibility: Claude Code / Codex（検出結果。必要なら利用者が狭める）
```

source を入力すると server が repository を取得して候補を検出し、`name`、`description`、source path、
検出した commit SHA を preview する。保存前に `SKILL.md` と含まれるファイル一覧を閲覧できるようにし、
任意の外部スキルを「URL を貼るだけで即実行」にはしない。

一覧には以下を表示する。

- skill 名と description
- source repository / source path
- 要求した ref と解決済み commit SHA
- bundle digest、インストール日時、更新の有無
- 対応可能な agent（Claude / Codex）
- scope（Personal / Team）と validation / review 状態

更新は明示操作とする。「更新を確認」で新 commit と差分を表示し、「この版へ更新」で新しい snapshot を
作る。自動追従は初期版では行わない。削除や無効化は新規セッションから反映し、稼働中 session には
設定再読み込み・再起動で反映する。

## 4. データモデル

skill は Settings 内の巨大な JSON や managed files の集合として保存せず、Settings scope に属する
独立した metadata と immutable bundle に分ける。bundle は Kubernetes Secret のサイズ制限を避けるため、
既存の object storage abstraction を拡張した skill artifact store に保存する。metadata repository は
既存 persistence backend に合わせて Kubernetes / libSQL 実装を持つ。

```go
type AgentSkill struct {
    ID             string
    Scope          ResourceScope // user | team
    UserID         string
    TeamID         string
    Name           string        // validated SKILL.md front matter name
    Description    string
    Source         SkillSource
    RequestedRef   string
    ResolvedCommit string
    SourcePath     string
    Digest         string        // sha256 of normalized bundle
    ArtifactKey    string
    CompatibleAgents []string    // claude | codex; 配置先ではなく利用可能範囲
    Enabled        bool
    ReviewStatus   string        // pending | approved | rejected
    CreatedAt      time.Time
    UpdatedAt      time.Time
}

type SkillSource struct {
    Type       string // github; later git | upload | skills_api
    Repository string // owner/repository
    URL        string // normalized public source URL
}
```

同じ repository から複数 skill を選んだ場合も skill ごとに resource を作る。更新・無効化・agent 選択を
個別に行え、source repository のディレクトリ構造を runtime API に漏らさずに済む。
`CompatibleAgents` は session へのインストール先を直接指定する値ではない。実際の配置先は session の
resolved `agent_type` から一意に決め、互換リストはその agent で利用してよいかを検証するために使う。

session profile には初期版で bundle を重複保持させない。必要なら `skill_ids` を追加して、Settings で
有効な全 skill ではなく profile ごとの allowlist を選べるようにする。空または未指定は Personal / Team
Settings の enabled skills を使用する。source profile を導入する場合、既存の environment / MCP と同様に
ID の集合として解決し、bundle 本体は継承しない。

## 5. import と検証

### 5.1 source の解決

server-side importer は次の順で処理する。

1. skills.sh URL を canonical GitHub repository、任意の skill hint へ正規化する。
2. 許可された GitHub host から repository metadata と archive を取得する。
3. 指定 ref を immutable commit SHA に解決する。
4. root または配下の `SKILL.md` を探索する。初期版は探索深度と候補数に上限を設ける。
5. 選択された skill directory だけを一時領域へ展開し、symlink を解決せず拒否する。
6. validation と security scan を行い、正規化した archive と SHA-256 digest を生成する。
7. artifact を保存した後に metadata を transactionally publish する。

skills.sh の非公開・非 GitHub source や pack URL は初期版では「未対応 source」として返す。skills.sh の
非公開 API や HTML scraping に依存しない。将来、安定した API または別 resolver を追加しても、下流は
canonical bundle を受け取るだけにする。

### 5.2 validation

- `SKILL.md` は skill directory 直下に一つだけ、UTF-8 の通常ファイルであること。
- front matter の `name` と `description` を必須にし、Agent Skills の共通範囲を検証する。
- directory 名、file path、front matter name は traversal、絶対 path、制御文字を拒否する。
- symlink、device、socket、FIFO、hardlink、setuid bit を拒否する。
- file 数、個別 file size、展開後合計 size、圧縮率に上限を設ける。
- 実行可能 script は bundle に保持するが import 中には実行しない。
- token、private key 等の既知パターンと危険な instruction / script を scan し、結果を UI に表示する。
- warning を含む Team skill は team 管理権限を持つ利用者の明示承認を要求する。

OpenAI の公式ドキュメントも skill を「特権的な code と instruction」として扱い、open catalog からの
無制限な end-user attachment を避けるよう求めている。そのため、人気順一覧からワンクリックで全ユーザーへ
配布する UI は設けず、source・固定版・内容確認・scope 権限を必須にする。

## 6. API

resource API を追加する。

| Method | Path | Behavior |
| --- | --- | --- |
| `POST` | `/skills/preview` | source/ref を解決し、候補・commit・validation を返す。保存しない |
| `GET` | `/skills` | 認可された Personal / Team skill metadata を一覧する |
| `POST` | `/skills` | preview token と選択 skill を使い snapshot を作成する |
| `GET` | `/skills/:id` | metadata、manifest、file tree、scan 結果を返す |
| `GET` | `/skills/:id/files/*` | text preview 可能な bundle file を返す |
| `PATCH` | `/skills/:id` | enabled、compatible_agents を更新する |
| `POST` | `/skills/:id/check-update` | requested ref の現在 commit と差分 metadata を返す |
| `POST` | `/skills/:id/update` | preview 済み commit から新 snapshot へ更新する |
| `DELETE` | `/skills/:id` | metadata を削除し、未参照 artifact を GC 対象にする |

`POST /skills` に任意 URL を再送させず、短命で user/scope/source/ref/commit/digest に bind した preview
token を使う。preview と install の間で内容が差し替わる TOCTOU を防ぐ。Team scope の操作は既存の team
settings 更新権限を要求する。file API は秘密を含む可能性を考慮して認可し、binary は原則 download させず
metadata のみ返す。

OpenAPI、Go client、frontend client を同時に更新する。session start response には秘密や bundle を含めず、
解決された skill ID、commit、digest のみを診断情報として含める。

## 7. scope のマージと衝突規則

起動時に team settings、personal settings、session profile allowlist を解決する。次に `auto` を含む
`agent_type` を既存の agent 選択処理で具体的な runtime agent へ確定し、その agent が
`compatible_agents` に含まれ、enabled かつ approved の skill だけを選ぶ。skill 解決側が `auto` を独自に
再判定してはならない。

agent の command namespace は最終的に skill `name` を使うため、同名 skill の暗黙上書きを禁止する。

- 同じ `(agent, name)` が複数 scope / source から選ばれた場合は session start を validation error にする。
- UI は衝突元を示し、片方を無効化するか profile allowlist から外すよう案内する。
- Claude plugin の namespaced skill は direct skill と共存できるが、bare alias の衝突は warning にする。
- 既存 `syncCodexSkills` が作る skill と direct skill が同名なら direct skill を上書きせず、起動を失敗させる。

Team を優先して黙って Personal skill を隠す方式は採らない。意図しない instruction の差し替えを防ぎ、
Claude と Codex で異なる precedence rule が発生しないようにする。

## 8. セッション起動時の materialize

親 proxy は session settings の解決時に metadata だけでなく、固定した artifact descriptor を manager へ渡す。

```go
type ResolvedSkill struct {
    ID          string   `json:"id"`
    Name        string   `json:"name"`
    Digest      string   `json:"digest"`
    ArtifactURL string   `json:"artifact_url"` // short-lived, manager-bound
}
```

`SessionSettings` には resolved skill と一緒に確定済みの `agent_type` が存在するため、provisioner は
次の pure function で一つの install plan を作る。

```go
type SkillInstallPlan struct {
    AgentType       string
    SkillsAgent     string // skills.sh CLI の --agent に相当する canonical ID
    DestinationRoot string
    Skills          []ResolvedSkill
}

func ResolveSkillInstallPlan(agentType string, skills []ResolvedSkill) (SkillInstallPlan, error)
```

初期版の対応表は次とする。alias は session 起動時の agent resolver で canonical type に直してから渡す。

| resolved `agent_type` | skills.sh agent | 配置先 | 既存 plugin skill の処理 |
| --- | --- | --- | --- |
| `claude-acp`, `claude-legacy` | `claude-code` | `~/.claude/skills/<name>` | Claude plugin install のみ。Codex への copy はしない |
| `codex-acp` | `codex` | `~/.codex/skills/<name>` | plugin bundle の portable skill だけ Codex 用に materialize |
| `pi`, `pi-ollama` | 対応外 | なし | 暗黙に Codex skill を symlink しない。別 capability として設計する |
| `cursor`、未知の type | 対応外 | なし | skill 指定があれば起動前に unsupported error |

skills.sh CLI を importer または materializer の実装に利用する場合も、`--agent '*'` は使わない。
上表の `SkillsAgent` を `--agent claude-code` または `--agent codex` として渡す。CLI を使わない実装でも、
同じ canonical ID と対応表を test oracle とし、CLI の agent 判定と ccplant の配置判定が分岐しないようにする。

artifact URL は短命・一回利用・対象 manager/session に bind し、公開 session API やログに出さない。External
Session Manager でも同じ descriptor を受け取り、capability `agent_skills_v1` がない manager には allocation
前に未対応を返す。Kubernetes と local manager が同じ provisioner materializer を使う。

provisioner は agent 起動前に以下を行う。

```text
download → digest verify → safe extract to staging
         → recursive file validation
         → atomic rename into ~/.agentapi/skills/<id>/<digest>/
         → publish to the one destination selected from agent_type
         → start selected agent
```

公開先は copy または同一 filesystem 内の symlink とする。配布する Claude / Codex の実バージョンで directory
symlink の discovery を統合テストし、どちらかが非対応なら recursive copy に統一する。canonical bundle 内の
relative reference は directory 全体を保持するためそのまま動く。`SKILL.md` だけを書き換えたり、Claude 用と
Codex 用で別内容を生成したりしない。

skill が Claude / Codex の両方に compatible でも、現在の session の `agent_type` に対応する一方へしか
公開しない。canonical bundle は一つなので、別々の Claude session と Codex session は同じ固定 artifact を
それぞれの探索先で利用できる。現在 provisioner が行う Pi から `~/.codex/skills` への symlink はこの規則に
反するため、新しい resolver 導入時に削除する。Pi を対応対象にする場合は、skills.sh の agent ID、探索先、
互換性を別途定義して対応表へ追加する。

restart では新しい resolved set を完全置換し、以前 materialize した managed skill のうち選択されなくなった
ものを削除する。repository に commit 済みの `.claude/skills` / `.codex/skills` やユーザーが session 内で
作成したファイルは削除しないよう、managed manifest に ownership を記録する。artifact download / digest
検証に失敗した場合は agent を起動せず、部分的な skill set で成功扱いしない。

### 8.1 既存 marketplace skill の移行

既存の `syncMarketplaces` / `syncCodexSkills` も `agent_type` を受け取り、同じ install plan を使う。

- Claude session では marketplace 登録と enabled plugin install を行うが、`syncCodexSkills` は呼ばない。
- Codex session では Claude plugin を runtime に有効化せず、選択された plugin bundle から portable skill を
  抽出して `~/.codex/skills` だけへ materialize する。
- skill を持たない Claude plugin、または hooks / agents / Claude 固有 MCP を必要とする plugin は Codex
  session で install 済みとして扱わず、非対応理由を返す。
- agent type ごとに別実装で directory を走査せず、`DiscoverSkillBundles` と
  `ResolveSkillInstallPlan`、共通 recursive materializer に分割する。

これにより「Claude plugin を常にインストールしてから Codex にもコピーする」という現在の副作用をなくす。
対象外 agent の config directory を作らないことも受け入れ条件に含める。

## 9. Claude / Codex の互換範囲

共通対応するのは Agent Skills の portable subset とする。

| 項目 | Claude Code | Codex | 方針 |
| --- | --- | --- | --- |
| `SKILL.md` の name / description / instructions | 対応 | 対応 | そのまま共有 |
| `references/`, `scripts/`, `assets/` | 対応 | 対応 | directory を再帰配置 |
| 手動 invoke | `/skill-name` | `$skill-name` | UI に両方を表示 |
| Claude 固有 front matter / `${CLAUDE_*}` | 対応 | 保証外 | import warning、Codex 対象は既定 off |
| plugin bundled MCP / hooks / agents | plugin として対応 | skill 単体では非互換 | Skills 機能の対象外。既存 Plugins を使う |
| agent 固有 CLI / tool 名への依存 | 条件付き | 条件付き | static warning と実機 smoke test |

したがって「同じ bundle が必ず同じ結果を返す」ことまでは保証しない。portable validation を通る skill は
両方へ配布でき、agent 固有拡張を使う skill は対象 agent を限定する。将来 `compatibility` metadata を
skills.sh 側から取得できる場合も、実際の内容検証と利用者の agent 選択を置き換えない。

## 10. security と運用

- import service の outbound network は許可 Git host と archive endpoint に限定し、SSRF を防ぐ。
- private repository 対応は初期版から外す。追加時は既存 GitHub broker を利用し、token を clone URL、log、
  artifact に残さない。
- artifact は tenant metadata からのみ参照し、digest 単位の物理 deduplication をしても認可は共有しない。
- skill script は session sandbox と既存 approval policy の範囲でのみ実行される。import 時には実行しない。
- skill の追加・更新・承認・削除を audit event として残す。
- session status には skill ID / name / digest と materialize error を出し、instruction 本文や署名 URLは出さない。
- skills.sh CLI の telemetry に依存しない。server-side import から第三者 telemetry を送らない。
- artifact retention は参照中 revision と rollback 用の直前 revision を保持し、それ以外を grace period 後に GC
  する。

## 11. 実装順序

1. `AgentSkill` entity / repository / artifact store、preview・CRUD API、OpenAPI を追加する。
2. importer の GitHub source 正規化、commit 固定、bundle validation、archive 生成を追加する。
3. Settings の Skills UI、内容 preview、差分付き update flow を追加する。
4. agent resolver の出力を受ける `ResolveSkillInstallPlan` と対応表を追加し、既存 marketplace 処理も
   agent type ごとの plan に移行する。
5. settings resolver と `SessionSettings` に `ResolvedSkill` を追加し、scope・compatibility・衝突を検証する。
6. provisioner に safe download / extract / atomic materialize / cleanup を追加する。
7. Claude Code / Codex の実配布バージョン、local / Kubernetes / ESM で受け入れテストする。
8. 既存 marketplace-to-Codex copy と Pi symlink を置換し、二重配布と後勝ち上書きをなくす。

初期リリースを小さくする場合も、1 repository / 1 skill / public GitHub / 両 agent という制限に留める。
セッション起動時の mutable clone や `npx` 実行、managed files への `SKILL.md` 手入力を暫定仕様にはしない。
それらは版固定、補助ファイル、安全性、更新管理を後から互換性なく変更することになるためである。

## 12. 受け入れ条件

- skills.sh URL と `owner/repository` の双方から候補を preview でき、保存後は commit SHA / digest が固定される。
- references と scripts を含む skill が Claude Code と Codex の双方から discovery / explicit invoke できる。
- Claude session では `.claude/skills` だけ、Codex session では `.codex/skills` だけに managed skill が現れる。
- skills.sh CLI を使う経路では resolved agent type に応じて `--agent claude-code` / `--agent codex` が選ばれ、
  `--agent '*'` を使わない。
- marketplace 由来 skill も同じ agent type 判定を使い、Claude session で Codex copy、Codex session で
  Claude plugin activation を行わない。
- `auto` は skill 処理より前に一度だけ解決され、unknown / unsupported agent は配置前に失敗する。
- Pi session は Codex skills directory を暗黙共有しない。
- session 起動中に upstream branch が更新されても、承認済み bundle の内容は変わらない。
- Personal / Team、direct skill / plugin skill の同名衝突を起動前に検出し、暗黙上書きしない。
- malformed front matter、path traversal、symlink、size/file-count 超過、digest 不一致を拒否する。
- importer は bundle 内 script を実行せず、private network URL へ接続しない。
- skill の無効化・削除・更新が新規 session と設定再読み込み後の session に完全置換で反映される。
- artifact store 障害時に skill を欠いた状態で agent を起動しない。
- ESM は capability negotiation し、未対応 manager へ skill 付き session を割り当てない。

## 13. 参照仕様

- [skills.sh documentation](https://www.skills.sh/docs)
- [skills.sh CLI reference](https://www.skills.sh/docs/cli)
- [Claude Code: Extend Claude with skills](https://code.claude.com/docs/en/skills)
- [OpenAI: Skills](https://developers.openai.com/api/docs/guides/tools-skills)
- [OpenAI: Build skills](https://developers.openai.com/plugins/build/skills)
