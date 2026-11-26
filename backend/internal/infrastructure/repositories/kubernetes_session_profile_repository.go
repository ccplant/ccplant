package repositories

import (
	"context"
	"encoding/json"
	stderrors "errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/takutakahashi/agentapi-proxy/pkg/modelprovider"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	portrepos "github.com/takutakahashi/agentapi-proxy/internal/usecases/ports/repositories"
)

const (
	LabelSessionProfile       = "agentapi.proxy/session-profile"
	LabelSessionProfileID     = "agentapi.proxy/session-profile-id"
	LabelSessionProfileScope  = "agentapi.proxy/session-profile-scope"
	LabelSessionProfileUserID = "agentapi.proxy/session-profile-user-id"
	// LabelSessionProfileTeamIDHash is the label key for hashed team ID
	LabelSessionProfileTeamIDHash = "agentapi.proxy/session-profile-team-id-hash"
	// AnnotationSessionProfileTeamID stores the original (unescaped) team ID
	AnnotationSessionProfileTeamID = "agentapi.proxy/session-profile-team-id"
	// SecretKeySessionProfile is the key in the Secret data for session profile JSON
	SecretKeySessionProfile = "session-profile.json"
	// SessionProfileSecretPrefix is the prefix for session profile Secret names
	SessionProfileSecretPrefix       = "agentapi-session-profile-"
	sessionProfileGraphRevisionKey   = "agentapi-session-profile-graph-revision"
	labelSessionProfileGraphRevision = "agentapi.proxy/session-profile-graph-revision"
)

// sessionProfileJSON is the JSON representation for storage
type sessionProfileJSON struct {
	ID                   string                   `json:"id"`
	Name                 string                   `json:"name"`
	Description          string                   `json:"description,omitempty"`
	UserID               string                   `json:"user_id"`
	Scope                entities.ResourceScope   `json:"scope,omitempty"`
	TeamID               string                   `json:"team_id,omitempty"`
	IsDefault            bool                     `json:"is_default,omitempty"`
	SelectorTags         map[string]string        `json:"selector_tags,omitempty"`
	SelectorRepositories []string                 `json:"selector_repositories,omitempty"`
	Config               sessionProfileConfigJSON `json:"config"`
	CreatedAt            time.Time                `json:"created_at"`
	UpdatedAt            time.Time                `json:"updated_at"`
}

type sessionProfileConfigJSON struct {
	SettingsTeamID         string                       `json:"settings_team_id,omitempty"`
	CodexConnection        *connectionJSON              `json:"codex_connection,omitempty"`
	ClaudeConnection       *connectionJSON              `json:"claude_connection,omitempty"`
	PiConnection           *connectionJSON              `json:"pi_connection,omitempty"`
	Environment            map[string]string            `json:"environment,omitempty"`
	Tags                   map[string]string            `json:"tags,omitempty"`
	Pool                   string                       `json:"pool,omitempty"`
	InitialMessageTemplate string                       `json:"initial_message_template,omitempty"`
	ReuseMessageTemplate   string                       `json:"reuse_message_template,omitempty"`
	CommandWrapperTemplate string                       `json:"command_wrapper_template,omitempty"`
	SystemPrompt           string                       `json:"system_prompt,omitempty"`
	SystemPromptMode       string                       `json:"system_prompt_mode,omitempty"`
	Params                 *entities.SessionParams      `json:"params,omitempty"`
	ReuseSession           bool                         `json:"reuse_session,omitempty"`
	SandboxPolicyID        string                       `json:"sandbox_policy_id,omitempty"`
	SessionTTL             string                       `json:"session_ttl,omitempty"`
	UnsyncedFilePaths      []string                     `json:"unsynced_file_paths,omitempty"`
	SourceSessionProfileID string                       `json:"source_session_profile_id,omitempty"`
	SecretIDs              []string                     `json:"secret_ids,omitempty"`
	GitHubRepositories     []string                     `json:"github_repositories,omitempty"`
	Skills                 []string                     `json:"skills,omitempty"`
	PiPackages             []string                     `json:"pi_packages,omitempty"`
	Files                  []entities.ProfileFile       `json:"files,omitempty"`
	Repositories           []entities.ProfileRepository `json:"repositories,omitempty"`
	MCPServers             map[string]*mcpServerJSON    `json:"mcp_servers,omitempty"`
	SessionState           *entities.SessionStateConfig `json:"session_state,omitempty"`
	PolicyYAML             string                       `json:"policy_yaml,omitempty"`
}

// KubernetesSessionProfileRepository implements SessionProfileRepository using Kubernetes Secrets
type KubernetesSessionProfileRepository struct {
	client      kubernetes.Interface
	namespace   string
	mu          sync.RWMutex
	connections *KubernetesSettingsRepository
	atomicStore kvstore.Store
}

// NewLibSQLSessionProfileRepository enables graph-wide CAS for the libSQL
// backend while retaining the Kubernetes-shaped profile document format.
func NewLibSQLSessionProfileRepository(client kubernetes.Interface, namespace string, store kvstore.Store, registry ...*services.EncryptionServiceRegistry) *KubernetesSessionProfileRepository {
	repo := NewKubernetesSessionProfileRepository(client, namespace, registry...)
	repo.atomicStore = store
	return repo
}

func NewSessionProfileRepository(client kubernetes.Interface, namespace string, store kvstore.Store, registry ...*services.EncryptionServiceRegistry) *KubernetesSessionProfileRepository {
	if _, ok := store.(kvstore.AtomicMutator); ok {
		return NewLibSQLSessionProfileRepository(client, namespace, store, registry...)
	}
	return NewKubernetesSessionProfileRepository(client, namespace, registry...)
}

// NewKubernetesSessionProfileRepository creates a new KubernetesSessionProfileRepository
func NewKubernetesSessionProfileRepository(client kubernetes.Interface, namespace string, registry ...*services.EncryptionServiceRegistry) *KubernetesSessionProfileRepository {
	return &KubernetesSessionProfileRepository{
		client:      client,
		namespace:   namespace,
		connections: NewKubernetesSettingsRepository(client, namespace, registry...),
	}
}

func sessionProfileSecretName(id string) string {
	return SessionProfileSecretPrefix + id
}

// Create creates a new session profile
func (r *KubernetesSessionProfileRepository) Create(ctx context.Context, profile *entities.SessionProfile) error {
	if err := profile.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.atomicStore != nil {
		return r.createAtomically(ctx, profile)
	}

	secretName := sessionProfileSecretName(profile.ID())
	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err == nil {
		return fmt.Errorf("session profile already exists: %s", profile.ID())
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("failed to check session profile existence: %w", err)
	}

	return r.saveProfile(ctx, profile)
}

func (r *KubernetesSessionProfileRepository) createAtomically(ctx context.Context, profile *entities.SessionProfile) error {
	mutator := r.atomicStore.(kvstore.AtomicMutator)
	for attempt := 0; attempt < 4; attempt++ {
		revision, profiles, err := r.graphSnapshot(ctx)
		if err != nil {
			return err
		}
		for _, existing := range profiles {
			if existing.ID() == profile.ID() {
				return fmt.Errorf("session profile already exists: %s", profile.ID())
			}
		}
		if err := validateSessionProfileGraph(profiles, profile); err != nil {
			return err
		}
		secret, err := r.buildProfileSecret(ctx, profile)
		if err != nil {
			return err
		}
		profileRecord, err := secretToRecord(secret, 0)
		if err != nil {
			return err
		}
		mutations := []kvstore.Mutation{
			{Type: kvstore.MutationCreate, Record: profileRecord},
			{Type: kvstore.MutationUpdate, Record: revision},
		}
		if _, err := mutator.ApplyAtomically(ctx, mutations); err != nil {
			if stderrors.Is(err, kvstore.ErrConflict) {
				continue
			}
			return fmt.Errorf("atomically create session profile: %w", err)
		}
		return nil
	}
	return portrepos.ErrSessionProfileGraphConflict
}

// Get retrieves a session profile by ID
func (r *KubernetesSessionProfileRepository) Get(ctx context.Context, id string) (*entities.SessionProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return r.loadProfile(ctx, id)
}

// List retrieves session profiles matching the filter
func (r *KubernetesSessionProfileRepository) List(ctx context.Context, filter portrepos.SessionProfileFilter) ([]*entities.SessionProfile, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	profiles, err := r.loadProfiles(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("failed to load session profiles: %w", err)
	}

	var result []*entities.SessionProfile
	for _, p := range profiles {
		if filter.UserID != "" && p.UserID() != filter.UserID {
			continue
		}
		if filter.Scope != "" && p.Scope() != filter.Scope {
			continue
		}
		if filter.TeamID != "" && p.TeamID() != filter.TeamID {
			continue
		}
		if len(filter.TeamIDs) > 0 && p.Scope() == entities.ScopeTeam {
			teamMatch := false
			for _, teamID := range filter.TeamIDs {
				if p.TeamID() == teamID {
					teamMatch = true
					break
				}
			}
			if !teamMatch {
				continue
			}
		}
		result = append(result, p)
	}

	return result, nil
}

// Update updates an existing session profile
func (r *KubernetesSessionProfileRepository) Update(ctx context.Context, profile *entities.SessionProfile) error {
	if err := profile.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.atomicStore != nil {
		return r.updateAtomically(ctx, profile)
	}

	secretName := sessionProfileSecretName(profile.ID())
	_, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return entities.ErrSessionProfileNotFound{ID: profile.ID()}
		}
		return fmt.Errorf("failed to get session profile: %w", err)
	}

	profile.SetUpdatedAt(time.Now())
	return r.saveProfile(ctx, profile)
}

func (r *KubernetesSessionProfileRepository) updateAtomically(ctx context.Context, profile *entities.SessionProfile) error {
	mutator, ok := r.atomicStore.(kvstore.AtomicMutator)
	if !ok {
		return stderrors.New("libSQL session profile repository requires atomic KV updates")
	}
	for attempt := 0; attempt < 4; attempt++ {
		revision, profiles, err := r.graphSnapshot(ctx)
		if err != nil {
			return err
		}
		if err := validateSessionProfileGraph(profiles, profile); err != nil {
			return fmt.Errorf("%w: %v", portrepos.ErrSessionProfileGraphConflict, err)
		}

		secretName := sessionProfileSecretName(profile.ID())
		existing, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				return entities.ErrSessionProfileNotFound{ID: profile.ID()}
			}
			return fmt.Errorf("failed to get session profile: %w", err)
		}
		profile.SetUpdatedAt(time.Now())
		secret, err := r.buildProfileSecret(ctx, profile)
		if err != nil {
			return err
		}
		secret.ResourceVersion = existing.ResourceVersion
		value, err := json.Marshal(secret)
		if err != nil {
			return err
		}
		version, err := strconv.ParseInt(existing.ResourceVersion, 10, 64)
		if err != nil {
			return fmt.Errorf("parse session profile resource version: %w", err)
		}
		profileRecord := kvstore.Record{Kind: kvstore.KindSecret, Namespace: r.namespace, Key: secretName, Labels: secret.Labels, Value: value, Version: version}
		mutations := []kvstore.Mutation{
			{Type: kvstore.MutationUpdate, Record: profileRecord},
			{Type: kvstore.MutationUpdate, Record: revision},
		}
		if _, err := mutator.ApplyAtomically(ctx, mutations); err != nil {
			if stderrors.Is(err, kvstore.ErrConflict) {
				continue
			}
			return fmt.Errorf("atomically update session profile graph: %w", err)
		}
		return nil
	}
	return portrepos.ErrSessionProfileGraphConflict
}

func (r *KubernetesSessionProfileRepository) graphSnapshot(ctx context.Context) (kvstore.Record, []*entities.SessionProfile, error) {
	for attempt := 0; attempt < 4; attempt++ {
		revision, err := r.graphRevision(ctx)
		if err != nil {
			return kvstore.Record{}, nil, err
		}
		profiles, err := r.loadProfiles(ctx, portrepos.SessionProfileFilter{})
		if err != nil {
			return kvstore.Record{}, nil, err
		}
		current, err := r.atomicStore.Get(ctx, revision.Kind, revision.Namespace, revision.Key)
		if err != nil {
			return kvstore.Record{}, nil, err
		}
		if current.Version == revision.Version {
			return current, profiles, nil
		}
	}
	return kvstore.Record{}, nil, portrepos.ErrSessionProfileGraphConflict
}

func (r *KubernetesSessionProfileRepository) graphRevision(ctx context.Context) (kvstore.Record, error) {
	record, err := r.atomicStore.Get(ctx, kvstore.KindConfigMap, r.namespace, sessionProfileGraphRevisionKey)
	if err == nil {
		return record, nil
	}
	if !stderrors.Is(err, kvstore.ErrNotFound) {
		return kvstore.Record{}, err
	}
	labels := map[string]string{labelSessionProfileGraphRevision: "true"}
	object := &corev1.ConfigMap{ObjectMeta: metav1.ObjectMeta{Name: sessionProfileGraphRevisionKey, Namespace: r.namespace, Labels: labels}, Data: map[string]string{"revision": "0"}}
	value, marshalErr := json.Marshal(object)
	if marshalErr != nil {
		return kvstore.Record{}, marshalErr
	}
	record = kvstore.Record{Kind: kvstore.KindConfigMap, Namespace: r.namespace, Key: sessionProfileGraphRevisionKey, Labels: labels, Value: value}
	created, createErr := r.atomicStore.Create(ctx, record)
	if stderrors.Is(createErr, kvstore.ErrConflict) {
		return r.atomicStore.Get(ctx, record.Kind, record.Namespace, record.Key)
	}
	return created, createErr
}

func validateSessionProfileGraph(profiles []*entities.SessionProfile, candidate *entities.SessionProfile) error {
	byID := make(map[string]*entities.SessionProfile, len(profiles))
	for _, profile := range profiles {
		byID[profile.ID()] = profile
	}
	byID[candidate.ID()] = candidate
	for _, profile := range byID {
		visited := map[string]struct{}{profile.ID(): {}}
		current := profile
		currentConfig := current.Config()
		for sourceID := currentConfig.SourceSessionProfileID(); sourceID != ""; sourceID = currentConfig.SourceSessionProfileID() {
			if _, seen := visited[sourceID]; seen {
				return fmt.Errorf("session profile source contains a cyclic reference")
			}
			visited[sourceID] = struct{}{}
			source, exists := byID[sourceID]
			if !exists {
				return entities.ErrSessionProfileNotFound{ID: sourceID}
			}
			if !sessionProfilesShareTenant(source, profile) {
				return fmt.Errorf("session profile source belongs to another tenant")
			}
			current = source
			currentConfig = current.Config()
		}
	}
	return nil
}

func sessionProfilesShareTenant(source, profile *entities.SessionProfile) bool {
	switch profile.Scope() {
	case entities.ScopeUser:
		return source.Scope() == entities.ScopeUser && source.UserID() == profile.UserID()
	case entities.ScopeTeam:
		return source.Scope() == entities.ScopeTeam && source.TeamID() == profile.TeamID()
	default:
		return false
	}
}

// Delete removes a session profile by ID
func (r *KubernetesSessionProfileRepository) Delete(ctx context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.atomicStore != nil {
		return r.deleteAtomically(ctx, id)
	}

	secretName := sessionProfileSecretName(id)
	err := r.client.CoreV1().Secrets(r.namespace).Delete(ctx, secretName, metav1.DeleteOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return entities.ErrSessionProfileNotFound{ID: id}
		}
		return fmt.Errorf("failed to delete session profile secret: %w", err)
	}

	return nil
}

func (r *KubernetesSessionProfileRepository) deleteAtomically(ctx context.Context, id string) error {
	mutator := r.atomicStore.(kvstore.AtomicMutator)
	for attempt := 0; attempt < 4; attempt++ {
		revision, profiles, err := r.graphSnapshot(ctx)
		if err != nil {
			return err
		}
		found := false
		for _, profile := range profiles {
			if profile.ID() == id {
				found = true
				continue
			}
			cfg := profile.Config()
			if cfg.SourceSessionProfileID() == id {
				return fmt.Errorf("%w: session profile is referenced by %s", portrepos.ErrSessionProfileGraphConflict, profile.ID())
			}
		}
		if !found {
			return entities.ErrSessionProfileNotFound{ID: id}
		}
		secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, sessionProfileSecretName(id), metav1.GetOptions{})
		if err != nil {
			return err
		}
		version, err := strconv.ParseInt(secret.ResourceVersion, 10, 64)
		if err != nil {
			return err
		}
		profileRecord := kvstore.Record{Kind: kvstore.KindSecret, Namespace: r.namespace, Key: secret.Name, Labels: secret.Labels, Version: version}
		mutations := []kvstore.Mutation{
			{Type: kvstore.MutationDelete, Record: profileRecord},
			{Type: kvstore.MutationUpdate, Record: revision},
		}
		if _, err := mutator.ApplyAtomically(ctx, mutations); err != nil {
			if stderrors.Is(err, kvstore.ErrConflict) {
				continue
			}
			return fmt.Errorf("atomically delete session profile: %w", err)
		}
		return nil
	}
	return portrepos.ErrSessionProfileGraphConflict
}

func secretToRecord(secret *corev1.Secret, version int64) (kvstore.Record, error) {
	value, err := json.Marshal(secret)
	if err != nil {
		return kvstore.Record{}, err
	}
	return kvstore.Record{Kind: kvstore.KindSecret, Namespace: secret.Namespace, Key: secret.Name, Labels: secret.Labels, Value: value, Version: version}, nil
}

func (r *KubernetesSessionProfileRepository) loadProfile(ctx context.Context, id string) (*entities.SessionProfile, error) {
	secretName := sessionProfileSecretName(id)
	secret, err := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, entities.ErrSessionProfileNotFound{ID: id}
		}
		return nil, fmt.Errorf("failed to get session profile secret: %w", err)
	}

	data, ok := secret.Data[SecretKeySessionProfile]
	if !ok {
		return nil, fmt.Errorf("session profile secret missing data key: %s", SecretKeySessionProfile)
	}

	var pj sessionProfileJSON
	if err := json.Unmarshal(data, &pj); err != nil {
		return nil, fmt.Errorf("failed to unmarshal session profile: %w", err)
	}

	// Restore team ID from annotation if present
	if annotationTeamID, ok := secret.Annotations[AnnotationSessionProfileTeamID]; ok && annotationTeamID != "" {
		pj.TeamID = annotationTeamID
	}

	return r.decodeProfile(ctx, &pj)
}

// loadProfiles pushes ownership filters into the persistence query. Session
// launch calls List for every request, so loading every profile in the cluster
// here made trigger latency proportional to the total KV record count.
func (r *KubernetesSessionProfileRepository) loadProfiles(ctx context.Context, filter portrepos.SessionProfileFilter) ([]*entities.SessionProfile, error) {
	selectors := []string{fmt.Sprintf("%s=true", LabelSessionProfile)}
	if filter.UserID != "" {
		selectors = append(selectors, fmt.Sprintf("%s=%s", LabelSessionProfileUserID, sanitizeLabelValue(filter.UserID)))
	}
	if filter.Scope != "" {
		selectors = append(selectors, fmt.Sprintf("%s=%s", LabelSessionProfileScope, filter.Scope))
	}
	if filter.TeamID != "" {
		selectors = append(selectors, fmt.Sprintf("%s=%s", LabelSessionProfileTeamIDHash, services.HashTeamID(filter.TeamID)))
	}
	secrets, err := r.client.CoreV1().Secrets(r.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: strings.Join(selectors, ","),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to list session profile secrets: %w", err)
	}

	result := make([]*entities.SessionProfile, 0, len(secrets.Items))
	for _, secret := range secrets.Items {
		data, ok := secret.Data[SecretKeySessionProfile]
		if !ok {
			continue
		}

		var pj sessionProfileJSON
		if err := json.Unmarshal(data, &pj); err != nil {
			continue
		}

		if annotationTeamID, ok := secret.Annotations[AnnotationSessionProfileTeamID]; ok && annotationTeamID != "" {
			pj.TeamID = annotationTeamID
		}

		profile, err := r.decodeProfile(ctx, &pj)
		if err != nil {
			return nil, err
		}
		result = append(result, profile)
	}

	return result, nil
}

func (r *KubernetesSessionProfileRepository) saveProfile(ctx context.Context, profile *entities.SessionProfile) error {
	secret, err := r.buildProfileSecret(ctx, profile)
	if err != nil {
		return err
	}
	secretName := secret.Name
	_, err = r.client.CoreV1().Secrets(r.namespace).Create(ctx, secret, metav1.CreateOptions{})
	if err != nil {
		if apierrors.IsAlreadyExists(err) {
			existing, getErr := r.client.CoreV1().Secrets(r.namespace).Get(ctx, secretName, metav1.GetOptions{})
			if getErr != nil {
				return fmt.Errorf("failed to get existing secret: %w", getErr)
			}
			existing.Data = secret.Data
			existing.Labels = secret.Labels
			existing.Annotations = secret.Annotations
			_, err = r.client.CoreV1().Secrets(r.namespace).Update(ctx, existing, metav1.UpdateOptions{})
			if err != nil {
				return fmt.Errorf("failed to update session profile secret: %w", err)
			}
			return nil
		}
		return fmt.Errorf("failed to create session profile secret: %w", err)
	}
	return nil
}

func (r *KubernetesSessionProfileRepository) buildProfileSecret(ctx context.Context, profile *entities.SessionProfile) (*corev1.Secret, error) {
	pj := r.entityToJSON(profile)
	cfg := profile.Config()
	for _, c := range []*modelprovider.Connection{cfg.CodexConnection(), cfg.ClaudeConnection(), cfg.PiConnection()} {
		if c != nil && c.APIKey != "" {
			svc := r.connections.encryptionSvc()
			if svc == nil || svc.Algorithm() == "noop" {
				return nil, fmt.Errorf("encryption must be configured to store session profile API keys")
			}
		}
	}
	var err error
	pj.Config.CodexConnection, err = r.connections.encodeConnection(ctx, cfg.CodexConnection())
	if err != nil {
		return nil, err
	}
	pj.Config.ClaudeConnection, err = r.connections.encodeConnection(ctx, cfg.ClaudeConnection())
	if err != nil {
		return nil, err
	}
	pj.Config.PiConnection, err = r.connections.encodeConnection(ctx, cfg.PiConnection())
	if err != nil {
		return nil, err
	}
	data, err := json.Marshal(pj)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal session profile: %w", err)
	}

	secretName := sessionProfileSecretName(profile.ID())
	labels := map[string]string{
		LabelSessionProfile:       "true",
		LabelSessionProfileID:     profile.ID(),
		LabelSessionProfileScope:  string(profile.Scope()),
		LabelSessionProfileUserID: profile.UserID(),
	}
	annotations := make(map[string]string)
	if profile.TeamID() != "" {
		labels[LabelSessionProfileTeamIDHash] = services.HashTeamID(profile.TeamID())
		annotations[AnnotationSessionProfileTeamID] = profile.TeamID()
	}

	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:        secretName,
			Namespace:   r.namespace,
			Labels:      labels,
			Annotations: annotations,
		},
		Type: corev1.SecretTypeOpaque,
		Data: map[string][]byte{
			SecretKeySessionProfile: data,
		},
	}, nil
}

func (r *KubernetesSessionProfileRepository) jsonToEntity(pj *sessionProfileJSON) *entities.SessionProfile {
	profile := entities.NewSessionProfile(pj.ID, pj.Name, pj.UserID)
	profile.SetDescription(pj.Description)
	profile.SetScope(pj.Scope)
	profile.SetTeamID(pj.TeamID)
	profile.SetIsDefault(pj.IsDefault)
	profile.SetSelectorTags(pj.SelectorTags)
	profile.SetSelectorRepositories(pj.SelectorRepositories)
	profile.SetCreatedAt(pj.CreatedAt)
	profile.SetUpdatedAt(pj.UpdatedAt)

	cfg := entities.NewSessionProfileConfig()
	cfg.SetEnvironment(pj.Config.Environment)
	cfg.SetTags(pj.Config.Tags)
	cfg.SetPool(pj.Config.Pool)
	cfg.SetSettingsTeamID(pj.Config.SettingsTeamID)
	cfg.SetInitialMessageTemplate(pj.Config.InitialMessageTemplate)
	cfg.SetReuseMessageTemplate(pj.Config.ReuseMessageTemplate)
	cfg.SetCommandWrapperTemplate(pj.Config.CommandWrapperTemplate)
	cfg.SetSystemPrompt(pj.Config.SystemPrompt)
	cfg.SetSystemPromptMode(pj.Config.SystemPromptMode)
	cfg.SetParams(pj.Config.Params)
	cfg.SetReuseSession(pj.Config.ReuseSession)
	cfg.SetSandboxPolicyID(pj.Config.SandboxPolicyID)
	cfg.SetSessionTTL(pj.Config.SessionTTL)
	cfg.SetUnsyncedFilePaths(pj.Config.UnsyncedFilePaths)
	cfg.SetSourceSessionProfileID(pj.Config.SourceSessionProfileID)
	cfg.SetSecretIDs(pj.Config.SecretIDs)
	cfg.SetGitHubRepositories(pj.Config.GitHubRepositories)
	cfg.SetSkills(pj.Config.Skills)
	cfg.SetPiPackages(pj.Config.PiPackages)
	cfg.SetSessionState(pj.Config.SessionState)
	cfg.SetPolicyYAML(pj.Config.PolicyYAML)
	cfg.SetProfileFiles(pj.Config.Files)
	cfg.SetProfileRepositories(pj.Config.Repositories)
	if pj.Config.MCPServers != nil {
		servers := entities.NewMCPServersSettings()
		for name, item := range pj.Config.MCPServers {
			if item == nil {
				continue
			}
			server := entities.NewMCPServer(name, item.Type)
			server.SetURL(item.URL)
			server.SetCommand(item.Command)
			server.SetArgs(item.Args)
			server.SetEnv(item.Env)
			server.SetHeaders(item.Headers)
			servers.SetServer(name, server)
		}
		cfg.SetMCPServers(servers)
	}
	profile.SetConfig(cfg)

	return profile
}

func (r *KubernetesSessionProfileRepository) entityToJSON(profile *entities.SessionProfile) *sessionProfileJSON {
	cfg := profile.Config()
	mcpServers := make(map[string]*mcpServerJSON)
	if cfg.MCPServers() != nil {
		for name, server := range cfg.MCPServers().Servers() {
			mcpServers[name] = &mcpServerJSON{Type: server.Type(), URL: server.URL(), Command: server.Command(), Args: server.Args(), Env: server.Env(), Headers: server.Headers()}
		}
	}
	return &sessionProfileJSON{
		ID:                   profile.ID(),
		Name:                 profile.Name(),
		Description:          profile.Description(),
		UserID:               profile.UserID(),
		Scope:                profile.Scope(),
		TeamID:               profile.TeamID(),
		IsDefault:            profile.IsDefault(),
		SelectorTags:         profile.SelectorTags(),
		SelectorRepositories: profile.SelectorRepositories(),
		Config: sessionProfileConfigJSON{
			Environment:            cfg.Environment(),
			Tags:                   cfg.Tags(),
			Pool:                   cfg.Pool(),
			SettingsTeamID:         cfg.SettingsTeamID(),
			InitialMessageTemplate: cfg.InitialMessageTemplate(),
			ReuseMessageTemplate:   cfg.ReuseMessageTemplate(),
			CommandWrapperTemplate: cfg.CommandWrapperTemplate(),
			SystemPrompt:           cfg.SystemPrompt(),
			SystemPromptMode:       cfg.SystemPromptMode(),
			Params:                 cfg.Params(),
			ReuseSession:           cfg.ReuseSession(),
			SandboxPolicyID:        cfg.SandboxPolicyID(),
			SessionTTL:             cfg.SessionTTL(),
			UnsyncedFilePaths:      cfg.UnsyncedFilePaths(),
			SourceSessionProfileID: cfg.SourceSessionProfileID(),
			SecretIDs:              cfg.SecretIDs(),
			GitHubRepositories:     cfg.GitHubRepositories(),
			Skills:                 cfg.Skills(),
			PiPackages:             cfg.PiPackages(),
			Files:                  cfg.ProfileFiles(),
			Repositories:           cfg.ProfileRepositories(),
			MCPServers:             mcpServers,
			SessionState:           cfg.SessionState(),
			PolicyYAML:             cfg.PolicyYAML(),
		},
		CreatedAt: profile.CreatedAt(),
		UpdatedAt: profile.UpdatedAt(),
	}
}

func (r *KubernetesSessionProfileRepository) decodeProfile(ctx context.Context, pj *sessionProfileJSON) (*entities.SessionProfile, error) {
	codex, err := r.connections.decodeConnection(ctx, pj.Config.CodexConnection)
	if err != nil {
		return nil, err
	}
	claude, err := r.connections.decodeConnection(ctx, pj.Config.ClaudeConnection)
	if err != nil {
		return nil, err
	}
	pi, err := r.connections.decodeConnection(ctx, pj.Config.PiConnection)
	if err != nil {
		return nil, err
	}
	p := r.jsonToEntity(pj)
	cfg := p.Config()
	cfg.SetCodexConnection(codex)
	cfg.SetClaudeConnection(claude)
	cfg.SetPiConnection(pi)
	p.SetConfig(cfg)
	p.SetUpdatedAt(pj.UpdatedAt)
	return p, nil
}
