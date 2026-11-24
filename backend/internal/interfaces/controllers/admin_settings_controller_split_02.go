package controllers

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/kvstore"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func decodeSecretData(record kvstore.Record, key string, target interface{}) error {
	var secret corev1.Secret
	if err := json.Unmarshal(record.Value, &secret); err != nil {
		return fmt.Errorf("decode KV secret: %w", err)
	}
	if err := json.Unmarshal(secret.Data[key], target); err != nil {
		return fmt.Errorf("decode KV document: %w", err)
	}
	return nil
}

func marshalSecret(name, recordType, dataKey string, value interface{}) ([]byte, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	secret := corev1.Secret{ObjectMeta: metav1.ObjectMeta{Name: name, Labels: map[string]string{"agentapi.proxy/type": recordType}}, Type: corev1.SecretTypeOpaque, Data: map[string][]byte{dataKey: data}}
	return json.Marshal(secret)
}

func versionKey(version int64) string {
	return fmt.Sprintf("%s%010d", adminSettingsVersionPrefix, version)
}

func sanitizeAdminSettings(doc adminSettingsDocument, runtimeApplied bool) adminSettingsResponse {
	copyBytes, _ := json.Marshal(doc.Sections)
	sections := map[string]interface{}{}
	_ = json.Unmarshal(copyBytes, &sections)
	configured := map[string]bool{}
	for _, path := range adminSecretPaths {
		if value, ok := getNestedString(sections, path); ok && value != "" {
			configured[path] = true
			deleteNested(sections, path)
		}
	}
	return adminSettingsResponse{SchemaVersion: doc.SchemaVersion, Version: doc.Version, Sections: sections, SecretConfigured: configured, UpdatedAt: doc.UpdatedAt, RuntimeApplied: runtimeApplied}
}

// runtimeApplied reports whether saved settings take effect in the running
// process. The runtime config provider does not apply admin-managed settings
// today (secret-wiping defect, see runtimeconfig.Provider), so this is false
// and the admin UI must surface it (ccplant-deploy#66 F4).
func (c *AdminSettingsController) runtimeApplied() bool {
	if c.provider == nil {
		return false
	}
	return c.provider.RuntimeApplicationEnabled()
}

func preserveOmittedSecrets(existing, requested map[string]interface{}) {
	for _, path := range adminSecretPaths {
		if _, present := getNestedString(requested, path); present {
			continue
		}
		if value, ok := getNestedString(existing, path); ok {
			setNested(requested, path, value)
		}
	}
}

func getNestedString(root map[string]interface{}, path string) (string, bool) {
	parts := strings.Split(path, ".")
	var current interface{} = root
	for _, part := range parts {
		object, ok := current.(map[string]interface{})
		if !ok {
			return "", false
		}
		current, ok = object[part]
		if !ok {
			return "", false
		}
	}
	value, ok := current.(string)
	return value, ok
}

func setNested(root map[string]interface{}, path, value string) {
	parts := strings.Split(path, ".")
	current := root
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]interface{})
		if !ok {
			next = map[string]interface{}{}
			current[part] = next
		}
		current = next
	}
	current[parts[len(parts)-1]] = value
}

func deleteNested(root map[string]interface{}, path string) {
	parts := strings.Split(path, ".")
	current := root
	for _, part := range parts[:len(parts)-1] {
		next, ok := current[part].(map[string]interface{})
		if !ok {
			return
		}
		current = next
	}
	delete(current, parts[len(parts)-1])
}
