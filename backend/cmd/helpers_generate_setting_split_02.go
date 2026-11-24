package cmd

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/proxybinary"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func runGenerateSettingSlackBot(cmd *cobra.Command) error {
	log.Printf("[GENERATE-SETTING] モード: SlackBot")

	// Determine input source
	inputPath := generateSettingInputPath
	if inputPath == "" {
		inputPath = "-" // default to stdin
	}

	log.Printf("[GENERATE-SETTING] ▶ 入力 JSON を読み込んでいます: %s", func() string {
		if inputPath == "-" {
			return "stdin"
		}
		return inputPath
	}())

	inputData, err := readGenerateSettingInput(inputPath)
	if err != nil {
		return fmt.Errorf("入力の読み込みに失敗しました: %w", err)
	}

	var input generateSettingInput
	if err := json.Unmarshal(inputData, &input); err != nil {
		return fmt.Errorf("入力 JSON の解析に失敗しました: %w", err)
	}

	if input.SlackBot == nil {
		return fmt.Errorf("入力 JSON に \"slackbot\" フィールドが必要です (--schedule を使うと Schedule モードになります)")
	}

	bot := input.SlackBot
	log.Printf("[GENERATE-SETTING] SlackBot: user_id=%q scope=%q team_id=%q teams=%v",
		bot.UserID, bot.Scope, bot.TeamID, bot.Teams)
	log.Printf("[GENERATE-SETTING] 入力内 settings エントリ数: %d", len(input.Settings))

	// Collect settings: embedded + flags (flags are higher priority)
	settingsList, err := loadSettingsFromFlags(input.Settings)
	if err != nil {
		return err
	}

	// Build env map
	env := make(map[string]string)
	applyAllSettings(env, settingsList)

	// Apply session_config.environment (highest priority)
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 3: SlackBot session_config.environment の適用 (最高優先度)")
	var sessionEnv map[string]string
	var agentType, initialMessage string
	var oneshot bool
	if bot.SessionConfig != nil {
		sessionEnv = bot.SessionConfig.Environment
		initialMessage = bot.SessionConfig.InitialMessageTemplate
		if bot.SessionConfig.Params != nil {
			agentType = bot.SessionConfig.Params.AgentType
			oneshot = bot.SessionConfig.Params.Oneshot
		}
	}
	applySessionEnv(env, sessionEnv)

	// Build SessionSettings
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 4: SessionSettings の組み立て")

	scope := bot.Scope
	if scope == "" {
		scope = "user"
	}

	result := &sessionsettings.SessionSettings{
		Session: sessionsettings.SessionMeta{
			UserID:    bot.UserID,
			Scope:     scope,
			TeamID:    bot.TeamID,
			AgentType: agentType,
			Oneshot:   oneshot,
			Teams:     bot.Teams,
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
// Settings loading helpers
// ---------------------------------------------------------------------------

// loadSettingsFromFlags builds the final ordered settings list.
// Order (lowest → highest priority):
//  1. embedded settings (from --input JSON's settings array)
//  2. --team-setting files (in the order specified)
//  3. --user-setting file
func loadSettingsFromFlags(embedded []*generateSettingSettings) ([]*generateSettingSettings, error) {
	var list []*generateSettingSettings

	// 1. Embedded settings (lowest priority)
	list = append(list, embedded...)

	// 2. --team-setting files
	for _, path := range generateSettingTeamSettings {
		s, err := loadSettingsFile(path)
		if err != nil {
			return nil, fmt.Errorf("--team-setting %q の読み込みに失敗しました: %w", path, err)
		}
		log.Printf("[GENERATE-SETTING]   --team-setting %q を読み込みました (name=%q)", path, s.Name)
		list = append(list, s)
	}

	// 3. --user-setting file (highest among settings)
	if generateSettingUserSetting != "" {
		s, err := loadSettingsFile(generateSettingUserSetting)
		if err != nil {
			return nil, fmt.Errorf("--user-setting %q の読み込みに失敗しました: %w", generateSettingUserSetting, err)
		}
		log.Printf("[GENERATE-SETTING]   --user-setting %q を読み込みました (name=%q)", generateSettingUserSetting, s.Name)
		list = append(list, s)
	}

	return list, nil
}

// loadSettingsFile reads and parses a settings JSON file.
func loadSettingsFile(path string) (*generateSettingSettings, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var s generateSettingSettings
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("JSON 解析エラー: %w", err)
	}
	return &s, nil
}

// applyAllSettings applies the full ordered settings list to the env map with logging.
func applyAllSettings(env map[string]string, list []*generateSettingSettings) {
	total := len(list)
	if total == 0 {
		log.Printf("[GENERATE-SETTING]")
		log.Printf("[GENERATE-SETTING] ▶ 手順 1: settings の適用 (設定なし)")
		printBedrockCriteria()
		return
	}

	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ▶ 手順 1: settings の適用 (優先度: 低→高、計 %d 件)", total)
	printBedrockCriteria()

	for idx, s := range list {
		name := s.Name
		if name == "" {
			name = fmt.Sprintf("<settings[%d]>", idx)
		}
		log.Printf("[GENERATE-SETTING]   [%d/%d] settings 適用: name=%q", idx+1, total, name)
		applySettingsToEnv(env, s, generateSettingVerbose)
	}
}

// applySessionEnv applies session_config.environment to the env map (highest priority).
func applySessionEnv(env map[string]string, sessionEnv map[string]string) {
	if len(sessionEnv) == 0 {
		log.Printf("[GENERATE-SETTING]   (session_config.environment は空です)")
		return
	}
	keys := sortedKeys(sessionEnv)
	for _, k := range keys {
		v := sessionEnv[k]
		if old, exists := env[k]; exists && old != v {
			log.Printf("[GENERATE-SETTING]   上書き: %s=%q (旧値: %q)", k, v, old)
		} else {
			log.Printf("[GENERATE-SETTING]   設定: %s=%q", k, v)
		}
		env[k] = v
	}
}

// buildStartupConfig returns the startup command config for the given agent type.
func buildStartupConfig(agentType string, env map[string]string) sessionsettings.StartupConfig {
	proxyBinary := proxybinary.FromMap(env)
	switch agentType {
	case "claude-acp":
		// acp-server bridges claude-agent-acp (ACP over stdio) to the agentapi HTTP interface.
		// Port is determined at runtime via AGENTAPI_PORT env var.
		log.Printf("[GENERATE-SETTING]   startup.command: [agentapi-proxy acp-server -- claude-agent-acp]")
		return sessionsettings.StartupConfig{
			Command: []string{proxyBinary},
			Args:    []string{"acp-server", "--", "claude-agent-acp"},
		}
	case "codex-acp":
		// acp-server bridges codex-acp (ACP adapter for OpenAI Codex) to the agentapi HTTP interface.
		// https://github.com/agentclientprotocol/codex-acp
		log.Printf("[GENERATE-SETTING]   startup.command: [agentapi-proxy acp-server -- codex-acp]")
		return sessionsettings.StartupConfig{
			Command: []string{proxyBinary},
			Args:    []string{"acp-server", "--", "codex-acp"},
		}
	case "pi":
		// acp-server bridges pi-acp to Pi. Provider configuration is generated
		// from the selected compatible API connection.
		// https://github.com/svkozak/pi-acp
		log.Printf("[GENERATE-SETTING]   startup.command: [agentapi-proxy acp-server -- pi-acp]")
		return sessionsettings.StartupConfig{
			Command:   []string{proxyBinary},
			Args:      []string{"acp-server", "--", "pi-acp"},
			PreScript: piInstallPreScript,
		}
	case "cursor":
		// acp-server bridges Cursor Agent CLI's native ACP server to the agentapi HTTP interface.
		// https://cursor.com/docs/cli/acp
		log.Printf("[GENERATE-SETTING]   startup.command: [agentapi-proxy acp-server --auto-approve --raw-json-log -- agent acp]")
		return sessionsettings.StartupConfig{
			Command: []string{proxyBinary},
			Args:    []string{"acp-server", "--auto-approve", "--raw-json-log", "--", "agent", "acp"},
		}
	case "antigravity":
		// Antigravity CLI provides a native ACP v1 server over stdio.
		// https://antigravity.google/docs/cli/acp/
		log.Printf("[GENERATE-SETTING]   startup.command: [agentapi-proxy acp-server -- agy acp]")
		return sessionsettings.StartupConfig{
			Command: []string{proxyBinary},
			Args:    []string{"acp-server", "--", "agy", "acp"},
		}
	default:
		log.Printf("[GENERATE-SETTING]   startup.command: [agentapi server --allowed-hosts * --allowed-origins *]")
		return sessionsettings.StartupConfig{
			Command: []string{"agentapi", "server"},
			Args:    []string{"--allowed-hosts", "*", "--allowed-origins", "*"},
		}
	}
}

// logSessionSettingsSummary logs a summary of the built SessionSettings.
func logSessionSettingsSummary(s *sessionsettings.SessionSettings) {
	log.Printf("[GENERATE-SETTING]   session.user_id=%q scope=%q agent_type=%q oneshot=%v",
		s.Session.UserID, s.Session.Scope, s.Session.AgentType, s.Session.Oneshot)
	log.Printf("[GENERATE-SETTING]   env 変数数: %d", len(s.Env))
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING] ✓ SessionSettings の組み立て完了")
}

// ---------------------------------------------------------------------------
// Bedrock judgment logic (mirrors expandSettingsToEnv in kubernetes_session_manager.go)
// ---------------------------------------------------------------------------

// printBedrockCriteria logs the Bedrock judgment criteria that govern how Bedrock
// settings are translated into environment variables.
func printBedrockCriteria() {
	log.Printf("[GENERATE-SETTING]")
	log.Printf("[GENERATE-SETTING]   ┌─ Bedrock 設定を env に入れる際の判断基準 ─────────────────────────────────┐")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   │  auth_mode の値によって以下のように判断します:")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   │  \"bedrock\" の場合:")
	log.Printf("[GENERATE-SETTING]   │    → CLAUDE_CODE_USE_BEDROCK=1 を設定")
	log.Printf("[GENERATE-SETTING]   │    → Bedrock 資格情報を env に追加 (各フィールドは空でない場合のみ)")
	log.Printf("[GENERATE-SETTING]   │      - model           → ANTHROPIC_MODEL")
	log.Printf("[GENERATE-SETTING]   │      - access_key_id   → AWS_ACCESS_KEY_ID")
	log.Printf("[GENERATE-SETTING]   │      - secret_access_key → AWS_SECRET_ACCESS_KEY")
	log.Printf("[GENERATE-SETTING]   │      - role_arn        → AWS_ROLE_ARN")
	log.Printf("[GENERATE-SETTING]   │      - profile         → AWS_PROFILE")
	log.Printf("[GENERATE-SETTING]   │    ★ 空値フィールドはスキップ: 高優先度の設定を上書きしないため")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   │  \"oauth\" の場合:")
	log.Printf("[GENERATE-SETTING]   │    → CLAUDE_CODE_USE_BEDROCK=0 を設定")
	log.Printf("[GENERATE-SETTING]   │    → 全 AWS 資格情報をクリア (空文字列で上書き)")
	log.Printf("[GENERATE-SETTING]   │    → claude_code_oauth_token があれば CLAUDE_CODE_OAUTH_TOKEN を設定")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   │  \"\" (空 = 未設定) の場合:")
	log.Printf("[GENERATE-SETTING]   │    → auth 関連の env vars を一切変更しない")
	log.Printf("[GENERATE-SETTING]   │    ★ 重要: これにより team の Bedrock 設定を user 設定が上書きしない")
	log.Printf("[GENERATE-SETTING]   │       例) team: auth_mode=bedrock  user: auth_mode=\"\"")
	log.Printf("[GENERATE-SETTING]   │           → team の CLAUDE_CODE_USE_BEDROCK=1 が保持される")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   │  優先度: --team-setting(複数、順)→ --user-setting → session_config.environment")
	log.Printf("[GENERATE-SETTING]   │")
	log.Printf("[GENERATE-SETTING]   └────────────────────────────────────────────────────────────────────────────┘")
	log.Printf("[GENERATE-SETTING]")
}

// applySettingsToEnv applies a single settings entry to the env map.
// This mirrors the expandSettingsToEnv logic in kubernetes_session_manager.go.
func applySettingsToEnv(env map[string]string, s *generateSettingSettings, verbose bool) {
	if s == nil {
		return
	}

	// 1. Custom env vars (always applied regardless of auth_mode)
	if len(s.EnvVars) > 0 {
		keys := sortedKeys(s.EnvVars)
		for _, k := range keys {
			v := s.EnvVars[k]
			if verbose {
				if old, exists := env[k]; exists && old != v {
					log.Printf("[GENERATE-SETTING]     env_vars: %s=%q (旧値: %q)", k, v, old)
				} else {
					log.Printf("[GENERATE-SETTING]     env_vars: %s=%q", k, v)
				}
			}
			env[k] = v
		}
		log.Printf("[GENERATE-SETTING]     env_vars: %d 件を適用しました", len(s.EnvVars))
	}

	// 2. Auth mode and credentials
	switch s.AuthMode {
	case "bedrock":
		log.Printf("[GENERATE-SETTING]     auth_mode=bedrock → CLAUDE_CODE_USE_BEDROCK=1 を設定")
		env["CLAUDE_CODE_USE_BEDROCK"] = "1"

		if s.Bedrock != nil {
			setBedrockField(env, "ANTHROPIC_MODEL", s.Bedrock.Model, "model")
			setBedrockField(env, "AWS_ACCESS_KEY_ID", s.Bedrock.AccessKeyID, "access_key_id")
			setBedrockField(env, "AWS_SECRET_ACCESS_KEY", s.Bedrock.SecretAccessKey, "secret_access_key")
			setBedrockField(env, "AWS_ROLE_ARN", s.Bedrock.RoleARN, "role_arn")
			setBedrockField(env, "AWS_PROFILE", s.Bedrock.Profile, "profile")
		} else {
			log.Printf("[GENERATE-SETTING]     bedrock フィールドが nil のため Bedrock 資格情報はスキップ")
		}

	case "oauth":
		log.Printf("[GENERATE-SETTING]     auth_mode=oauth → CLAUDE_CODE_USE_BEDROCK=0、AWS 資格情報をクリア")
		env["CLAUDE_CODE_USE_BEDROCK"] = "0"
		env["ANTHROPIC_MODEL"] = ""
		env["AWS_ACCESS_KEY_ID"] = ""
		env["AWS_SECRET_ACCESS_KEY"] = ""
		env["AWS_ROLE_ARN"] = ""
		env["AWS_PROFILE"] = ""
		if s.ClaudeCodeOAuthToken != "" {
			log.Printf("[GENERATE-SETTING]     claude_code_oauth_token → CLAUDE_CODE_OAUTH_TOKEN を設定")
			env["CLAUDE_CODE_OAUTH_TOKEN"] = s.ClaudeCodeOAuthToken
		}

	case "":
		log.Printf("[GENERATE-SETTING]     auth_mode=\"\" (未設定) → auth 関連 env vars を変更しない")
		// Do not touch any auth-related env vars.
		// This ensures that a team's Bedrock settings are preserved when a user
		// settings entry has no auth_mode configured.
		if s.ClaudeCodeOAuthToken != "" {
			log.Printf("[GENERATE-SETTING]     claude_code_oauth_token → CLAUDE_CODE_OAUTH_TOKEN を設定 (auth_mode 無関係)")
			env["CLAUDE_CODE_OAUTH_TOKEN"] = s.ClaudeCodeOAuthToken
		}

	default:
		log.Printf("[GENERATE-SETTING]     auth_mode=%q は未知の値です。auth 関連 env vars を変更しません", s.AuthMode)
	}
}

// setBedrockField sets envKey=value if value is non-empty; otherwise logs that it is skipped.
