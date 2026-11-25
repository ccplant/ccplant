package analyzer

import (
	"fmt"
	"sort"
	"strings"
)

func CompareReports(baseline, current Report) ComparisonReport {
	result := ComparisonReport{
		BaselineScore: baseline.Score, CurrentScore: current.Score,
		ScoreDelta:         current.Score - baseline.Score,
		BaselineConfidence: baseline.Confidence, CurrentConfidence: current.Confidence,
		Passed: true,
	}
	if baseline.Version != "" && current.Version != "" && baseline.Version != current.Version {
		result.Regressions = append(result.Regressions, Regression{"report-version", fmt.Sprintf("baseline version %s cannot be compared with current version %s; regenerate the baseline", baseline.Version, current.Version)})
	}
	if current.Score < baseline.Score {
		result.Regressions = append(result.Regressions, Regression{"score", fmt.Sprintf("score decreased by %d", baseline.Score-current.Score)})
	}
	if baseline.Confidence-current.Confidence > 0.1 {
		result.Regressions = append(result.Regressions, Regression{"confidence", fmt.Sprintf("confidence decreased from %.3f to %.3f", baseline.Confidence, current.Confidence)})
	}
	baseViolations, currentViolations := violationSet(baseline.Violations), violationSet(current.Violations)
	for key := range currentViolations {
		if !baseViolations[key] {
			result.Regressions = append(result.Regressions, Regression{"new-violation", key})
		}
	}
	for key := range baseViolations {
		if !currentViolations[key] {
			result.Improvements = append(result.Improvements, Regression{"removed-violation", key})
		}
	}
	baseCycles, currentCycles := cycleSet(baseline.Cycles), cycleSet(current.Cycles)
	for key := range currentCycles {
		if !baseCycles[key] {
			result.Regressions = append(result.Regressions, Regression{"new-cycle", key})
		}
	}
	if len(current.Unknown) > len(baseline.Unknown) {
		result.Regressions = append(result.Regressions, Regression{"unassigned-files", fmt.Sprintf("increased from %d to %d", len(baseline.Unknown), len(current.Unknown))})
	}
	baseModules := map[string]ModuleReport{}
	for _, m := range baseline.Modules {
		baseModules[m.Name] = m
	}
	for _, m := range current.Modules {
		if b, ok := baseModules[m.Name]; ok && m.PublicSymbols > b.PublicSymbols {
			result.Regressions = append(result.Regressions, Regression{"public-api-growth", fmt.Sprintf("%s public symbols increased from %d to %d", m.Name, b.PublicSymbols, m.PublicSymbols)})
		}
	}
	if result.ScoreDelta > 0 {
		result.Improvements = append(result.Improvements, Regression{"score", fmt.Sprintf("score increased by %d", result.ScoreDelta)})
	}
	sort.Slice(result.Regressions, func(i, j int) bool {
		return result.Regressions[i].Kind+result.Regressions[i].Detail < result.Regressions[j].Kind+result.Regressions[j].Detail
	})
	sort.Slice(result.Improvements, func(i, j int) bool {
		return result.Improvements[i].Kind+result.Improvements[i].Detail < result.Improvements[j].Kind+result.Improvements[j].Detail
	})
	result.Passed = len(result.Regressions) == 0
	return result
}

func violationSet(violations []Violation) map[string]bool {
	out := map[string]bool{}
	for _, v := range violations {
		out[fmt.Sprintf("%s %s -> %s at %s:%d", v.Kind, v.FromModule, v.ToModule, v.FromFile, v.Line)] = true
	}
	return out
}
func cycleSet(cycles [][]string) map[string]bool {
	out := map[string]bool{}
	for _, cycle := range cycles {
		c := append([]string{}, cycle...)
		sort.Strings(c)
		out[strings.Join(c, " -> ")] = true
	}
	return out
}
