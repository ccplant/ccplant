package sessionsettings

import (
	"math"
	"reflect"
)

// Normalize the JSON-shaped hook data, including Go-created []map slices.
func normalizedCodexHooks(root map[string]interface{}) map[string]interface{} {
	result := make(map[string]interface{}, len(root))
	for key, value := range root {
		result[key] = cloneCodexHookValue(value)
	}
	return result
}

func cloneCodexHookValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		return normalizedCodexHooks(v)
	case []interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = cloneCodexHookValue(item)
		}
		return result
	case []map[string]interface{}:
		result := make([]interface{}, len(v))
		for i, item := range v {
			result[i] = normalizedCodexHooks(item)
		}
		return result
	case float64:
		// JSON decodes numbers as float64, but Codex requires integer TOML for
		// timeout and other integer hook fields. Preserve that native type.
		if v == math.Trunc(v) && v >= math.MinInt64 && v < math.MaxInt64 {
			return int64(v)
		}
	}
	return value
}

func appendUniqueCodexHookEntries(existing, added []interface{}) []interface{} {
	result := append([]interface{}(nil), existing...)
	for _, entry := range added {
		found := false
		for _, previous := range result {
			if reflect.DeepEqual(previous, entry) {
				found = true
				break
			}
		}
		if !found {
			result = append(result, entry)
		}
	}
	return result
}

// MergeCodexHooks combines trusted hook sources without mutating either input.
func MergeCodexHooks(base, added map[string]interface{}) map[string]interface{} {
	result := normalizedCodexHooks(base)
	hooks, _ := result["hooks"].(map[string]interface{})
	if hooks == nil {
		hooks = map[string]interface{}{}
	}
	addHooks, _ := RemoveManagedCodexHooks(added, base)["hooks"].(map[string]interface{})
	for event, raw := range addHooks {
		entries, ok := raw.([]interface{})
		if !ok {
			if _, exists := hooks[event]; !exists {
				hooks[event] = raw
			}
			continue
		}
		existing, _ := hooks[event].([]interface{})
		hooks[event] = appendUniqueCodexHookEntries(existing, entries)
	}
	result["hooks"] = hooks
	return result
}

// RemoveManagedCodexHooks removes only identical managed hooks. Event and group
// metadata (matcher, timeout, etc.) must also match. Other user hooks remain in
// the user layer with their normal approval requirements.
func RemoveManagedCodexHooks(user, managed map[string]interface{}) map[string]interface{} {
	result := normalizedCodexHooks(user)
	userEvents, _ := result["hooks"].(map[string]interface{})
	managedEvents, _ := normalizedCodexHooks(managed)["hooks"].(map[string]interface{})
	for event, raw := range userEvents {
		managedEntries, _ := managedEvents[event].([]interface{})
		var identities []struct {
			group map[string]interface{}
			hook  interface{}
		}
		for _, rawEntry := range managedEntries {
			entry, ok := rawEntry.(map[string]interface{})
			if !ok {
				continue
			}
			hooks, _ := entry["hooks"].([]interface{})
			delete(entry, "hooks")
			for _, hook := range hooks {
				identities = append(identities, struct {
					group map[string]interface{}
					hook  interface{}
				}{entry, hook})
			}
		}
		entries, ok := raw.([]interface{})
		if !ok {
			continue
		}
		keptEntries := []interface{}{}
		for _, rawEntry := range entries {
			entry, ok := rawEntry.(map[string]interface{})
			if !ok {
				keptEntries = append(keptEntries, rawEntry)
				continue
			}
			hooks, ok := entry["hooks"].([]interface{})
			if !ok {
				keptEntries = append(keptEntries, entry)
				continue
			}
			delete(entry, "hooks")
			kept := []interface{}{}
			for _, hook := range hooks {
				managed := false
				for _, identity := range identities {
					if reflect.DeepEqual(identity.group, entry) && reflect.DeepEqual(identity.hook, hook) {
						managed = true
						break
					}
				}
				if !managed {
					kept = append(kept, hook)
				}
			}
			if len(kept) > 0 || len(hooks) == 0 {
				entry["hooks"] = kept
				keptEntries = append(keptEntries, entry)
			}
		}
		if len(keptEntries) == 0 {
			delete(userEvents, event)
		} else {
			userEvents[event] = keptEntries
		}
	}
	return result
}
