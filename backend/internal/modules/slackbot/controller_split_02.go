package slackbot

import (
	"net/http"

	"github.com/labstack/echo/v4"
	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/pkg/auth"
)

func (c *SlackBotController) UpdateSlackBot(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	bot, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrSlackBotNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "slackbot not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get slackbot")
	}

	userID := getSlackBotUserID(ctx)
	if !c.userCanAccess(ctx, bot, userID) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	var req UpdateSlackBotRequest
	if err := ctx.Bind(&req); err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "invalid request body")
	}

	if req.Name != "" {
		bot.SetName(req.Name)
	}
	if req.Status != "" {
		bot.SetStatus(req.Status)
	}
	if req.BotTokenSecretName != nil {
		bot.SetBotTokenSecretName(*req.BotTokenSecretName)
	}
	if req.BotTokenSecretKey != nil {
		bot.SetBotTokenSecretKey(*req.BotTokenSecretKey)
	}
	if req.AllowedEventTypes != nil {
		bot.SetAllowedEventTypes(req.AllowedEventTypes)
	}
	if req.AllowedChannelNames != nil {
		bot.SetAllowedChannelNames(req.AllowedChannelNames)
	}
	if req.AllowedUserIDs != nil {
		bot.SetAllowedUserIDs(req.AllowedUserIDs)
	}
	if req.MaxSessions > 0 {
		bot.SetMaxSessions(req.MaxSessions)
	}
	if req.SessionConfig != nil {
		bot.SetSessionConfig(toEntitySessionConfig(req.SessionConfig))
	}
	if req.NotifyOnSessionCreated != nil {
		bot.SetNotifyOnSessionCreated(req.NotifyOnSessionCreated)
	}
	if req.AllowBotMessages != nil {
		bot.SetAllowBotMessages(req.AllowBotMessages)
	}

	// Update team memberships.
	// If an explicit list is supplied in the request, use it (allows API-key users
	// to pin specific teams). Otherwise refresh from the requester's current auth
	// context (picks up GitHub team membership changes for OAuth users).
	if req.Teams != nil {
		bot.SetTeams(req.Teams)
	} else if authzCtx := auth.GetAuthorizationContext(ctx); authzCtx != nil {
		bot.SetTeams(authzCtx.TeamScope.Teams)
	}

	if req.BotToken != "" {
		bot.SetBotToken(req.BotToken)
	}
	if req.AppToken != "" {
		bot.SetAppToken(req.AppToken)
	}
	if req.Secret != nil {
		if err := c.validateSecretRef(ctx, bot, req.Secret); err != nil {
			return err
		}
		bot.SetSettingsSecret(req.Secret.SecretID, req.Secret.BotTokenKey, req.Secret.AppTokenKey)
	}

	if err := c.repo.Update(ctx.Request().Context(), bot); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to update slackbot")
	}

	return ctx.JSON(http.StatusOK, c.toResponse(bot))
}

// DeleteSlackBot handles DELETE /slackbots/:id
func (c *SlackBotController) DeleteSlackBot(ctx echo.Context) error {
	id := ctx.Param("id")
	if id == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "id is required")
	}

	bot, err := c.repo.Get(ctx.Request().Context(), id)
	if err != nil {
		if _, ok := err.(entities.ErrSlackBotNotFound); ok {
			return echo.NewHTTPError(http.StatusNotFound, "slackbot not found")
		}
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to get slackbot")
	}

	userID := getSlackBotUserID(ctx)
	if !c.userCanAccess(ctx, bot, userID) {
		return echo.NewHTTPError(http.StatusForbidden, "access denied")
	}

	if err := c.repo.Delete(ctx.Request().Context(), id); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, "failed to delete slackbot")
	}

	return ctx.NoContent(http.StatusNoContent)
}

// --- Helpers ---

func (c *SlackBotController) toResponse(bot *entities.SlackBot) *SlackBotResponse {
	return ToResponse(bot)
}

// ToResponse converts a SlackBot entity to its non-secret transport form.
func ToResponse(bot *entities.SlackBot) *SlackBotResponse {
	resp := &SlackBotResponse{
		ID:                     bot.ID(),
		Name:                   bot.Name(),
		UserID:                 bot.UserID(),
		Scope:                  bot.Scope(),
		TeamID:                 bot.TeamID(),
		Teams:                  bot.Teams(),
		Status:                 bot.Status(),
		BotTokenSecretName:     bot.BotTokenSecretName(),
		BotTokenSecretKey:      bot.BotTokenSecretKey(),
		AllowedEventTypes:      bot.AllowedEventTypes(),
		AllowedChannelNames:    bot.AllowedChannelNames(),
		AllowedUserIDs:         bot.AllowedUserIDs(),
		MaxSessions:            bot.MaxSessions(),
		NotifyOnSessionCreated: bot.NotifyOnSessionCreated(),
		AllowBotMessages:       bot.AllowBotMessages(),
		CreatedAt:              bot.CreatedAt(),
		UpdatedAt:              bot.UpdatedAt(),
	}
	if bot.SessionConfig() != nil {
		resp.SessionConfig = fromEntitySessionConfig(bot.SessionConfig())
	}
	if bot.SettingsSecretID() != "" {
		resp.Secret = &SlackBotSecretRef{SecretID: bot.SettingsSecretID(), BotTokenKey: bot.SettingsBotTokenKey(), AppTokenKey: bot.SettingsAppTokenKey()}
	}
	return resp
}

func (c *SlackBotController) validateSecretRef(ctx echo.Context, bot *entities.SlackBot, ref *SlackBotSecretRef) error {
	if c.settingsRepo == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "settings secrets are unavailable")
	}
	if ref.SecretID == "" {
		return echo.NewHTTPError(http.StatusBadRequest, "secret.secret_id is required")
	}
	settingsName := bot.UserID()
	if bot.Scope() == entities.ScopeTeam {
		settingsName = bot.TeamID()
	}
	settings, err := c.settingsRepo.FindByName(ctx.Request().Context(), settingsName)
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, "referenced secret was not found")
	}
	botKey, appKey := ref.BotTokenKey, ref.AppTokenKey
	if botKey == "" {
		botKey = "bot-token"
	}
	if appKey == "" {
		appKey = "app-token"
	}
	for _, secret := range settings.SecretSettings() {
		if secret.ID != ref.SecretID {
			continue
		}
		if secret.Values[botKey] == "" || secret.Values[appKey] == "" {
			return echo.NewHTTPError(http.StatusBadRequest, "referenced secret must contain non-empty bot and app token keys")
		}
		for _, projection := range secret.Projections {
			if projection.Key == botKey || projection.Key == appKey {
				return echo.NewHTTPError(http.StatusBadRequest, "Slack token keys cannot be projected")
			}
		}
		ref.BotTokenKey, ref.AppTokenKey = botKey, appKey
		return nil
	}
	return echo.NewHTTPError(http.StatusBadRequest, "referenced secret was not found")
}

func (c *SlackBotController) userCanAccess(ctx echo.Context, bot *entities.SlackBot, userID string) bool {
	if bot.Scope() == entities.ScopeTeam {
		// For team-scoped bots, allow access (team membership checked by middleware)
		return true
	}
	return bot.UserID() == userID
}

func toEntitySessionConfig(cfg *SlackBotSessionConfig) *entities.WebhookSessionConfig {
	if cfg == nil {
		return nil
	}
	sc := entities.NewWebhookSessionConfig()
	sc.SetInitialMessageTemplate(cfg.InitialMessageTemplate)
	sc.SetReuseMessageTemplate(cfg.ReuseMessageTemplate)
	if cfg.Tags != nil {
		sc.SetTags(cfg.Tags)
	}
	if cfg.Environment != nil {
		sc.SetEnvironment(cfg.Environment)
	}
	if cfg.SessionProfileID != "" {
		sc.SetSessionProfileID(cfg.SessionProfileID)
	}
	if cfg.Params != nil {
		params := &entities.SessionParams{
			Pool:             cfg.Params.Pool,
			ManagerID:        cfg.Params.ManagerID,
			AgentType:        cfg.Params.AgentType,
			Model:            cfg.Params.Model,
			Oneshot:          cfg.Params.Oneshot,
			AuthProxy:        cfg.Params.AuthProxy,
			RepoFullName:     cfg.Params.RepoFullName,
			CredentialSource: cfg.Params.CredentialSource,
		}
		sc.SetParams(params)
	}
	return sc
}

func fromEntitySessionConfig(sc *entities.WebhookSessionConfig) *SlackBotSessionConfig {
	if sc == nil {
		return nil
	}
	cfg := &SlackBotSessionConfig{
		InitialMessageTemplate: sc.InitialMessageTemplate(),
		ReuseMessageTemplate:   sc.ReuseMessageTemplate(),
		Tags:                   sc.Tags(),
		Environment:            sc.Environment(),
		SessionProfileID:       sc.SessionProfileID(),
	}
	if sc.Params() != nil {
		cfg.Params = &SlackBotSessionParams{
			Pool:             sc.Params().Pool,
			ManagerID:        sc.Params().ManagerID,
			AgentType:        sc.Params().AgentType,
			Model:            sc.Params().Model,
			Oneshot:          sc.Params().Oneshot,
			AuthProxy:        sc.Params().AuthProxy,
			RepoFullName:     sc.Params().RepoFullName,
			CredentialSource: sc.Params().CredentialSource,
		}
	}
	return cfg
}
