package repositories

import (
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func (r *KubernetesSlackBotRepository) jsonToEntity(sbj *slackBotJSON) *entities.SlackBot {
	slackBot := entities.NewSlackBot(sbj.ID, sbj.Name, sbj.UserID)
	slackBot.SetScope(sbj.Scope)
	slackBot.SetTeamID(sbj.TeamID)
	if len(sbj.Teams) > 0 {
		slackBot.SetTeams(sbj.Teams)
	}
	slackBot.SetStatus(sbj.Status)
	if sbj.BotTokenSecretName != "" {
		slackBot.SetBotTokenSecretName(sbj.BotTokenSecretName)
	}
	if sbj.BotTokenSecretKey != "" {
		slackBot.SetBotTokenSecretKey(sbj.BotTokenSecretKey)
	}
	if sbj.AppTokenSecretKey != "" {
		slackBot.SetAppTokenSecretKey(sbj.AppTokenSecretKey)
	}
	if sbj.SettingsSecretID != "" {
		slackBot.SetSettingsSecret(sbj.SettingsSecretID, sbj.SettingsBotTokenKey, sbj.SettingsAppTokenKey)
	}
	if len(sbj.AllowedEventTypes) > 0 {
		slackBot.SetAllowedEventTypes(sbj.AllowedEventTypes)
	}
	if len(sbj.AllowedChannelNames) > 0 {
		slackBot.SetAllowedChannelNames(sbj.AllowedChannelNames)
	}
	if len(sbj.AllowedUserIDs) > 0 {
		slackBot.SetAllowedUserIDs(sbj.AllowedUserIDs)
	}
	if sbj.MaxSessions > 0 {
		slackBot.SetMaxSessions(sbj.MaxSessions)
	}
	if sbj.SessionConfig != nil {
		slackBot.SetSessionConfig(r.sessionConfigJSONToSlackBotEntity(sbj.SessionConfig))
	}
	slackBot.SetNotifyOnSessionCreated(sbj.NotifyOnSessionCreated)
	slackBot.SetAllowBotMessages(sbj.AllowBotMessages)
	slackBot.SetCreatedAt(sbj.CreatedAt)
	slackBot.SetUpdatedAt(sbj.UpdatedAt)
	return slackBot
}

// entityToJSON converts entity to JSON representation
func (r *KubernetesSlackBotRepository) entityToJSON(sb *entities.SlackBot) *slackBotJSON {
	sbj := &slackBotJSON{
		ID:                  sb.ID(),
		Name:                sb.Name(),
		UserID:              sb.UserID(),
		Scope:               sb.Scope(),
		TeamID:              sb.TeamID(),
		Teams:               sb.Teams(),
		Status:              sb.Status(),
		BotTokenSecretName:  sb.BotTokenSecretName(),
		BotTokenSecretKey:   sb.BotTokenSecretKey(),
		AppTokenSecretKey:   sb.AppTokenSecretKey(),
		SettingsSecretID:    sb.SettingsSecretID(),
		SettingsBotTokenKey: sb.SettingsBotTokenKey(),
		SettingsAppTokenKey: sb.SettingsAppTokenKey(),
		AllowedEventTypes:   sb.AllowedEventTypes(),
		AllowedChannelNames: sb.AllowedChannelNames(),
		AllowedUserIDs:      sb.AllowedUserIDs(),
		MaxSessions:         sb.MaxSessions(),
		CreatedAt:           sb.CreatedAt(),
		UpdatedAt:           sb.UpdatedAt(),
	}
	// Only store notify_on_session_created when explicitly set to false,
	// to avoid bloating stored data for the default (true) case.
	if v := sb.RawNotifyOnSessionCreated(); v != nil {
		sbj.NotifyOnSessionCreated = v
	}

	// Only store allow_bot_messages when explicitly set to true,
	// to avoid bloating stored data for the default (false) case.
	if v := sb.RawAllowBotMessages(); v != nil {
		sbj.AllowBotMessages = v
	}

	if sc := sb.SessionConfig(); sc != nil {
		sbj.SessionConfig = r.sessionConfigSlackBotEntityToJSON(sc)
	}

	return sbj
}

// sessionConfigJSONToSlackBotEntity converts session config JSON to entity
// Reuses webhookSessionConfigJSON since SlackBot uses the same WebhookSessionConfig type
func (r *KubernetesSlackBotRepository) sessionConfigJSONToSlackBotEntity(scj *webhookSessionConfigJSON) *entities.WebhookSessionConfig {
	sc := entities.NewWebhookSessionConfig()
	sc.SetEnvironment(scj.Environment)
	sc.SetTags(scj.Tags)
	sc.SetInitialMessageTemplate(scj.InitialMessageTemplate)
	sc.SetReuseMessageTemplate(scj.ReuseMessageTemplate)
	sc.SetReuseSession(scj.ReuseSession)
	sc.SetMountPayload(scj.MountPayload)
	if scj.Params != nil {
		sc.SetParams(scj.Params)
	}
	if scj.SessionProfileID != "" {
		sc.SetSessionProfileID(scj.SessionProfileID)
	}
	return sc
}

// sessionConfigSlackBotEntityToJSON converts session config entity to JSON
// Reuses webhookSessionConfigJSON since SlackBot uses the same WebhookSessionConfig type
func (r *KubernetesSlackBotRepository) sessionConfigSlackBotEntityToJSON(sc *entities.WebhookSessionConfig) *webhookSessionConfigJSON {
	scj := &webhookSessionConfigJSON{
		Environment:            sc.Environment(),
		Tags:                   sc.Tags(),
		InitialMessageTemplate: sc.InitialMessageTemplate(),
		ReuseMessageTemplate:   sc.ReuseMessageTemplate(),
		ReuseSession:           sc.ReuseSession(),
		MountPayload:           sc.MountPayload(),
		SessionProfileID:       sc.SessionProfileID(),
	}
	if params := sc.Params(); params != nil {
		scj.Params = params
	}
	return scj
}
