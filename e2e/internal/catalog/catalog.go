package catalog

import (
	"errors"
	"fmt"
	"os"
	"regexp"
	"time"

	"github.com/ccplant/ccplant/e2e/internal/domain"
	"gopkg.in/yaml.v3"
)

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9]*(?:[.-][a-z0-9]+)*$`)

func Load(path string) (*domain.Catalog, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var c domain.Catalog
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	if err := Validate(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func Validate(c *domain.Catalog) error {
	if len(c.Categories) == 0 || len(c.Scenarios) == 0 {
		return errors.New("catalog must contain categories and scenarios")
	}
	categories := map[string]bool{}
	for _, v := range c.Categories {
		if !idPattern.MatchString(v.ID) || v.Description == "" {
			return fmt.Errorf("invalid category %q", v.ID)
		}
		if categories[v.ID] {
			return fmt.Errorf("duplicate category %q", v.ID)
		}
		categories[v.ID] = true
	}
	ids := map[string]bool{}
	for _, s := range c.Scenarios {
		if !idPattern.MatchString(s.ID) || s.Description == "" {
			return fmt.Errorf("invalid scenario %q", s.ID)
		}
		if ids[s.ID] {
			return fmt.Errorf("duplicate scenario %q", s.ID)
		}
		ids[s.ID] = true
		if !categories[s.Category] {
			return fmt.Errorf("scenario %q has unknown category", s.ID)
		}
		if _, err := time.ParseDuration(s.Timeout); err != nil {
			return fmt.Errorf("scenario %q: invalid timeout", s.ID)
		}
		switch s.Kind {
		case "go-test":
			if s.Package == "" || s.Test == "" {
				return fmt.Errorf("scenario %q requires package and test", s.ID)
			}
		case "playwright":
			if s.Tag == "" {
				return fmt.Errorf("scenario %q requires tag", s.ID)
			}
		default:
			return fmt.Errorf("scenario %q has unsupported kind %q", s.ID, s.Kind)
		}
	}
	return nil
}

func Find(c *domain.Catalog, id string) (domain.Scenario, domain.Category, bool) {
	var scenario domain.Scenario
	found := false
	for _, s := range c.Scenarios {
		if s.ID == id {
			scenario, found = s, true
			break
		}
	}
	if !found {
		return domain.Scenario{}, domain.Category{}, false
	}
	for _, category := range c.Categories {
		if category.ID == scenario.Category {
			return scenario, category, true
		}
	}
	return domain.Scenario{}, domain.Category{}, false
}
