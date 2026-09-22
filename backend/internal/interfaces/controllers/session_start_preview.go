package controllers

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

var previewSecretKeys = map[string]struct{}{
	"api_key": {}, "token": {}, "access_token": {}, "refresh_token": {}, "password": {},
	"secret": {}, "credential": {}, "credentials": {}, "content": {}, "github_token": {},
	"webhook_payload": {}, "hmac_secret": {},
}

// redactSessionStartPreview converts a typed value to its public preview form.
// Environment and header values are always secret because their names are user-defined.
func redactSessionStartPreview(input interface{}) (interface{}, []string, error) {
	raw, err := json.Marshal(input)
	if err != nil {
		return nil, nil, err
	}
	var value interface{}
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, nil, err
	}
	redactions := make([]string, 0)
	redactPreviewNode(value, "", false, &redactions)
	sort.Strings(redactions)
	return value, redactions, nil
}

func redactPreviewNode(value interface{}, path string, redactValues bool, redactions *[]string) {
	switch node := value.(type) {
	case map[string]interface{}:
		for key, child := range node {
			childPath := path + "/" + escapeJSONPointer(key)
			keyLower := strings.ToLower(key)
			_, secretKey := previewSecretKeys[keyLower]
			containerSecrets := keyLower == "env" || keyLower == "environment" || keyLower == "headers"
			if redactValues || secretKey {
				node[key] = "<redacted>"
				*redactions = append(*redactions, childPath)
				continue
			}
			redactPreviewNode(child, childPath, containerSecrets, redactions)
		}
	case []interface{}:
		for index, child := range node {
			redactPreviewNode(child, fmt.Sprintf("%s/%d", path, index), redactValues, redactions)
		}
	}
}

func escapeJSONPointer(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "~", "~0"), "/", "~1")
}
