package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteConfigCreatesDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	outputPath := filepath.Join(tmpDir, "deep", "nested", "dir", "merged.json")

	config := &MCPConfig{
		MCPServers: map[string]MCPServer{},
	}

	err := WriteConfig(config, outputPath)
	require.NoError(t, err)

	assert.FileExists(t, outputPath)
}

func TestMergeAndWrite(t *testing.T) {
	tmpDir := t.TempDir()
	inputDir := filepath.Join(tmpDir, "input")
	require.NoError(t, os.MkdirAll(inputDir, 0755))

	config := `{"mcpServers": {"server": {"type": "http", "url": "https://example.com"}}}`
	require.NoError(t, os.WriteFile(filepath.Join(inputDir, "config.json"), []byte(config), 0644))

	outputPath := filepath.Join(tmpDir, "output", "merged.json")

	err := MergeAndWrite([]string{inputDir}, outputPath, MergeOptions{})
	require.NoError(t, err)

	assert.FileExists(t, outputPath)

	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)

	var result MCPConfig
	require.NoError(t, json.Unmarshal(data, &result))

	assert.Len(t, result.MCPServers, 1)
	assert.Equal(t, "https://example.com", result.MCPServers["server"].URL)
}

func TestMCPServerAllFields(t *testing.T) {
	tmpDir := t.TempDir()

	config := `{
		"mcpServers": {
			"http-server": {
				"type": "http",
				"url": "https://example.com/mcp",
				"headers": {
					"Authorization": "Bearer token",
					"X-Custom": "value"
				}
			},
			"stdio-server": {
				"type": "stdio",
				"command": "npx",
				"args": ["-y", "@example/server"],
				"env": {
					"API_KEY": "secret",
					"DEBUG": "true"
				}
			},
			"sse-server": {
				"type": "sse",
				"url": "https://example.com/sse",
				"headers": {
					"Authorization": "Bearer token"
				}
			}
		}
	}`
	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, "config.json"), []byte(config), 0644))

	result, err := MergeConfigs([]string{tmpDir}, MergeOptions{})
	require.NoError(t, err)

	// Verify HTTP server
	httpServer := result.MCPServers["http-server"]
	assert.Equal(t, "http", httpServer.Type)
	assert.Equal(t, "https://example.com/mcp", httpServer.URL)
	assert.Equal(t, "Bearer token", httpServer.Headers["Authorization"])
	assert.Equal(t, "value", httpServer.Headers["X-Custom"])

	// Verify Stdio server
	stdioServer := result.MCPServers["stdio-server"]
	assert.Equal(t, "stdio", stdioServer.Type)
	assert.Equal(t, "npx", stdioServer.Command)
	assert.Equal(t, []string{"-y", "@example/server"}, stdioServer.Args)
	assert.Equal(t, "secret", stdioServer.Env["API_KEY"])
	assert.Equal(t, "true", stdioServer.Env["DEBUG"])

	// Verify SSE server
	sseServer := result.MCPServers["sse-server"]
	assert.Equal(t, "sse", sseServer.Type)
	assert.Equal(t, "https://example.com/sse", sseServer.URL)
	assert.Equal(t, "Bearer token", sseServer.Headers["Authorization"])
}

func TestMergeConfigsOverrideOrder(t *testing.T) {
	tmpDir := t.TempDir()
	dir1 := filepath.Join(tmpDir, "dir1")
	dir2 := filepath.Join(tmpDir, "dir2")
	dir3 := filepath.Join(tmpDir, "dir3")

	require.NoError(t, os.MkdirAll(dir1, 0755))
	require.NoError(t, os.MkdirAll(dir2, 0755))
	require.NoError(t, os.MkdirAll(dir3, 0755))

	// Same server name in all directories with different URLs
	config1 := `{"mcpServers": {"server": {"type": "http", "url": "https://dir1.example.com"}}}`
	config2 := `{"mcpServers": {"server": {"type": "http", "url": "https://dir2.example.com"}}}`
	config3 := `{"mcpServers": {"server": {"type": "http", "url": "https://dir3.example.com"}}}`

	require.NoError(t, os.WriteFile(filepath.Join(dir1, "config.json"), []byte(config1), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir2, "config.json"), []byte(config2), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(dir3, "config.json"), []byte(config3), 0644))

	// dir3 should win (last in list)
	result, err := MergeConfigs([]string{dir1, dir2, dir3}, MergeOptions{})
	require.NoError(t, err)

	assert.Equal(t, "https://dir3.example.com", result.MCPServers["server"].URL)
}
