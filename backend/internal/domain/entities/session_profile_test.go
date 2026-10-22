package entities

import "testing"

func TestSessionProfileMatchesSelectorTags(t *testing.T) {
	profile := NewSessionProfile("profile-1", "selected", "user-1")

	if profile.MatchesSelectorTags(map[string]string{"env": "dev"}) {
		t.Fatal("empty selector should not match")
	}

	profile.SetSelectorTags(map[string]string{"env": "dev", "repo": "owner/repo"})
	if !profile.MatchesSelectorTags(map[string]string{
		"env":   "dev",
		"repo":  "owner/repo",
		"extra": "kept",
	}) {
		t.Fatal("expected all selector tags to match")
	}
	if profile.MatchesSelectorTags(map[string]string{"env": "dev"}) {
		t.Fatal("missing selector tag should not match")
	}
	if profile.MatchesSelectorTags(map[string]string{"env": "prod", "repo": "owner/repo"}) {
		t.Fatal("different selector value should not match")
	}
}

func TestSessionProfileSelectorTagsReturnsCopy(t *testing.T) {
	profile := NewSessionProfile("profile-1", "selected", "user-1")
	profile.SetSelectorTags(map[string]string{"env": "dev"})

	tags := profile.SelectorTags()
	tags["env"] = "prod"

	if profile.SelectorTags()["env"] != "dev" {
		t.Fatal("SelectorTags should return a copy")
	}
}

func TestSessionProfileMatchesAnySelectorRepository(t *testing.T) {
	profile := NewSessionProfile("profile-1", "selected", "user-1")
	profile.SetSelectorRepositories([]string{"owner/api", "owner/web"})

	if !profile.MatchesSelectorTags(map[string]string{"repository": "owner/web"}) {
		t.Fatal("expected any configured repository to match")
	}
	if profile.MatchesSelectorTags(map[string]string{"repository": "owner/other"}) {
		t.Fatal("unexpected repository match")
	}

	repositories := profile.SelectorRepositories()
	repositories[0] = "changed/repo"
	if profile.SelectorRepositories()[0] != "owner/api" {
		t.Fatal("SelectorRepositories should return a copy")
	}
}

func TestSessionProfileSelectorRepositoriesCombineWithTags(t *testing.T) {
	profile := NewSessionProfile("profile-1", "selected", "user-1")
	profile.SetSelectorRepositories([]string{"owner/api", "owner/web"})
	profile.SetSelectorTags(map[string]string{"env": "dev"})

	if !profile.MatchesSelectorTags(map[string]string{"repository": "owner/api", "env": "dev"}) {
		t.Fatal("expected repository and tag selector to match")
	}
	if profile.MatchesSelectorTags(map[string]string{"repository": "owner/api", "env": "prod"}) {
		t.Fatal("repository selector must not bypass other tags")
	}
}
