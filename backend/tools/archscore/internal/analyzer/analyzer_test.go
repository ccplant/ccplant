package analyzer

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAnalyzeGoAndTypeScript(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/app\n")
	write(t, root, "domain/order.go", "package domain\nimport \"example.test/app/infra\"\n")
	write(t, root, "infra/db.go", "package infra\nimport \"example.test/app/domain\"\n")
	write(t, root, "ui/view.ts", "import { run } from '../app/run';\nexport { db } from '../infra/db';\n")
	write(t, root, "app/run.ts", "import '../domain/model';\nexport const run = 1;\n")
	write(t, root, "domain/model.ts", "export const model = 1;\n")
	write(t, root, "infra/db.ts", "export const db = 1;\n")
	cfg := Config{MaxFanOut: 3, Modules: []ModuleConfig{
		{Name: "domain", Paths: []string{"domain/**"}},
		{Name: "app", Paths: []string{"app/**"}, MayDependOn: []string{"domain"}},
		{Name: "infra", Paths: []string{"infra/**"}, MayDependOn: []string{"domain"}},
		{Name: "ui", Paths: []string{"ui/**"}, MayDependOn: []string{"app"}},
	}}
	r, err := Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if r.Files != 6 {
		t.Fatalf("files=%d", r.Files)
	}
	if len(r.Violations) != 2 {
		t.Fatalf("violations=%d: %#v", len(r.Violations), r.Violations)
	}
	if len(r.Cycles) != 1 {
		t.Fatalf("cycles=%d: %#v", len(r.Cycles), r.Cycles)
	}
	if r.Gate != "fail" {
		t.Fatalf("gate=%s", r.Gate)
	}
}

func TestHistoryFromRepositorySubdirectory(t *testing.T) {
	repo := t.TempDir()
	root := filepath.Join(repo, "backend")
	write(t, root, "go.mod", "module example.test/app\n")
	write(t, root, "a/a.go", "package a\n")
	write(t, root, "b/b.go", "package b\n")
	runGit(t, repo, "init")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "both")
	write(t, root, "a/a.go", "package a\n// changed\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "a only")

	cfg := Config{HistoryCommits: 10, MinModuleChanges: 1, MinCochange: 1, Modules: []ModuleConfig{
		{Name: "a", Paths: []string{"a/**"}},
		{Name: "b", Paths: []string{"b/**"}},
	}}
	r, err := Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Modules[0].ChangePartners) != 1 || r.Modules[0].ChangePartners[0] != "b" {
		t.Fatalf("subdirectory history was not matched: %#v", r.Modules)
	}
	if r.Modules[0].ChangeLocality != .5 || r.Modules[0].ContainedChanges != 1 {
		t.Fatalf("unexpected locality: %#v", r.Modules[0])
	}
	last := r.Components[len(r.Components)-1]
	if last.Name != "change-locality" || last.Detail != "evaluated 2/2 modules and 3 changes; 0 changes meet the 80% locality target" {
		t.Fatalf("unexpected history component: %#v", last)
	}
}

func TestChangeLocalityWeightsModulesByEvidence(t *testing.T) {
	report := Report{Modules: []ModuleReport{
		{Name: "well-supported"},
		{Name: "barely-supported"},
	}}
	root := t.TempDir()
	files := map[string]*sourceFile{
		"good/a.go": {Path: "good/a.go", Module: "well-supported"},
		"bad/b.go":  {Path: "bad/b.go", Module: "barely-supported"},
	}
	runGit(t, root, "init")
	for i := 0; i < 10; i++ {
		write(t, root, "good/a.go", fmt.Sprintf("package good\n// %d\n", i))
		runGit(t, root, "add", ".")
		runGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", fmt.Sprintf("good %d", i))
	}
	for i := 0; i < 2; i++ {
		write(t, root, "good/a.go", fmt.Sprintf("package good\n// shared %d\n", i))
		write(t, root, "bad/b.go", fmt.Sprintf("package bad\n// shared %d\n", i))
		runGit(t, root, "add", ".")
		runGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", fmt.Sprintf("shared %d", i))
	}
	applyHistory(root, files, Config{HistoryCommits: 20, MinModuleChanges: 1, MinCochange: 1, MinLocalityRatio: .8}, &report)
	component := report.Components[0]
	if component.Score != 4 {
		t.Fatalf("well-supported history should dominate sparse evidence: %#v", component)
	}
}

func TestDiscoverGroupsDependentPackagesDeterministically(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/app\n")
	write(t, root, "orders/order.go", "package orders\nimport \"example.test/app/payments\"\n")
	write(t, root, "payments/payment.go", "package payments\n")
	write(t, root, "unrelated/audit.go", "package unrelated\n")
	cfg := DiscoverConfig{MinClusters: 2, MaxClusters: 2, HistoryCommits: 10, MinCochange: 2}
	first, err := Discover(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Discover(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Candidates) != 1 || len(second.Candidates) != 1 {
		t.Fatalf("candidates: %#v", first.Candidates)
	}
	if fmt.Sprint(first.Candidates) != fmt.Sprint(second.Candidates) {
		t.Fatal("discovery is not deterministic")
	}
	found := false
	for _, boundary := range first.Candidates[0].Boundaries {
		if strings.Join(boundary.Units, ",") == "orders,payments" {
			found = true
			if strings.Contains(strings.Join(boundary.Paths, ","), "orders/**") || !contains(boundary.Paths, "orders/*.go") {
				t.Fatalf("unsafe generated paths: %#v", boundary.Paths)
			}
			if boundary.Evidence.InternalDependencyEdges != 1 {
				t.Fatalf("evidence: %#v", boundary.Evidence)
			}
		}
	}
	if !found {
		t.Fatalf("dependent packages were not grouped: %#v", first.Candidates[0])
	}
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestCompositionRootIsExemptFromFanOutPenalty(t *testing.T) {
	modules := []ModuleConfig{{Name: "root", Role: "composition-root"}}
	files := map[string]*sourceFile{"root/main.go": {Path: "root/main.go", Module: "root"}}
	var edges []edge
	for i := 0; i < 6; i++ {
		name := fmt.Sprintf("m%d", i)
		modules = append(modules, ModuleConfig{Name: name})
		files[name+"/x.go"] = &sourceFile{Path: name + "/x.go", Module: name}
		edges = append(edges, edge{FromFile: "root/main.go", ToFile: name + "/x.go", FromModule: "root", ToModule: name})
	}
	report := buildReport(files, edges, Config{Modules: modules, MaxFanOut: 2})
	if report.Components[3].Score != 15 {
		t.Fatalf("composition root was penalized: %#v", report.Components[3])
	}
	modules[0].Role = ""
	report = buildReport(files, edges, Config{Modules: modules, MaxFanOut: 2})
	if report.Components[3].Score >= 15 {
		t.Fatalf("ordinary module was not penalized: %#v", report.Components[3])
	}
}

func TestCompositionRootWithInboundDependenciesWarns(t *testing.T) {
	modules := []ModuleConfig{{Name: "root", Role: "composition-root"}, {Name: "feature"}}
	files := map[string]*sourceFile{
		"root/main.go":       {Path: "root/main.go", Module: "root"},
		"feature/feature.go": {Path: "feature/feature.go", Module: "feature"},
	}
	edges := []edge{{FromFile: "feature/feature.go", ToFile: "root/main.go", FromModule: "feature", ToModule: "root"}}
	report := buildReport(files, edges, Config{Modules: modules})
	if len(report.Modules[0].Warnings) != 1 {
		t.Fatalf("expected role-mixing warning: %#v", report.Modules[0])
	}
}

func TestTestsAreExcludedUnlessRequested(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/app\n")
	write(t, root, "a/a.go", "package a\n")
	write(t, root, "a/a_test.go", "package a\nimport _ \"example.test/app/b\"\n")
	write(t, root, "b/b.go", "package b\n")
	cfg := Config{Modules: []ModuleConfig{{Name: "a", Paths: []string{"a/**"}}, {Name: "b", Paths: []string{"b/**"}}}}
	report, err := Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if report.Files != 2 || report.Edges != 0 {
		t.Fatalf("tests affected production score: %#v", report)
	}
	cfg.IncludeTests = true
	report, err = Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if report.Files != 3 || report.Edges != 1 {
		t.Fatalf("tests were not included on request: %#v", report)
	}
}

func TestGeneratedCodeRemainsStaticButIsExcludedFromHistory(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/app\n")
	write(t, root, "a/generated.go", "// Code generated by test; DO NOT EDIT.\npackage a\nimport _ \"example.test/app/b\"\n")
	write(t, root, "b/b.go", "package b\n")
	runGit(t, root, "init")
	runGit(t, root, "add", ".")
	runGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "initial")
	write(t, root, "a/generated.go", "// Code generated by test; DO NOT EDIT.\npackage a\nimport _ \"example.test/app/b\"\n// regenerated\n")
	runGit(t, root, "add", ".")
	runGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.com", "commit", "-m", "regenerate")
	cfg := Config{HistoryCommits: 10, MinModuleChanges: 1, Modules: []ModuleConfig{
		{Name: "a", Paths: []string{"a/**"}}, {Name: "b", Paths: []string{"b/**"}},
	}}
	report, err := Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if report.Edges != 1 || report.GeneratedFiles != 1 || report.HistoryGeneratedExcluded != 1 {
		t.Fatalf("generated static analysis was lost: %#v", report)
	}
	if report.Modules[0].ChangeCommits != 0 {
		t.Fatalf("generated churn affected history: %#v", report.Modules[0])
	}
	cfg.IncludeGeneratedHistory = true
	report, err = Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if report.Modules[0].ChangeCommits != 2 || report.HistoryGeneratedExcluded != 0 {
		t.Fatalf("generated history opt-in failed: %#v", report)
	}
}

func TestDiscoverAutoScalesClusterCapacity(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/app\n")
	for i := 0; i < 5; i++ {
		body := "package p\n"
		if i < 4 {
			body += fmt.Sprintf("import _ \"example.test/app/p%d\"\n", i+1)
		}
		write(t, root, fmt.Sprintf("p%d/p.go", i), body)
	}
	report, err := Discover(root, DiscoverConfig{MinClusters: 1, MaxClusters: 2, MaxBoundarySize: 2, MaxBoundaryFiles: 10})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, candidate := range report.Candidates {
		found = found || candidate.ClusterCount == 3
	}
	if !found || !strings.Contains(strings.Join(report.Notes, " "), "raised max clusters") {
		t.Fatalf("discovery was not auto-scaled: %#v", report)
	}
}

func TestSinglePackageModuleHasNeutralCohesion(t *testing.T) {
	files := map[string]*sourceFile{
		"a/one.go": {Path: "a/one.go", Module: "a"},
		"a/two.go": {Path: "a/two.go", Module: "a"},
		"b/one.go": {Path: "b/one.go", Module: "b"},
	}
	edges := []edge{{FromFile: "a/one.go", ToFile: "b/one.go", FromModule: "a", ToModule: "b"}}
	reports := moduleReports(files, edges, Config{Modules: []ModuleConfig{{Name: "a"}, {Name: "b"}}})
	if reports[0].Units != 1 || reports[0].Cohesion != 1 {
		t.Fatalf("single Go package should be neutral: %#v", reports[0])
	}
	if reports[0].CohesionEvaluated {
		t.Fatalf("single Go package must not count as cohesion evidence: %#v", reports[0])
	}
}

func TestCouplingLoadUsesTargetInstability(t *testing.T) {
	files := map[string]*sourceFile{
		"a/a.go": {Path: "a/a.go", Module: "a"},
		"b/b.go": {Path: "b/b.go", Module: "b"},
		"c/c.go": {Path: "c/c.go", Module: "c"},
	}
	edges := []edge{
		{FromModule: "a", ToModule: "b"},
		{FromModule: "b", ToModule: "c"},
	}
	reports := moduleReports(files, edges, Config{Modules: []ModuleConfig{{Name: "a"}, {Name: "b"}, {Name: "c"}}})
	if reports[0].CouplingLoad != .5 || reports[1].CouplingLoad != 0 {
		t.Fatalf("unexpected stability-weighted load: %#v", reports)
	}
}

func TestObservedScoreExcludesUnevaluatedPoints(t *testing.T) {
	report := Report{Components: []ComponentScore{
		{Name: "known-good", Score: 30, Max: 30, Confidence: 1},
		{Name: "unknown", Score: 70, Max: 70, Confidence: 0},
	}}
	finalizeScores(&report)
	if report.Score != 100 || report.NominalScore != 100 || report.EvaluatedPoints != 30 || report.Confidence != .3 {
		t.Fatalf("unexpected evidence-normalized score: %#v", report)
	}
	if report.ScoreLowerBound != 30 || report.ScoreUpperBound != 100 {
		t.Fatalf("unexpected uncertainty bounds: %#v", report)
	}
}

func TestObservedScoreWeightsPartialEvidence(t *testing.T) {
	report := Report{Components: []ComponentScore{
		{Name: "good", Score: 10, Max: 10, Confidence: 1},
		{Name: "partial-bad", Score: 0, Max: 10, Confidence: .5},
	}}
	finalizeScores(&report)
	if report.Score != 67 || report.EvaluatedPoints != 15 || report.Confidence != .15 {
		t.Fatalf("unexpected partial-evidence score: %#v", report)
	}
	if report.ScoreLowerBound != 10 || report.ScoreUpperBound != 95 {
		t.Fatalf("unexpected partial-evidence bounds: %#v", report)
	}
}

func TestUnassignedFilesReduceStructuralConfidence(t *testing.T) {
	report := Report{
		Files: 10, AnalysisCoverage: .5,
		Components: []ComponentScore{
			{Name: "acyclicity", Score: 20, Max: 20, Confidence: 1},
			{Name: "module-coverage", Score: 5, Max: 10, Confidence: 1},
		},
	}
	finalizeScores(&report)
	if report.Components[0].Confidence != .5 || report.Components[0].EvaluatedPoints != 10 {
		t.Fatalf("structural confidence ignored source coverage: %#v", report)
	}
	if report.Components[1].Confidence != 1 {
		t.Fatalf("coverage confidence must remain directly evaluable: %#v", report)
	}
}

func TestAcyclicityScoresAffectedModuleRatio(t *testing.T) {
	modules := []ModuleConfig{{Name: "a"}, {Name: "b"}, {Name: "c"}, {Name: "d"}}
	files := map[string]*sourceFile{}
	for _, module := range modules {
		files[module.Name+"/x.go"] = &sourceFile{Path: module.Name + "/x.go", Module: module.Name}
	}
	edges := []edge{
		{FromModule: "a", ToModule: "b"},
		{FromModule: "b", ToModule: "a"},
	}
	report := buildReport(files, edges, Config{Modules: modules})
	if report.Components[1].Score != 10 || report.Components[1].Detail != "2/4 active modules participate in 1 strongly connected components" {
		t.Fatalf("acyclicity did not reflect affected modules: %#v", report.Components[1])
	}
}

func TestCycleDiagnosticsIdentifyStructuralHub(t *testing.T) {
	modules := []ModuleConfig{{Name: "hub", Role: "composition-root"}, {Name: "b"}, {Name: "c"}}
	edges := []edge{
		{FromModule: "hub", ToModule: "b"}, {FromModule: "b", ToModule: "hub"},
		{FromModule: "hub", ToModule: "c"}, {FromModule: "c", ToModule: "hub"},
	}
	cycles := moduleCycles(edges)
	diagnostics := diagnoseCycles(cycles, edges, modules)
	if len(diagnostics) != 1 || len(diagnostics[0].BreakCandidates) == 0 {
		t.Fatalf("missing cycle diagnostics: %#v", diagnostics)
	}
	best := diagnostics[0].BreakCandidates[0]
	if best.Module != "hub" || best.Role != "composition-root" || best.RemainingCycleModules != 0 || best.Reduction != 3 {
		t.Fatalf("wrong cycle hub: %#v", best)
	}
}

func TestObservationalPolicyExcludesBoundaryPoints(t *testing.T) {
	files := map[string]*sourceFile{"a/a.go": {Path: "a/a.go", Module: "a"}}
	report := buildReport(files, nil, Config{PolicyMode: "observational", Modules: []ModuleConfig{{Name: "a"}}})
	finalizeScores(&report)
	if report.Components[0].Confidence != 0 || report.Components[0].EvaluatedPoints != 0 {
		t.Fatalf("observational policy counted as enforcement evidence: %#v", report.Components[0])
	}
}

func TestBoundaryScoreUsesViolationBreadthNotDuplicateImports(t *testing.T) {
	modules := []ModuleConfig{{Name: "a"}, {Name: "b"}, {Name: "c"}}
	files := map[string]*sourceFile{
		"a/a.go": {Path: "a/a.go", Module: "a"},
		"b/b.go": {Path: "b/b.go", Module: "b"},
		"c/c.go": {Path: "c/c.go", Module: "c"},
	}
	edges := []edge{
		{FromFile: "a/1.go", ToFile: "b/b.go", FromModule: "a", ToModule: "b"},
		{FromFile: "a/2.go", ToFile: "b/b.go", FromModule: "a", ToModule: "b"},
		{FromFile: "a/3.go", ToFile: "b/b.go", FromModule: "a", ToModule: "b"},
		{FromFile: "a/a.go", ToFile: "c/c.go", FromModule: "a", ToModule: "c"},
	}
	modules[0].MayDependOn = []string{"c"}
	report := buildReport(files, edges, Config{Modules: modules})
	component := report.Components[0]
	if component.Score != 15 || component.Detail != "1/2 observed dependency pairs violate policy across 3 import edges; policy mode enforced" {
		t.Fatalf("duplicate imports distorted boundary score: %#v", component)
	}
}

func TestDiscoverRespectsBoundaryFileLimit(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/app\n")
	write(t, root, "a/one.go", "package a\nimport \"example.test/app/b\"\n")
	write(t, root, "a/two.go", "package a\n")
	write(t, root, "b/one.go", "package b\n")
	write(t, root, "b/two.go", "package b\n")
	report, err := Discover(root, DiscoverConfig{MinClusters: 1, MaxClusters: 2, MaxBoundarySize: 10, MaxBoundaryFiles: 3})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Candidates) != 1 || report.Candidates[0].ClusterCount != 2 {
		t.Fatalf("file limit was ignored: %#v", report.Candidates)
	}
	if len(report.Notes) == 0 {
		t.Fatal("expected an explanation for stopping before min-clusters")
	}
}

func TestDiscoverReturnsStoppedCandidate(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/app\n")
	write(t, root, "a/a.go", "package a\n")
	write(t, root, "b/b.go", "package b\n")
	report, err := Discover(root, DiscoverConfig{MinClusters: 1, MaxClusters: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Candidates) != 1 || report.Candidates[0].ClusterCount != 2 {
		t.Fatalf("stopped candidate missing: %#v", report.Candidates)
	}
}

func TestPublicContractRejectsPrivateImportsAndCountsExports(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/app\n")
	write(t, root, "provider/api/api.go", "package api\ntype Client struct{}\nfunc New() Client { return Client{} }\n")
	write(t, root, "provider/internal/impl.go", "package internal\ntype Secret struct{}\n")
	write(t, root, "consumer/use.go", "package consumer\nimport \"example.test/app/provider/internal\"\n")
	limit := 1
	cfg := Config{Modules: []ModuleConfig{
		{Name: "provider", Paths: []string{"provider/**"}, PublicPaths: []string{"provider/api/**"}, MaxPublicSymbols: &limit},
		{Name: "consumer", Paths: []string{"consumer/**"}, MayDependOn: []string{"provider"}},
	}}
	report, err := Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Violations) != 1 || report.Violations[0].Kind != "private-dependency" {
		t.Fatalf("private import not rejected: %#v", report.Violations)
	}
	if report.Modules[0].PublicSymbols != 2 {
		t.Fatalf("exports=%d", report.Modules[0].PublicSymbols)
	}
	if report.Modules[0].PrivateInboundViolations != 1 {
		t.Fatalf("private inbound=%d", report.Modules[0].PrivateInboundViolations)
	}
	if report.Components[2].Name != "public-contract" || report.Components[2].Score != 8 {
		t.Fatalf("public contract score: %#v", report.Components[2])
	}
	if report.Components[2].Confidence != 1 {
		t.Fatalf("the only consumed contract should provide full evidence: %#v", report.Components[2])
	}
}

func TestPublicContractEvidenceIsWeightedByInboundBoundaries(t *testing.T) {
	limit := 20
	modules := []ModuleConfig{
		{Name: "platform", PublicPaths: []string{"platform/api/**"}, MaxPublicSymbols: &limit},
		{Name: "unused"}, {Name: "a"}, {Name: "b"},
	}
	files := map[string]*sourceFile{
		"platform/api/api.go": {Path: "platform/api/api.go", Module: "platform"},
		"unused/u.go":         {Path: "unused/u.go", Module: "unused"},
		"a/a.go":              {Path: "a/a.go", Module: "a"},
		"b/b.go":              {Path: "b/b.go", Module: "b"},
	}
	edges := []edge{
		{FromModule: "a", ToModule: "platform", ToFile: "platform/api/api.go"},
		{FromModule: "b", ToModule: "platform", ToFile: "platform/api/api.go"},
	}
	report := buildReport(files, edges, Config{Modules: modules})
	if report.Components[2].Confidence != 1 {
		t.Fatalf("unused modules diluted contract evidence: %#v", report.Components[2])
	}
}

func TestCompareReportsDetectsArchitectureRegressions(t *testing.T) {
	baseline := Report{Score: 90, Confidence: 1, Modules: []ModuleReport{{Name: "api", PublicSymbols: 2}}}
	current := Report{Score: 85, Confidence: .8, Violations: []Violation{{Kind: "private-dependency", FromModule: "a", ToModule: "b", FromFile: "a/x.go", Line: 3}}, Cycles: [][]string{{"a", "b"}}, Unknown: []string{"unknown.go"}, Modules: []ModuleReport{{Name: "api", PublicSymbols: 3}}}
	comparison := CompareReports(baseline, current)
	if comparison.Passed || len(comparison.Regressions) != 6 {
		t.Fatalf("unexpected comparison: %#v", comparison)
	}
}

func TestCompareReportsRejectsDifferentScoringVersions(t *testing.T) {
	result := CompareReports(Report{Version: "1", Score: 90}, Report{Version: "2", Score: 90})
	if result.Passed || len(result.Regressions) != 1 || result.Regressions[0].Kind != "report-version" {
		t.Fatalf("version mismatch was not rejected: %#v", result)
	}
}

func TestTypeScriptCommentsAreIgnored(t *testing.T) {
	root := t.TempDir()
	write(t, root, "a/a.ts", "// import '../b/b'\n/* require('../b/b') */\nexport const a=1")
	write(t, root, "b/b.ts", "export const b=1")
	cfg := Config{Modules: []ModuleConfig{{Name: "a", Paths: []string{"a/**"}}, {Name: "b", Paths: []string{"b/**"}}}}
	r, err := Analyze(root, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if r.Edges != 0 || len(r.Violations) != 0 {
		t.Fatalf("unexpected imports: %#v", r)
	}
}

func TestGlob(t *testing.T) {
	for _, tc := range []struct {
		pattern, value string
		want           bool
	}{
		{"src/**", "src/a/b.ts", true},
		{"src/*.ts", "src/a.ts", true},
		{"src/*.ts", "src/a/b.ts", false},
	} {
		if got := glob(tc.pattern, tc.value); got != tc.want {
			t.Errorf("glob(%q,%q)=%v", tc.pattern, tc.value, got)
		}
	}
}

func TestAnalyzeRejectsOverlappingModulePaths(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/app\n")
	write(t, root, "internal/service/a.go", "package service\n")
	_, err := Analyze(root, Config{Modules: []ModuleConfig{
		{Name: "parent", Paths: []string{"internal/**"}},
		{Name: "service", Paths: []string{"internal/service/**"}},
	}})
	if err == nil || !strings.Contains(err.Error(), "matches multiple modules: parent, service") {
		t.Fatalf("expected overlap error, got %v", err)
	}
}

func TestEmptyPublicPathIsNotEvidence(t *testing.T) {
	root := t.TempDir()
	write(t, root, "go.mod", "module example.test/app\n")
	write(t, root, "feature/impl.go", "package feature\n")
	report, err := Analyze(root, Config{Modules: []ModuleConfig{{
		Name: "feature", Paths: []string{"feature/**"}, PublicPaths: []string{"feature/public/**"},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if report.Components[2].Confidence != 0 || len(report.Modules[0].Warnings) != 1 {
		t.Fatalf("empty public path counted as evidence: %#v", report)
	}
}

func write(t *testing.T, root, name, body string) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}
