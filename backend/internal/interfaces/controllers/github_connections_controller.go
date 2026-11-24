package controllers

import (
	"context"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	ports "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
	"github.com/takutakahashi/agentapi-proxy/pkg/utils"
	"k8s.io/client-go/kubernetes"
)

const (
	githubConnectionLabel  = "agentapi.ccplant.io/github-connection"
	githubIdentityLabel    = "agentapi.ccplant.io/github-identity"
	githubPrincipalLabel   = "agentapi.ccplant.io/github-principal"
	githubOAuthStateLabel  = "agentapi.ccplant.io/github-oauth-state"
	githubBrokerLeaseLabel = "agentapi.ccplant.io/github-broker-lease"
	githubOAuthStateTTL    = 10 * time.Minute
)

var githubSecretEnvPattern = regexp.MustCompile(`^GITHUB_OAUTH_[A-Z0-9_]+_CLIENT_SECRET$`)

const githubAppPrivateKeyKey = "github_app_private_key"

type githubAppConfiguration struct {
	AppID int64 `json:"app_id"`
}

type githubConnection struct {
	ID                string                  `json:"id"`
	Name              string                  `json:"name"`
	BaseURL           string                  `json:"base_url"`
	APIURL            string                  `json:"api_url"`
	OAuthClientID     string                  `json:"oauth_client_id"`
	OAuthScope        string                  `json:"oauth_scope"`
	SecretSource      string                  `json:"secret_source"`
	SecretEnvironment string                  `json:"secret_environment,omitempty"`
	Enabled           bool                    `json:"enabled"`
	ShowOnLogin       *bool                   `json:"show_on_login"`
	AllowUserCreation bool                    `json:"allow_user_creation"`
	Organizations     []string                `json:"organizations,omitempty"`
	GitHubApp         *githubAppConfiguration `json:"github_app,omitempty"`
	CreatedAt         time.Time               `json:"created_at"`
	UpdatedAt         time.Time               `json:"updated_at"`
}

type githubConnectionResponse struct {
	githubConnection
	SecretConfigured              bool   `json:"secret_configured"`
	GitHubAppPrivateKeyConfigured bool   `json:"github_app_private_key_configured"`
	CallbackURL                   string `json:"callback_url"`
	LinkedIdentities              int    `json:"linked_identities"`
}

type githubConnectionRequest struct {
	Name              string                  `json:"name"`
	BaseURL           string                  `json:"base_url"`
	APIURL            string                  `json:"api_url"`
	OAuthClientID     string                  `json:"oauth_client_id"`
	OAuthScope        string                  `json:"oauth_scope"`
	Enabled           *bool                   `json:"enabled,omitempty"`
	ShowOnLogin       *bool                   `json:"show_on_login,omitempty"`
	AllowUserCreation *bool                   `json:"allow_user_creation,omitempty"`
	Organizations     []string                `json:"organizations,omitempty"`
	GitHubApp         *githubAppConfiguration `json:"github_app,omitempty"`
	Secret            struct {
		Source      string `json:"source"`
		Value       string `json:"value,omitempty"`
		Environment string `json:"environment,omitempty"`
	} `json:"oauth_client_secret"`
}

type githubSecretUpdate struct {
	Source      string `json:"source"`
	Value       string `json:"value,omitempty"`
	Environment string `json:"environment,omitempty"`
}

type githubAppPrivateKeyUpdate struct {
	Value string `json:"value"`
}

type githubAppTestRequest struct {
	Repository string `json:"repository"`
}

type githubBrokerLease struct {
	SessionID    string    `json:"session_id"`
	ConnectionID string    `json:"connection_id"`
	PrincipalID  string    `json:"principal_id,omitempty"`
	Repository   string    `json:"repository"`
	Repositories []string  `json:"repositories,omitempty"`
	ExpiresAt    time.Time `json:"expires_at"`
	TokenHash    string    `json:"token_hash"`
}

type githubCachedToken struct {
	Token     string
	ExpiresAt time.Time
}

type githubPrincipal struct {
	ID                string    `json:"id"`
	InternalUserID    string    `json:"internal_user_id"`
	ApplicationUserID string    `json:"application_user_id,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
}

type principalRepairIdentityRequest struct {
	ConnectionID     string `json:"connection_id"`
	GitHubUserID     int64  `json:"github_user_id"`
	Login            string `json:"login,omitempty"`
	AvatarURL        string `json:"avatar_url,omitempty"`
	ReassignExisting bool   `json:"reassign_existing,omitempty"`
}

type principalRepairRequest struct {
	ClearApplicationUserID bool                             `json:"clear_application_user_id,omitempty"`
	Identities             []principalRepairIdentityRequest `json:"identities,omitempty"`
}

type principalRepairResponse struct {
	Principal  githubPrincipal  `json:"principal"`
	Identities []githubIdentity `json:"identities"`
}

type githubIdentity struct {
	ID           string    `json:"id"`
	PrincipalID  string    `json:"principal_id"`
	ConnectionID string    `json:"connection_id"`
	GitHubUserID int64     `json:"github_user_id"`
	Login        string    `json:"login"`
	AvatarURL    string    `json:"avatar_url,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

const githubAccessTokenKey = "access_token"
const githubExpiresAtKey = "expires_at"

type githubIdentityResponse struct {
	githubIdentity
	ConnectionName string `json:"connection_name"`
	BaseURL        string `json:"base_url"`
	Current        bool   `json:"current"`
}

type githubOAuthState struct {
	ID             string    `json:"id"`
	ConnectionID   string    `json:"connection_id"`
	PrincipalID    string    `json:"principal_id"`
	InternalUserID string    `json:"internal_user_id"`
	ReturnTo       string    `json:"return_to"`
	CallbackURL    string    `json:"callback_url"`
	Mode           string    `json:"mode,omitempty"`
	ExpiresAt      time.Time `json:"expires_at"`
}

type GitHubConnectionLoginResult struct {
	AccessToken  string
	APIURL       string
	UserID       string
	ConnectionID string
}

type githubOAuthUser struct {
	ID        int64  `json:"id"`
	Login     string `json:"login"`
	AvatarURL string `json:"avatar_url"`
}

// GitHubConnectionsController manages administrator-defined GitHub OAuth
// connections and user-owned external identities.
type GitHubConnectionsController struct {
	client           kubernetes.Interface
	namespace        string
	httpClient       *http.Client
	callbackURL      string
	builtInAPIURL    string
	encryptedStorage bool
	brokerMu         sync.Mutex
	tokenCache       map[string]githubCachedToken
	membershipRepo   ports.TeamMembershipRepository
	membershipSyncer interface {
		AutoSyncForPrincipal(context.Context, string, string) error
	}
}

func NewGitHubConnectionsController(client kubernetes.Interface, namespace, publicBaseURL string, encryptedStorage ...bool) *GitHubConnectionsController {
	controller := &GitHubConnectionsController{
		client:        client,
		namespace:     namespace,
		httpClient:    utils.NewPublicHTTPClient(),
		builtInAPIURL: "https://api.github.com",
		tokenCache:    make(map[string]githubCachedToken),
	}
	if publicBaseURL != "" {
		controller.callbackURL = strings.TrimSuffix(publicBaseURL, "/") + "/auth/github-connections/callback"
	}
	if len(encryptedStorage) > 0 {
		controller.encryptedStorage = encryptedStorage[0]
	}
	return controller
}

// SetBuiltInAPIURL configures the API endpoint used with credentials issued by
// the deployment-wide GitHub OAuth provider. It differs from administrator-
// defined GitHub connections, whose API endpoints are stored per connection.
func (c *GitHubConnectionsController) SetBuiltInAPIURL(apiURL string) {
	if normalized := strings.TrimSpace(apiURL); normalized != "" {
		c.builtInAPIURL = strings.TrimSuffix(normalized, "/")
	}
}

func (c *GitHubConnectionsController) builtInConnection() githubConnection {
	return githubConnection{ID: "github", APIURL: c.builtInAPIURL, Enabled: true}
}

func (c *GitHubConnectionsController) isBuiltInConnection(connection githubConnection) bool {
	builtInAPIURL := strings.TrimSuffix(strings.TrimSpace(c.builtInAPIURL), "/")
	connectionAPIURL := strings.TrimSuffix(strings.TrimSpace(connection.APIURL), "/")
	if builtInAPIURL != "" && connectionAPIURL != "" {
		return strings.EqualFold(builtInAPIURL, connectionAPIURL)
	}
	// Keep compatibility with principals created by the default GitHub.com
	// provider before the built-in API URL was recorded by this controller.
	return strings.EqualFold(strings.TrimSuffix(strings.TrimSpace(connection.BaseURL), "/"), "https://github.com")
}

func (c *GitHubConnectionsController) SetMembershipRepository(repo ports.TeamMembershipRepository) {
	c.membershipRepo = repo
}

func (c *GitHubConnectionsController) SetMembershipSyncer(syncer interface {
	AutoSyncForPrincipal(context.Context, string, string) error
}) {
	c.membershipSyncer = syncer
}
