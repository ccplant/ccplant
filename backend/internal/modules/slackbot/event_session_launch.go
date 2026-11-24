package slackbot

import (
	"context"
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	sessionuc "github.com/takutakahashi/agentapi-proxy/internal/usecases/session"
	"github.com/takutakahashi/agentapi-proxy/pkg/telemetry"
)

type slackSessionLaunchPlan struct {
	eventKey, botID, channel, threadKey, sessionID   string
	configuredRepo, detectedRepo, triggeredUserID    string
	userID, teamID, agentType, model, initialMessage string
	teams                                            []string
	scope                                            entities.ResourceScope
	maxSessions                                      int
	environment                                      map[string]string
	tags, reuseTags                                  map[string]string
	payload                                          map[string]interface{}
	eventText                                        string
	bot                                              *entities.SlackBot
}

func (h *SlackBotEventHandler) launchSlackSession(ctx context.Context, plan slackSessionLaunchPlan) {
	if h.dryRun {
		log.Printf("[SLACKBOT] [DRY-RUN] Would create session: id=%s, channel=%s, thread=%s, agentType=%s, scope=%s", plan.sessionID, plan.channel, plan.threadKey, plan.agentType, plan.scope)
		if plan.bot.NotifyOnSessionCreated() {
			h.postSessionURLToSlack(ctx, plan.channel, plan.threadKey, plan.sessionID, plan.tags["repository"], plan.bot)
		}
		return
	}

	result, err := telemetry.LoggedOperation(ctx, "slackbot.LaunchSession", func(launchCtx context.Context) (sessionuc.LaunchResult, error) {
		return h.launcher.Launch(launchCtx, plan.sessionID, h.buildSlackLaunchRequest(plan))
	}, telemetry.String("slackbot.id", plan.botID), telemetry.String("slack.channel", plan.channel), telemetry.String("slack.thread_ts", plan.threadKey))
	if err != nil {
		h.setOutcome(simulationDecisionError)
		h.processedEvents.Delete(plan.eventKey)
		log.Printf("[SLACKBOT] Failed to create session: %v", err)
		return
	}
	if result.SessionReused {
		log.Printf("[SLACKBOT] Reused session %s for thread %s", result.SessionID, plan.threadKey)
	} else {
		log.Printf("[SLACKBOT] Created session %s for thread %s", result.SessionID, plan.threadKey)
	}
	if !result.SessionReused && plan.bot.NotifyOnSessionCreated() {
		h.postSessionURLToSlack(ctx, plan.channel, plan.threadKey, result.SessionID, plan.tags["repository"], plan.bot)
	}
}

func (h *SlackBotEventHandler) buildSlackLaunchRequest(plan slackSessionLaunchPlan) sessionuc.LaunchRequest {
	var repoInfo *entities.RepositoryInfo
	effectiveRepo := plan.configuredRepo
	if effectiveRepo == "" {
		effectiveRepo = plan.detectedRepo
	}
	if effectiveRepo != "" {
		repoInfo = &entities.RepositoryInfo{FullName: effectiveRepo, CloneDir: "/home/agentapi/workdir/repo"}
	}

	request := sessionuc.LaunchRequest{
		UserID: plan.userID, TriggeredUserID: plan.triggeredUserID, Scope: plan.scope, TeamID: plan.teamID, Teams: plan.teams,
		Environment: plan.environment, Tags: plan.tags, InitialMessage: plan.initialMessage,
		ReuseSession: true, ReuseMatchTags: plan.reuseTags, ReuseMessage: h.buildMessage(plan.bot, plan.payload, plan.eventText, true),
		StopBeforeReuse: true, DeferReuseToStart: true, MaxSessions: plan.maxSessions, LimitMatchTags: map[string]string{"slackbot_id": plan.botID},
		AgentType: plan.agentType, Model: plan.model, RepoInfo: repoInfo,
		SlackParams: h.slackLaunchParams(plan.channel, plan.threadKey, plan.bot),
	}
	if config := plan.bot.SessionConfig(); config != nil {
		request.SessionProfileID = config.SessionProfileID()
		if params := config.Params(); params != nil {
			request.Pool, request.ManagerID = params.Pool, params.ManagerID
			request.Sandbox, request.Docker, request.AuthProxy = params.Sandbox, params.Docker, params.AuthProxy
			request.InitialMessageWaitSecond = params.InitialMessageWaitSecond
			request.CycleMessage, request.CycleMaxCount = params.CycleMessage, params.CycleMaxCount
			request.SessionTTL = sessionuc.ResolveSessionTTL(params)
			request.CredentialSource = params.CredentialSource
		}
	}
	return request
}

func (h *SlackBotEventHandler) slackLaunchParams(channel, threadKey string, bot *entities.SlackBot) *entities.SlackParams {
	params := &entities.SlackParams{Channel: channel, ThreadTS: threadKey, BotTokenSecretName: h.defaultBotTokenSecretName, BotTokenSecretKey: h.defaultBotTokenSecretKey}
	if bot.BotTokenSecretName() != "" {
		params.BotTokenSecretName = bot.BotTokenSecretName()
	}
	if bot.BotTokenSecretKey() != "" {
		params.BotTokenSecretKey = bot.BotTokenSecretKey()
	}
	return params
}
