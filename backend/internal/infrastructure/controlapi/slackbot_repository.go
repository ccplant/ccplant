package controlapi

import (
	"context"
	"fmt"
	"net/http"
	"net/url"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/slackbot"
	"github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

// SlackBotRepository is a read-only worker view of API-owned SlackBot data.
type SlackBotRepository struct{ client *SessionManager }

func NewSlackBotRepository(client *SessionManager) *SlackBotRepository {
	return &SlackBotRepository{client: client}
}

func (r *SlackBotRepository) ListAll(ctx context.Context) ([]*entities.SlackBot, error) {
	var responses []*slackbot.SlackBotResponse
	if err := r.client.do(ctx, http.MethodGet, "/internal/worker/slackbots", nil, &responses); err != nil {
		return nil, err
	}
	result := make([]*entities.SlackBot, 0, len(responses))
	for _, response := range responses {
		result = append(result, slackBotEntity(response))
	}
	return result, nil
}

func (r *SlackBotRepository) Get(ctx context.Context, id string) (*entities.SlackBot, error) {
	bots, err := r.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	for _, bot := range bots {
		if bot.ID() == id {
			return bot, nil
		}
	}
	return nil, entities.ErrSlackBotNotFound{ID: id}
}

func (r *SlackBotRepository) Credentials(ctx context.Context, id string) (string, string, error) {
	var response struct {
		AppToken string `json:"app_token"`
		BotToken string `json:"bot_token"`
	}
	err := r.client.do(ctx, http.MethodGet, "/internal/worker/slackbots/"+url.PathEscape(id)+"/credentials", nil, &response)
	return response.AppToken, response.BotToken, err
}

func (*SlackBotRepository) Create(context.Context, *entities.SlackBot) error {
	return fmt.Errorf("worker SlackBot repository is read-only")
}
func (*SlackBotRepository) Update(context.Context, *entities.SlackBot) error {
	return fmt.Errorf("worker SlackBot repository is read-only")
}
func (*SlackBotRepository) Delete(context.Context, string) error {
	return fmt.Errorf("worker SlackBot repository is read-only")
}
func (r *SlackBotRepository) List(ctx context.Context, filter repositories.SlackBotFilter) ([]*entities.SlackBot, error) {
	bots, err := r.ListAll(ctx)
	if err != nil {
		return nil, err
	}
	result := make([]*entities.SlackBot, 0, len(bots))
	for _, bot := range bots {
		if filter.UserID != "" && bot.UserID() != filter.UserID {
			continue
		}
		if filter.Status != "" && bot.Status() != filter.Status {
			continue
		}
		if filter.Scope != "" && bot.Scope() != filter.Scope {
			continue
		}
		if filter.TeamID != "" && bot.TeamID() != filter.TeamID {
			continue
		}
		result = append(result, bot)
	}
	return result, nil
}

func slackBotEntity(response *slackbot.SlackBotResponse) *entities.SlackBot {
	bot := entities.NewSlackBot(response.ID, response.Name, response.UserID)
	bot.SetOwnership(response.Scope, response.UserID, response.TeamID)
	bot.SetTeams(response.Teams)
	bot.SetStatus(response.Status)
	bot.SetBotTokenSecretName(response.BotTokenSecretName)
	bot.SetBotTokenSecretKey(response.BotTokenSecretKey)
	bot.SetAllowedEventTypes(response.AllowedEventTypes)
	bot.SetAllowedChannelNames(response.AllowedChannelNames)
	bot.SetAllowedUserIDs(response.AllowedUserIDs)
	bot.SetSessionConfig(slackbotSessionConfig(response.SessionConfig))
	bot.SetMaxSessions(response.MaxSessions)
	bot.SetNotifyOnSessionCreated(&response.NotifyOnSessionCreated)
	bot.SetAllowBotMessages(&response.AllowBotMessages)
	if response.Secret != nil {
		bot.SetSettingsSecret(response.Secret.SecretID, response.Secret.BotTokenKey, response.Secret.AppTokenKey)
	}
	bot.SetCreatedAt(response.CreatedAt)
	bot.SetUpdatedAt(response.UpdatedAt)
	return bot
}

func slackbotSessionConfig(config *slackbot.SlackBotSessionConfig) *entities.WebhookSessionConfig {
	if config == nil {
		return nil
	}
	result := entities.NewWebhookSessionConfig()
	result.SetInitialMessageTemplate(config.InitialMessageTemplate)
	result.SetReuseMessageTemplate(config.ReuseMessageTemplate)
	result.SetTags(config.Tags)
	result.SetEnvironment(config.Environment)
	result.SetSessionProfileID(config.SessionProfileID)
	if config.Params != nil {
		result.SetParams(&entities.SessionParams{Pool: config.Params.Pool, ManagerID: config.Params.ManagerID, AgentType: config.Params.AgentType, Model: config.Params.Model, Oneshot: config.Params.Oneshot, AuthProxy: config.Params.AuthProxy, RepoFullName: config.Params.RepoFullName, CredentialSource: config.Params.CredentialSource})
	}
	return result
}
