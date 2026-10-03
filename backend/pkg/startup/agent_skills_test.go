package startup

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveSkillsAgent(t *testing.T) {
	tests := map[string]string{
		"claude-acp": "claude-code", "claude-legacy": "claude-code",
		"codex-acp": "codex", "pi-ollama": "", "cursor": "",
	}
	for agentType, want := range tests {
		got, err := resolveSkillsAgent(agentType)
		if err != nil || got != want {
			t.Fatalf("resolveSkillsAgent(%q) = %q, %v; want %q", agentType, got, err, want)
		}
	}
	if _, err := resolveSkillsAgent("unknown-agent"); err == nil {
		t.Fatal("expected unsupported agent type error")
	}
}

func TestInstallSkillsPackagesSelectsOneAgent(t *testing.T) {
	dir := t.TempDir()
	argsPath := filepath.Join(dir, "args")
	homePath := filepath.Join(dir, "home")
	binPath := filepath.Join(dir, "skills")
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > \"$SKILLS_ARGS_PATH\"\nprintf '%s' \"$HOME\" > \"$SKILLS_HOME_PATH\"\n"
	if err := os.WriteFile(binPath, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	original := skillsBinPath
	skillsBinPath = binPath
	t.Cleanup(func() { skillsBinPath = original })
	t.Setenv("SKILLS_ARGS_PATH", argsPath)
	t.Setenv("SKILLS_HOME_PATH", homePath)

	if err := installSkillsPackages(dir, "codex", []string{"owner/repository"}); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(argsPath)
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Split(strings.TrimSpace(string(raw)), "\n")
	want := []string{"add", "owner/repository", "--agent", "codex", "--skill", "*", "--global", "--copy", "--yes"}
	if strings.Join(args, "|") != strings.Join(want, "|") {
		t.Fatalf("args = %#v, want %#v", args, want)
	}
	home, err := os.ReadFile(homePath)
	if err != nil || string(home) != dir {
		t.Fatalf("HOME = %q, err=%v", string(home), err)
	}
}

func TestCopySkillDirRecursivelyCopiesResources(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source")
	destination := filepath.Join(t.TempDir(), "destination")
	if err := os.MkdirAll(filepath.Join(source, "references"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "SKILL.md"), []byte("---\nname: demo\n---\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(source, "references", "guide.md"), []byte("guide"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := copySkillDir(source, destination); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(destination, "references", "guide.md"))
	if err != nil || string(got) != "guide" {
		t.Fatalf("nested resource = %q, err=%v", string(got), err)
	}
}

func TestSyncCodexSkillsCopiesOnlyEnabledPlugins(t *testing.T) {
	dir := t.TempDir()
	marketplacesDir := filepath.Join(dir, "marketplaces")
	for _, plugin := range []string{"enabled", "disabled"} {
		skillDir := filepath.Join(marketplacesDir, "example", "plugins", plugin, "skills", plugin+"-skill")
		if err := os.MkdirAll(skillDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(plugin), 0644); err != nil {
			t.Fatal(err)
		}
	}

	if err := syncCodexSkills(dir, marketplacesDir, []string{"enabled@example"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".codex", "skills", "enabled-skill", "SKILL.md")); err != nil {
		t.Fatalf("enabled skill was not copied: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".codex", "skills", "disabled-skill")); !os.IsNotExist(err) {
		t.Fatalf("disabled skill should not be copied, err=%v", err)
	}
}
