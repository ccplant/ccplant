package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"gopkg.in/yaml.v2"
	sigsyaml "sigs.k8s.io/yaml"
)

// AuthConfigOverride represents auth configuration overrides from external file
type AuthConfigOverride struct {
	GitHub *GitHubAuthConfigOverride `json:"github,omitempty" yaml:"github,omitempty"`
}

// GitHubAuthConfigOverride represents GitHub auth configuration overrides
type GitHubAuthConfigOverride struct {
	UserMapping *GitHubUserMapping `json:"user_mapping,omitempty" yaml:"user_mapping,omitempty"`
}

var authConfigOverrideSections = []string{"github"}

// LoadAuthConfigFromFile loads auth configuration from an external file (e.g., ConfigMap)
func LoadAuthConfigFromFile(config *Config, filename string) error {
	return loadAuthConfigFromFile(config, filename)
}

// loadAuthConfigFromFile loads auth configuration from an external file (e.g., ConfigMap)
func loadAuthConfigFromFile(config *Config, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close auth config file: %v", err)
		}
	}()

	var authOverride AuthConfigOverride

	// Determine file format based on extension
	if strings.HasSuffix(filename, ".yaml") || strings.HasSuffix(filename, ".yml") {
		// Use yaml package directly for YAML files
		decoder := yaml.NewDecoder(file)
		if err := decoder.Decode(&authOverride); err != nil {
			return err
		}
		warnUnknownAuthConfigSections(filename)
	} else {
		// Default to JSON
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(&authOverride); err != nil {
			return err
		}
	}

	// Apply overrides to the main config
	if authOverride.GitHub != nil {
		// Initialize GitHub config if it doesn't exist
		if config.Auth.GitHub == nil {
			config.Auth.GitHub = &GitHubAuthConfig{}
		}

		// Override user mapping if provided
		if authOverride.GitHub.UserMapping != nil {
			log.Printf("[CONFIG] Applying GitHub user mapping from external config:")
			log.Printf("[CONFIG]   Default role: %s", authOverride.GitHub.UserMapping.DefaultRole)
			log.Printf("[CONFIG]   Default permissions: %v", authOverride.GitHub.UserMapping.DefaultPermissions)
			log.Printf("[CONFIG]   Team role mappings: %+v", authOverride.GitHub.UserMapping.TeamRoleMapping)

			config.Auth.GitHub.UserMapping = *authOverride.GitHub.UserMapping
			log.Printf("[CONFIG] Applied GitHub user mapping from external config")

			// Verify the configuration was applied
			log.Printf("[CONFIG] After applying - Default role: %s", config.Auth.GitHub.UserMapping.DefaultRole)
			log.Printf("[CONFIG] After applying - Default permissions: %v", config.Auth.GitHub.UserMapping.DefaultPermissions)
			log.Printf("[CONFIG] After applying - Team role mappings: %+v", config.Auth.GitHub.UserMapping.TeamRoleMapping)
		}
	} else {
		log.Printf("[CONFIG] No GitHub config found in auth override file")
	}

	return nil
}

// warnUnknownAuthConfigSections logs a warning for top-level sections the auth
// config loader does not understand, so operators never assume an unsupported
// setting took effect.
func warnUnknownAuthConfigSections(filename string) {
	rawFile, err := os.Open(filename)
	if err != nil {
		return
	}
	defer func() { _ = rawFile.Close() }()
	var raw map[interface{}]interface{}
	if err := yaml.NewDecoder(rawFile).Decode(&raw); err != nil {
		return
	}
	for key := range raw {
		name := fmt.Sprintf("%v", key)
		known := false
		for _, section := range authConfigOverrideSections {
			if name == section {
				known = true
				break
			}
		}
		if !known {
			log.Printf("[CONFIG] Warning: unknown section %q in auth config file %s is ignored (supported: %v)", name, filename, authConfigOverrideSections)
		}
	}
}

// K8sSessionConfigOverride represents kubernetes session configuration overrides from external file
type K8sSessionConfigOverride struct {
	KubernetesSession *struct {
		NodeSelector map[string]string      `json:"node_selector,omitempty" yaml:"node_selector"`
		Affinity     map[string]interface{} `json:"affinity,omitempty" yaml:"affinity"`
		Tolerations  []Toleration           `json:"tolerations,omitempty" yaml:"tolerations"`
	} `json:"kubernetes_session,omitempty" yaml:"kubernetes_session"`
}

// loadK8sSessionConfigFromFile loads kubernetes session configuration from an external file
func loadK8sSessionConfigFromFile(config *Config, filename string) error {
	file, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer func() {
		if err := file.Close(); err != nil {
			log.Printf("Failed to close kubernetes session config file: %v", err)
		}
	}()

	var k8sOverride K8sSessionConfigOverride

	// Determine file format based on extension
	if strings.HasSuffix(filename, ".yaml") || strings.HasSuffix(filename, ".yml") {
		data, err := io.ReadAll(file)
		if err != nil {
			return err
		}
		// sigs.k8s.io/yaml converts YAML through JSON, ensuring nested arbitrary
		// values (notably affinity) use string-keyed maps that json.Marshal can
		// safely clone later in runtimeconfig.Provider.
		if err := sigsyaml.Unmarshal(data, &k8sOverride); err != nil {
			return err
		}
	} else {
		decoder := json.NewDecoder(file)
		if err := decoder.Decode(&k8sOverride); err != nil {
			return err
		}
	}

	// Apply overrides to the main config
	if k8sOverride.KubernetesSession != nil {
		if k8sOverride.KubernetesSession.NodeSelector != nil {
			config.KubernetesSession.NodeSelector = k8sOverride.KubernetesSession.NodeSelector
			log.Printf("[CONFIG] Applied kubernetes session node_selector: %v", config.KubernetesSession.NodeSelector)
		}
		if k8sOverride.KubernetesSession.Affinity != nil {
			config.KubernetesSession.Affinity = k8sOverride.KubernetesSession.Affinity
			log.Printf("[CONFIG] Applied kubernetes session affinity: %v", config.KubernetesSession.Affinity)
		}
		if k8sOverride.KubernetesSession.Tolerations != nil {
			config.KubernetesSession.Tolerations = k8sOverride.KubernetesSession.Tolerations
			log.Printf("[CONFIG] Applied kubernetes session tolerations: %+v", config.KubernetesSession.Tolerations)
		}
	}

	return nil
}
