package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

const piInstallPreScript = `mkdir -p "$HOME/.pi/agent/npm"
test -f "$HOME/.pi/agent/npm/package.json" || printf '{"private":true,"dependencies":{}}\n' > "$HOME/.pi/agent/npm/package.json"
if [ -d "$HOME/.pi/agent/npm/node_modules/pi-mcp-adapter" ]; then
  echo "Pi extensions already installed, skipping install"
else
  NPM_SHIM_DIR="$(mktemp -d)"
  trap 'rm -rf "$NPM_SHIM_DIR"' EXIT
  cat > "$NPM_SHIM_DIR/npm" <<'EOF'
#!/bin/sh
set -e
prefix=""
packages=""
while [ "$#" -gt 0 ]; do
  case "$1" in
    install) shift ;;
    --prefix) prefix="$2"; shift 2 ;;
    --legacy-peer-deps) shift ;;
    *) packages="$packages $1"; shift ;;
  esac
done
if [ -z "$prefix" ]; then prefix="$PWD"; fi
mkdir -p "$prefix"
test -f "$prefix/package.json" || printf '%s\n' '{"private":true,"dependencies":{}}' > "$prefix/package.json"
exec bun add --cwd "$prefix" $packages
EOF
  chmod +x "$NPM_SHIM_DIR/npm"
  if [ ! -d "$HOME/.pi/agent/npm/node_modules/pi-mcp-adapter" ]; then
    PATH="$NPM_SHIM_DIR:$PATH" pi install npm:pi-mcp-adapter
  fi
  rm -rf "$NPM_SHIM_DIR"
  trap - EXIT
fi`

// ---------------------------------------------------------------------------
// Input types – SlackBot mode (--input)
// ---------------------------------------------------------------------------

// generateSettingInput is the top-level input structure for the generate-setting command
// when using the --input (SlackBot) mode.
// It contains the SlackBot configuration and optional user/team settings, ordered from
// lowest priority to highest priority.
type generateSettingInput struct {
	SlackBot *generateSettingSlackBot   `json:"slackbot"`
	Settings []*generateSettingSettings `json:"settings,omitempty"`
}

// generateSettingSlackBot represents the relevant SlackBot configuration.
type generateSettingSlackBot struct {
	UserID        string                        `json:"user_id,omitempty"`
	Scope         string                        `json:"scope,omitempty"` // "user" or "team"
	TeamID        string                        `json:"team_id,omitempty"`
	Teams         []string                      `json:"teams,omitempty"` // GitHub team slugs, e.g. ["myorg/backend"]
	SessionConfig *generateSettingSessionConfig `json:"session_config,omitempty"`
}

// generateSettingSessionConfig is the session configuration embedded in a SlackBot.
type generateSettingSessionConfig struct {
	Environment            map[string]string      `json:"environment,omitempty"`
	Tags                   map[string]string      `json:"tags,omitempty"`
	InitialMessageTemplate string                 `json:"initial_message_template,omitempty"`
	Params                 *generateSettingParams `json:"params,omitempty"`
}

// generateSettingParams holds session-creation parameters (SlackBot mode).
type generateSettingParams struct {
	AgentType string `json:"agent_type,omitempty"`
	Oneshot   bool   `json:"oneshot,omitempty"`
}

// ---------------------------------------------------------------------------
// Input types – Schedule mode (--schedule)
// ---------------------------------------------------------------------------

// generateSettingSchedule represents a Schedule configuration.
// Matches the JSON schema used by POST /schedules.
type generateSettingSchedule struct {
	Name          string                          `json:"name,omitempty"`
	UserID        string                          `json:"user_id,omitempty"`
	Scope         string                          `json:"scope,omitempty"` // "user" or "team"
	TeamID        string                          `json:"team_id,omitempty"`
	Teams         []string                        `json:"teams,omitempty"`
	CronExpr      string                          `json:"cron_expr,omitempty"`
	ScheduledAt   string                          `json:"scheduled_at,omitempty"` // ISO8601 string
	Timezone      string                          `json:"timezone,omitempty"`
	SessionConfig *generateSettingScheduleSession `json:"session_config,omitempty"`
}

// generateSettingScheduleSession is the session_config block inside a Schedule.
type generateSettingScheduleSession struct {
	Environment map[string]string              `json:"environment,omitempty"`
	Tags        map[string]string              `json:"tags,omitempty"`
	Params      *generateSettingScheduleParams `json:"params,omitempty"`
}

// generateSettingScheduleParams holds session-creation parameters (Schedule mode).
type generateSettingScheduleParams struct {
	Message                  string `json:"message,omitempty"`
	AgentType                string `json:"agent_type,omitempty"`
	Oneshot                  bool   `json:"oneshot,omitempty"`
	InitialMessageWaitSecond *int   `json:"initial_message_wait_second,omitempty"`
}

// ---------------------------------------------------------------------------
// Settings type (shared between all modes)
// ---------------------------------------------------------------------------

// generateSettingSettings represents user or team settings that influence the session.
// Applied in order (index 0 = lowest priority, last entry = highest priority).
// Typically: base → teams… → user.
type generateSettingSettings struct {
	Name                 string                  `json:"name,omitempty"`
	AuthMode             string                  `json:"auth_mode,omitempty"` // "bedrock", "oauth", or "" (empty = no-op)
	Bedrock              *generateSettingBedrock `json:"bedrock,omitempty"`
	EnvVars              map[string]string       `json:"env_vars,omitempty"`
	ClaudeCodeOAuthToken string                  `json:"claude_code_oauth_token,omitempty"`
}

// generateSettingBedrock holds AWS Bedrock credential settings.
type generateSettingBedrock struct {
	Enabled         bool   `json:"enabled"`
	Model           string `json:"model,omitempty"`
	AccessKeyID     string `json:"access_key_id,omitempty"`
	SecretAccessKey string `json:"secret_access_key,omitempty"`
	RoleARN         string `json:"role_arn,omitempty"`
	Profile         string `json:"profile,omitempty"`
}

// ---------------------------------------------------------------------------
// Command flags
// ---------------------------------------------------------------------------

var (
	generateSettingInputPath    string
	generateSettingSchedulePath string
	generateSettingUserSetting  string
	generateSettingTeamSettings []string
	generateSettingOutputFmt    string
	generateSettingVerbose      bool
)

// ---------------------------------------------------------------------------
// Cobra command definition
// ---------------------------------------------------------------------------

var generateSettingCmd = &cobra.Command{
	Use:   "generate-setting",
	Short: "SlackBot / Schedule 設定から session settings JSON を組み立てて出力する",
	Long: `SlackBot または Schedule の設定をもとに、実際のセッション起動時と同じ手順で
session settings を組み立て、その過程をログに出しながら最終的な JSON / YAML を標準出力します。

手順ログには以下が含まれます:
  1. team settings の適用 (--team-setting の順で低→高優先度)
  2. user settings の適用 (--user-setting)
  3. Bedrock 設定を env に入れる際の判断基準
  4. session_config.environment の上書き適用 (最高優先度)

■ Schedule モード (--schedule)

  スケジュール設定 JSON を直接指定します。
  --team-setting と --user-setting で対応する settings ファイルを指定してください。

  スケジュール JSON の形式:
  {
    "name": "daily-review",
    "user_id": "alice",
    "scope": "user",
    "teams": ["myorg/backend-team"],
    "cron_expr": "0 9 * * 1-5",
    "timezone": "Asia/Tokyo",
    "session_config": {
      "environment": { "MY_VAR": "value" },
      "tags": { "repo": "myorg/myrepo" },
      "params": {
        "agent_type": "claude-acp",
        "message": "Run daily checks",
        "oneshot": true
      }
    }
  }

■ SlackBot モード (--input、デフォルト)

  SlackBot 設定と settings を一つの JSON にまとめて指定します。
  settings 配列と --team-setting / --user-setting フラグの両方が使えます。

  {
    "slackbot": {
      "user_id": "alice",
      "scope": "user",
      "teams": ["myorg/backend-team"],
      "session_config": {
        "environment": { "MY_VAR": "value" },
        "initial_message_template": "Hello!",
        "params": { "agent_type": "claude-acp", "oneshot": false }
      }
    },
    "settings": [...]
  }

■ settings ファイルの形式 (--user-setting / --team-setting)

  {
    "name": "alice",
    "auth_mode": "bedrock",
    "bedrock": {
      "enabled": true,
      "model": "anthropic.claude-3-5-sonnet-20241022-v2:0",
      "role_arn": "arn:aws:iam::123456789012:role/bedrock-role"
    },
    "env_vars": { "MY_KEY": "my_value" }
  }

使用例:
  # Schedule モード
  agentapi-proxy helpers generate-setting \
    --schedule schedule.json \
    --team-setting team-settings.json \
    --user-setting user-settings.json

  # 複数チーム設定 (--team-setting は繰り返し可)
  agentapi-proxy helpers generate-setting \
    --schedule schedule.json \
    --team-setting base-team.json \
    --team-setting backend-team.json \
    --user-setting alice-settings.json

  # SlackBot モード (既存)
  agentapi-proxy helpers generate-setting \
    --input slackbot-config.json \
    --team-setting team-settings.json \
    --user-setting user-settings.json

  # YAML 形式で出力
  agentapi-proxy helpers generate-setting --schedule schedule.json --format yaml`,
	RunE: runGenerateSetting,
}

func init() {
	generateSettingCmd.Flags().StringVarP(&generateSettingInputPath, "input", "i", "",
		"SlackBot モード: 入力 JSON ファイルパス (\"-\" で標準入力)。--schedule 未指定時はデフォルト stdin")
	generateSettingCmd.Flags().StringVar(&generateSettingSchedulePath, "schedule", "",
		"Schedule モード: スケジュール設定 JSON ファイルパス (指定時は --input より優先)")
	generateSettingCmd.Flags().StringVar(&generateSettingUserSetting, "user-setting", "",
		"ユーザー settings JSON ファイルパス (最高優先度で適用)")
	generateSettingCmd.Flags().StringArrayVar(&generateSettingTeamSettings, "team-setting", nil,
		"チーム settings JSON ファイルパス (複数回指定可、指定順で低→高優先度)")
	generateSettingCmd.Flags().StringVar(&generateSettingOutputFmt, "format", "json",
		"出力形式 (json または yaml)")
	generateSettingCmd.Flags().BoolVarP(&generateSettingVerbose, "verbose", "v", false,
		"詳細ログを出力する")

	HelpersCmd.AddCommand(generateSettingCmd)
}

// ---------------------------------------------------------------------------
// Main logic
// ---------------------------------------------------------------------------

func runGenerateSetting(cmd *cobra.Command, args []string) error {
	if generateSettingOutputFmt != "json" && generateSettingOutputFmt != "yaml" {
		return fmt.Errorf("invalid format %q: must be json or yaml", generateSettingOutputFmt)
	}

	if generateSettingSchedulePath != "" {
		return runGenerateSettingSchedule()
	}
	return runGenerateSettingSlackBot(cmd)
}

// ---------------------------------------------------------------------------
// Schedule mode
// ---------------------------------------------------------------------------

func runGenerateSettingSchedule() error {
	log.Printf("[GENERATE-SETTING] モード: Schedule")
	log.Printf("[GENERATE-SETTING] ▶ スケジュール設定 JSON を読み込んでいます: %s", generateSettingSchedulePath)

	data, err := os.ReadFile(generateSettingSchedulePath)
	if err != nil {
		return fmt.Errorf("スケジュール設定の読み込みに失敗しました: %w", err)
	}

	var sched generateSettingSchedule
	if err := json.Unmarshal(data, &sched); err != nil {
		return fmt.Errorf("スケジュール設定 JSON の解析に失敗しました: %w", err)
	}

	// Log schedule summary
	log.Printf("[GENERATE-SETTING] スケジュール情報:")
	log.Printf("[GENERATE-SETTING]   name=%q", sched.Name)
	log.Printf("[GENERATE-SETTING]   user_id=%q scope=%q team_id=%q", sched.UserID, sched.Scope, sched.TeamID)
	if len(sched.Teams) > 0 {
		log.Printf("[GENERATE-SETTING]   teams=%v", sched.Teams)
	}
	if sched.CronExpr != "" {
		log.Printf("[GENERATE-SETTING]   cron_expr=%q timezone=%q", sched.CronExpr, sched.Timezone)
	}
	if sched.ScheduledAt != "" {
		log.Printf("[GENERATE-SETTING]   scheduled_at=%q", sched.ScheduledAt)
	}

	// Collect settings from flags
	settingsList, err := loadSettingsFromFlags(nil)
	if err != nil {
		return err
	}

	// Build env map
	env := make(map[string]string)
	applyAllSettings(env, settingsList)

	// Apply session_config.environment (highest priority)
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 3: session_config.environment の適用 (最高優先度)")
	var sessionEnv map[string]string
	var agentType, initialMessage string
	var oneshot bool
	if sched.SessionConfig != nil {
		sessionEnv = sched.SessionConfig.Environment
		if sched.SessionConfig.Params != nil {
			agentType = sched.SessionConfig.Params.AgentType
			oneshot = sched.SessionConfig.Params.Oneshot
			initialMessage = sched.SessionConfig.Params.Message
		}
	}
	applySessionEnv(env, sessionEnv)

	// Build SessionSettings
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 4: SessionSettings の組み立て")

	scope := sched.Scope
	if scope == "" {
		scope = "user"
	}

	result := &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{
			UserID:    sched.UserID,
			Scope:     scope,
			TeamID:    sched.TeamID,
			AgentType: agentType,
			Oneshot:   oneshot,
			Teams:     sched.Teams,
		},
		Env:            env,
		InitialMessage: initialMessage,
		Claude: sessionsettings.ClaudeConfig{
			ClaudeJSON: map[string]interface{}{
				"hasCompletedOnboarding":        true,
				"bypassPermissionsModeAccepted": true,
			},
		},
	}
	result.Startup = buildStartupConfig(agentType, env)
	logSessionSettingsSummary(result)

	return outputSessionSettings(result, generateSettingOutputFmt)
}

// ---------------------------------------------------------------------------
// SlackBot mode
// ---------------------------------------------------------------------------
