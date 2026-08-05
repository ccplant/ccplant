package kvstore

import (
	"context"
	"encoding/json"
	"errors"
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
type KubernetesAdapter struct {
	kubernetes.Interface
	store Store
}

func NewKubernetesAdapter(base kubernetes.Interface, store Store) kubernetes.Interface {
	return &KubernetesAdapter{Interface: base, store: store}
}

func (c *KubernetesAdapter) CoreV1() typedcorev1.CoreV1Interface {
	return &coreAdapter{CoreV1Interface: c.Interface.CoreV1(), store: c.store}
}

type coreAdapter struct {
	typedcorev1.CoreV1Interface
	store Store
}

func (c *coreAdapter) Secrets(namespace string) typedcorev1.SecretInterface {
	return &secretAdapter{SecretInterface: c.CoreV1Interface.Secrets(namespace), store: c.store, namespace: namespace}
}

func (c *coreAdapter) ConfigMaps(namespace string) typedcorev1.ConfigMapInterface {
	return &configMapAdapter{ConfigMapInterface: c.CoreV1Interface.ConfigMaps(namespace), store: c.store, namespace: namespace}
}

type secretAdapter struct {
	typedcorev1.SecretInterface
	store     Store
	namespace string
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
	if err != nil {
		return nil, storageError(secretResource, object.Name, err)
	}
	result := object.DeepCopy()
	result.ResourceVersion = strconv.FormatInt(record.Version, 10)
	return result, nil
}

func (a *secretAdapter) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.Secret, error) {
	if !isPersistentKV(KindSecret, name) {
		return a.SecretInterface.Get(ctx, name, opts)
	}
	record, err := a.store.Get(ctx, KindSecret, a.namespace, name)
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
	if err != nil {
		return storageError(secretResource, name, err)
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
	for _, record := range records {
		var object corev1.Secret
		if err := json.Unmarshal(record.Value, &object); err != nil {
			return nil, err
		}
		object.ResourceVersion = strconv.FormatInt(record.Version, 10)
		if selector.Matches(labels.Set(object.Labels)) {
			result.Items = append(result.Items, object)
		}
	}
	return result, nil
}

type configMapAdapter struct {
	typedcorev1.ConfigMapInterface
	store     Store
	namespace string
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
	if err != nil {
		return nil, storageError(configMapResource, object.Name, err)
	}
	result := object.DeepCopy()
	result.ResourceVersion = strconv.FormatInt(record.Version, 10)
	return result, nil
}

func (a *configMapAdapter) Get(ctx context.Context, name string, opts metav1.GetOptions) (*corev1.ConfigMap, error) {
	if !isPersistentKV(KindConfigMap, name) {
		return a.ConfigMapInterface.Get(ctx, name, opts)
	}
	record, err := a.store.Get(ctx, KindConfigMap, a.namespace, name)
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
	if err != nil {
		return storageError(configMapResource, name, err)
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
	for _, record := range records {
		var object corev1.ConfigMap
		if err := json.Unmarshal(record.Value, &object); err != nil {
			return nil, err
		}
		object.ResourceVersion = strconv.FormatInt(record.Version, 10)
		if selector.Matches(labels.Set(object.Labels)) {
			result.Items = append(result.Items, object)
		}
	}
	return result, nil
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
