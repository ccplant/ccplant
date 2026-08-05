package kvstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	typedcorev1 "k8s.io/client-go/kubernetes/typed/core/v1"
)

var secretResource = schema.GroupResource{Resource: "secrets"}
var configMapResource = schema.GroupResource{Resource: "configmaps"}

// KubernetesAdapter routes Secret and ConfigMap KV operations through Store.
// LegacyFallback leaves pre-existing Kubernetes objects readable without
// copying them; Projection keeps new records available to Pods.
type KubernetesAdapter struct {
	kubernetes.Interface
	store          Store
	projection     bool
	legacyFallback bool
}

func NewKubernetesAdapter(base kubernetes.Interface, store Store, projection, legacyFallback bool) kubernetes.Interface {
	return &KubernetesAdapter{Interface: base, store: store, projection: projection, legacyFallback: legacyFallback}
}

func (c *KubernetesAdapter) CoreV1() typedcorev1.CoreV1Interface {
	return &coreAdapter{CoreV1Interface: c.Interface.CoreV1(), store: c.store, projection: c.projection, legacyFallback: c.legacyFallback}
}

type coreAdapter struct {
	typedcorev1.CoreV1Interface
	store          Store
	projection     bool
	legacyFallback bool
}

func (c *coreAdapter) Secrets(namespace string) typedcorev1.SecretInterface {
	return &secretAdapter{SecretInterface: c.CoreV1Interface.Secrets(namespace), store: c.store, namespace: namespace, projection: c.projection, legacyFallback: c.legacyFallback}
}

func (c *coreAdapter) ConfigMaps(namespace string) typedcorev1.ConfigMapInterface {
	return &configMapAdapter{ConfigMapInterface: c.CoreV1Interface.ConfigMaps(namespace), store: c.store, namespace: namespace, projection: c.projection, legacyFallback: c.legacyFallback}
}

type secretAdapter struct {
	typedcorev1.SecretInterface
	store          Store
	namespace      string
	projection     bool
	legacyFallback bool
}

func (a *secretAdapter) Create(ctx context.Context, object *corev1.Secret, opts metav1.CreateOptions) (*corev1.Secret, error) {
	if !isPersistentKV(KindSecret, object.Name) {
		return a.SecretInterface.Create(ctx, object, opts)
	}
	value, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	record, err := a.store.Create(ctx, Record{Kind: KindSecret, Namespace: a.namespace, Key: object.Name, Value: value})
	if err != nil {
		return nil, storageError(secretResource, object.Name, err)
	}
	result := object.DeepCopy()
	result.Namespace = a.namespace
	result.ResourceVersion = strconv.FormatInt(record.Version, 10)
	if a.projection {
		projected := object.DeepCopy()
		projected.ResourceVersion = ""
		if _, err := a.SecretInterface.Create(ctx, projected, opts); err != nil {
			_ = a.store.Delete(ctx, KindSecret, a.namespace, object.Name)
			return nil, fmt.Errorf("project Secret: %w", err)
		}
	}
	return result, nil
}

func (a *secretAdapter) Update(ctx context.Context, object *corev1.Secret, opts metav1.UpdateOptions) (*corev1.Secret, error) {
	if !isPersistentKV(KindSecret, object.Name) {
		return a.SecretInterface.Update(ctx, object, opts)
	}
	version, _ := strconv.ParseInt(object.ResourceVersion, 10, 64)
	value, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	record, err := a.store.Update(ctx, Record{Kind: KindSecret, Namespace: a.namespace, Key: object.Name, Value: value, Version: version})
	if errors.Is(err, ErrConflict) && a.legacyFallback {
		return a.SecretInterface.Update(ctx, object, opts)
	}
	if err != nil {
		return nil, storageError(secretResource, object.Name, err)
	}
	result := object.DeepCopy()
	result.ResourceVersion = strconv.FormatInt(record.Version, 10)
	if a.projection {
		if err := updateSecretProjection(ctx, a.SecretInterface, object, opts); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (a *secretAdapter) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Secret, error) {
	if !isPersistentKV(KindSecret, name) {
		return a.SecretInterface.Get(ctx, name, opts)
	}
	record, err := a.store.Get(ctx, KindSecret, a.namespace, name)
	if errors.Is(err, ErrNotFound) && a.legacyFallback {
		return a.SecretInterface.Get(ctx, name, opts)
	}
	if err != nil {
		return nil, storageError(secretResource, name, err)
	}
	var object corev1.Secret
	if err := json.Unmarshal(record.Value, &object); err != nil {
		return nil, err
	}
	object.Namespace = a.namespace
	object.ResourceVersion = strconv.FormatInt(record.Version, 10)
	return &object, nil
}

func (a *secretAdapter) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	if !isPersistentKV(KindSecret, name) {
		return a.SecretInterface.Delete(ctx, name, opts)
	}
	err := a.store.Delete(ctx, KindSecret, a.namespace, name)
	if errors.Is(err, ErrNotFound) && a.legacyFallback {
		return a.SecretInterface.Delete(ctx, name, opts)
	}
	if err != nil {
		return storageError(secretResource, name, err)
	}
	if a.projection {
		return a.SecretInterface.Delete(ctx, name, opts)
	}
	return nil
}

func (a *secretAdapter) List(ctx context.Context, opts metav1.ListOptions) (*corev1.SecretList, error) {
	records, err := a.store.List(ctx, Query{Kind: KindSecret, Namespace: a.namespace})
	if err != nil {
		return nil, err
	}
	selector, err := labels.Parse(opts.LabelSelector)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	result := &corev1.SecretList{}
	seen := map[string]bool{}
	for _, record := range records {
		var object corev1.Secret
		if err := json.Unmarshal(record.Value, &object); err != nil {
			return nil, err
		}
		object.ResourceVersion = strconv.FormatInt(record.Version, 10)
		if selector.Matches(labels.Set(object.Labels)) {
			result.Items = append(result.Items, object)
			seen[object.Name] = true
		}
	}
	if a.legacyFallback {
		legacy, err := a.SecretInterface.List(ctx, opts)
		if err != nil {
			return nil, err
		}
		for _, object := range legacy.Items {
			if !seen[object.Name] {
				result.Items = append(result.Items, object)
			}
		}
	}
	return result, nil
}

type configMapAdapter struct {
	typedcorev1.ConfigMapInterface
	store                      Store
	namespace                  string
	projection, legacyFallback bool
}

func (a *configMapAdapter) Create(ctx context.Context, object *corev1.ConfigMap, opts metav1.CreateOptions) (*corev1.ConfigMap, error) {
	if !isPersistentKV(KindConfigMap, object.Name) {
		return a.ConfigMapInterface.Create(ctx, object, opts)
	}
	value, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	record, err := a.store.Create(ctx, Record{Kind: KindConfigMap, Namespace: a.namespace, Key: object.Name, Value: value})
	if err != nil {
		return nil, storageError(configMapResource, object.Name, err)
	}
	result := object.DeepCopy()
	result.Namespace = a.namespace
	result.ResourceVersion = strconv.FormatInt(record.Version, 10)
	if a.projection {
		projected := object.DeepCopy()
		projected.ResourceVersion = ""
		if _, err := a.ConfigMapInterface.Create(ctx, projected, opts); err != nil {
			_ = a.store.Delete(ctx, KindConfigMap, a.namespace, object.Name)
			return nil, fmt.Errorf("project ConfigMap: %w", err)
		}
	}
	return result, nil
}

func (a *configMapAdapter) Update(ctx context.Context, object *corev1.ConfigMap, opts metav1.UpdateOptions) (*corev1.ConfigMap, error) {
	if !isPersistentKV(KindConfigMap, object.Name) {
		return a.ConfigMapInterface.Update(ctx, object, opts)
	}
	version, _ := strconv.ParseInt(object.ResourceVersion, 10, 64)
	value, err := json.Marshal(object)
	if err != nil {
		return nil, err
	}
	record, err := a.store.Update(ctx, Record{Kind: KindConfigMap, Namespace: a.namespace, Key: object.Name, Value: value, Version: version})
	if errors.Is(err, ErrConflict) && a.legacyFallback {
		return a.ConfigMapInterface.Update(ctx, object, opts)
	}
	if err != nil {
		return nil, storageError(configMapResource, object.Name, err)
	}
	result := object.DeepCopy()
	result.ResourceVersion = strconv.FormatInt(record.Version, 10)
	if a.projection {
		projected, err := a.ConfigMapInterface.Get(ctx, object.Name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}
		projected.Data = object.Data
		projected.BinaryData = object.BinaryData
		projected.Labels = object.Labels
		projected.Annotations = object.Annotations
		if _, err := a.ConfigMapInterface.Update(ctx, projected, opts); err != nil {
			return nil, fmt.Errorf("update ConfigMap projection: %w", err)
		}
	}
	return result, nil
}

func (a *configMapAdapter) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.ConfigMap, error) {
	if !isPersistentKV(KindConfigMap, name) {
		return a.ConfigMapInterface.Get(ctx, name, opts)
	}
	record, err := a.store.Get(ctx, KindConfigMap, a.namespace, name)
	if errors.Is(err, ErrNotFound) && a.legacyFallback {
		return a.ConfigMapInterface.Get(ctx, name, opts)
	}
	if err != nil {
		return nil, storageError(configMapResource, name, err)
	}
	var object corev1.ConfigMap
	if err := json.Unmarshal(record.Value, &object); err != nil {
		return nil, err
	}
	object.Namespace = a.namespace
	object.ResourceVersion = strconv.FormatInt(record.Version, 10)
	return &object, nil
}

func (a *configMapAdapter) Delete(ctx context.Context, name string, opts metav1.DeleteOptions) error {
	if !isPersistentKV(KindConfigMap, name) {
		return a.ConfigMapInterface.Delete(ctx, name, opts)
	}
	err := a.store.Delete(ctx, KindConfigMap, a.namespace, name)
	if errors.Is(err, ErrNotFound) && a.legacyFallback {
		return a.ConfigMapInterface.Delete(ctx, name, opts)
	}
	if err != nil {
		return storageError(configMapResource, name, err)
	}
	if a.projection {
		return a.ConfigMapInterface.Delete(ctx, name, opts)
	}
	return nil
}

func (a *configMapAdapter) List(ctx context.Context, opts metav1.ListOptions) (*corev1.ConfigMapList, error) {
	records, err := a.store.List(ctx, Query{Kind: KindConfigMap, Namespace: a.namespace})
	if err != nil {
		return nil, err
	}
	selector, err := labels.Parse(opts.LabelSelector)
	if err != nil {
		return nil, apierrors.NewBadRequest(err.Error())
	}
	result := &corev1.ConfigMapList{}
	seen := map[string]bool{}
	for _, record := range records {
		var object corev1.ConfigMap
		if err := json.Unmarshal(record.Value, &object); err != nil {
			return nil, err
		}
		object.ResourceVersion = strconv.FormatInt(record.Version, 10)
		if selector.Matches(labels.Set(object.Labels)) {
			result.Items = append(result.Items, object)
			seen[object.Name] = true
		}
	}
	if a.legacyFallback {
		legacy, err := a.ConfigMapInterface.List(ctx, opts)
		if err != nil {
			return nil, err
		}
		for _, object := range legacy.Items {
			if !seen[object.Name] {
				result.Items = append(result.Items, object)
			}
		}
	}
	return result, nil
}

func updateSecretProjection(ctx context.Context, client typedcorev1.SecretInterface, desired *corev1.Secret, opts metav1.UpdateOptions) error {
	projected, err := client.Get(ctx, desired.Name, metav1.GetOptions{})
	if err != nil {
		return err
	}
	projected.Data = desired.Data
	projected.StringData = desired.StringData
	projected.Labels = desired.Labels
	projected.Annotations = desired.Annotations
	projected.Type = desired.Type
	if _, err := client.Update(ctx, projected, opts); err != nil {
		return fmt.Errorf("update Secret projection: %w", err)
	}
	return nil
}

func storageError(resource schema.GroupResource, name string, err error) error {
	switch {
	case errors.Is(err, ErrNotFound):
		return apierrors.NewNotFound(resource, name)
	case errors.Is(err, ErrConflict):
		return apierrors.NewConflict(resource, name, err)
	default:
		return err
	}
}

// isPersistentKV separates application documents from operational Kubernetes
// resources. New document families must be registered here and covered by the
// adapter tests; arbitrary Pod-mounted Secrets are never redirected.
func isPersistentKV(kind Kind, name string) bool {
	prefixes := map[Kind][]string{
		KindSecret: {
			"agentapi-api-token-", "agentapi-agent-files-", "agentapi-personal-api-key-",
			"agentapi-session-profile-", "agentapi-session-route-", "agentapi-settings-",
			"agentapi-slackbot-", "agentapi-team-config-", "agentapi-user-files-",
			"agentapi-webhook-", "agentapi-schedule-", "agentapi-session-allocation-",
			"agentapi-provision-request-", "agentapi-notification-",
		},
		KindConfigMap: {
			"agentapi-memory-", "agentapi-oauth-state-", "agentapi-sandbox-domains-",
			"agentapi-sandbox-policy-", "agentapi-task-group-", "agentapi-task-",
		},
	}
	exact := map[Kind]map[string]bool{
		KindSecret:    {"agentapi-schedules": true},
		KindConfigMap: {"agentapi-session-shares": true, "agentapi-user-team-mapping": true},
	}
	if exact[kind][name] {
		return true
	}
	for _, prefix := range prefixes[kind] {
		if strings.HasPrefix(name, prefix) {
			return true
		}
	}
	return false
}
