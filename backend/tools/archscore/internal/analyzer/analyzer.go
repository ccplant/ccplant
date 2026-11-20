package analyzer

import (
	"bufio"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	tsImport        = regexp.MustCompile(`(?m)(?:import|export)\s+(?:[^;\n]*?\s+from\s+)?["']([^"']+)["']|require\s*\(\s*["']([^"']+)["']\s*\)|import\s*\(\s*["']([^"']+)["']\s*\)`)
	goModule        = regexp.MustCompile(`(?m)^module\s+(\S+)\s*$`)
	generatedMarker = regexp.MustCompile(`(?m)^\s*(?://|/\*)\s*(?:Code generated .* DO NOT EDIT\.|@generated\b)`)
)

func Analyze(root string, cfg Config) (Report, error) {
	if cfg.MaxFanOut <= 0 {
		cfg.MaxFanOut = 5
	}
	if cfg.MaxCouplingLoad <= 0 {
		cfg.MaxCouplingLoad = 5
	}
	if cfg.HistoryCommits <= 0 {
		cfg.HistoryCommits = 100
	}
	if cfg.MinModuleChanges <= 0 {
		cfg.MinModuleChanges = 5
	}
	if cfg.MinCochange <= 0 {
		cfg.MinCochange = 3
	}
	if cfg.MinLocalityRatio <= 0 {
		cfg.MinLocalityRatio = 0.8
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return Report{}, err
	}
	files, modulePath, err := collect(abs, cfg)
	if err != nil {
		return Report{}, err
	}
	edges := resolveEdges(files, modulePath)
	report := buildReport(files, edges, cfg)
	for _, file := range files {
		if file.Generated {
			report.GeneratedFiles++
			if !cfg.IncludeGeneratedHistory {
				report.HistoryGeneratedExcluded++
			}
		}
	}
	applyHistory(abs, files, cfg, &report)
	finalizeScores(&report)
	return report, nil
}

func collect(root string, cfg Config) (map[string]*sourceFile, string, error) {
	files := map[string]*sourceFile{}
	modulePath := ""
	if b, err := os.ReadFile(filepath.Join(root, "go.mod")); err == nil {
		if m := goModule.FindSubmatch(b); len(m) == 2 {
			modulePath = string(m[1])
		}
	}
	err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			base := d.Name()
			if base == ".git" || base == "node_modules" || base == "vendor" || base == "dist" {
				return filepath.SkipDir
			}
			return nil
		}
		if excluded(rel, cfg.Exclude) {
			return nil
		}
		if !cfg.IncludeTests && isTestSource(rel) {
			return nil
		}
		ext := filepath.Ext(rel)
		if ext != ".go" && ext != ".ts" && ext != ".tsx" && ext != ".mts" && ext != ".cts" {
			return nil
		}
		mod, matches := assignModule(rel, cfg.Modules)
		if len(matches) > 1 {
			return fmt.Errorf("%s matches multiple modules: %s", rel, strings.Join(matches, ", "))
		}
		imps, err := parseImports(p, ext)
		if err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		exports, err := countExports(p, ext)
		if err != nil {
			return fmt.Errorf("%s exports: %w", rel, err)
		}
		generated, err := isGeneratedSource(p)
		if err != nil {
			return fmt.Errorf("%s generated marker: %w", rel, err)
		}
		files[rel] = &sourceFile{Path: rel, Module: mod, Imports: imps, Exports: exports, Generated: generated}
		return nil
	})
	return files, modulePath, err
}

func isGeneratedSource(path string) (bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	if len(b) > 4096 {
		b = b[:4096]
	}
	return generatedMarker.Match(b), nil
}

var tsExportDecl = regexp.MustCompile(`(?m)^\s*export\s+(?:default\s+)?(?:declare\s+)?(?:async\s+)?(?:class|function|const|let|var|interface|type|enum)\s+[A-Za-z_$][\w$]*`)
var tsExportList = regexp.MustCompile(`(?s)export\s*\{([^}]*)\}`)

func countExports(path, ext string) (int, error) {
	if ext == ".go" {
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return 0, err
		}
		count := 0
		for _, decl := range f.Decls {
			switch d := decl.(type) {
			case *ast.FuncDecl:
				if d.Name.IsExported() {
					count++
				}
			case *ast.GenDecl:
				for _, spec := range d.Specs {
					switch s := spec.(type) {
					case *ast.TypeSpec:
						if s.Name.IsExported() {
							count++
						}
					case *ast.ValueSpec:
						for _, name := range s.Names {
							if name.IsExported() {
								count++
							}
						}
					}
				}
			}
		}
		return count, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	clean := stripComments(string(b))
	count := len(tsExportDecl.FindAllStringIndex(clean, -1))
	for _, match := range tsExportList.FindAllStringSubmatch(clean, -1) {
		for _, item := range strings.Split(match[1], ",") {
			if strings.TrimSpace(item) != "" {
				count++
			}
		}
	}
	return count, nil
}

func parseImports(path, ext string) ([]sourceImport, error) {
	if ext == ".go" {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, path, nil, parser.ImportsOnly)
		if err != nil {
			return nil, err
		}
		out := make([]sourceImport, 0, len(f.Imports))
		for _, imp := range f.Imports {
			v, _ := strconv.Unquote(imp.Path.Value)
			out = append(out, sourceImport{Path: v, Line: fset.Position(imp.Pos()).Line})
		}
		return out, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	clean := stripComments(string(b))
	matches := tsImport.FindAllStringSubmatchIndex(clean, -1)
	out := make([]sourceImport, 0, len(matches))
	for _, m := range matches {
		value, pos := "", -1
		for g := 1; g <= 3; g++ {
			if m[g*2] >= 0 {
				pos = m[g*2]
				value = clean[m[g*2]:m[g*2+1]]
				break
			}
		}
		if pos >= 0 {
			out = append(out, sourceImport{Path: value, Line: 1 + strings.Count(clean[:pos], "\n")})
		}
	}
	return out, nil
}

func isTestSource(path string) bool {
	base := filepath.Base(path)
	return strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, ".test.ts") || strings.HasSuffix(base, ".test.tsx") ||
		strings.HasSuffix(base, ".spec.ts") || strings.HasSuffix(base, ".spec.tsx")
}

func stripComments(s string) string {
	b := []byte(s)
	line, block, quote := false, false, byte(0)
	for i := 0; i < len(b); i++ {
		if line {
			if b[i] == '\n' {
				line = false
			} else {
				b[i] = ' '
			}
			continue
		}
		if block {
			if i+1 < len(b) && b[i] == '*' && b[i+1] == '/' {
				b[i], b[i+1] = ' ', ' '
				i++
				block = false
			} else if b[i] != '\n' {
				b[i] = ' '
			}
			continue
		}
		if quote != 0 {
			if b[i] == '\\' {
				i++
			} else if b[i] == quote {
				quote = 0
			}
			continue
		}
		if b[i] == '\'' || b[i] == '"' || b[i] == '`' {
			quote = b[i]
			continue
		}
		if i+1 < len(b) && b[i] == '/' && b[i+1] == '/' {
			b[i], b[i+1] = ' ', ' '
			i++
			line = true
		}
		if i+1 < len(b) && b[i] == '/' && b[i+1] == '*' {
			b[i], b[i+1] = ' ', ' '
			i++
			block = true
		}
	}
	return string(b)
}

func resolveEdges(files map[string]*sourceFile, goMod string) []edge {
	var out []edge
	for _, f := range files {
		for _, imp := range f.Imports {
			target := ""
			if strings.HasPrefix(filepath.Ext(f.Path), ".go") && goMod != "" && (imp.Path == goMod || strings.HasPrefix(imp.Path, goMod+"/")) {
				dir := strings.TrimPrefix(strings.TrimPrefix(imp.Path, goMod), "/")
				target = firstGoFile(files, dir)
			} else if strings.HasPrefix(imp.Path, ".") {
				base := filepath.ToSlash(filepath.Clean(filepath.Join(filepath.Dir(f.Path), imp.Path)))
				target = resolveTS(files, base)
			}
			if target != "" {
				t := files[target]
				out = append(out, edge{f.Path, target, f.Module, t.Module, imp.Path, imp.Line})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].FromFile == out[j].FromFile {
			return out[i].Line < out[j].Line
		}
		return out[i].FromFile < out[j].FromFile
	})
	return out
}

func firstGoFile(files map[string]*sourceFile, dir string) string {
	var found []string
	for p := range files {
		if filepath.Ext(p) == ".go" && filepath.ToSlash(filepath.Dir(p)) == dir {
			found = append(found, p)
		}
	}
	sort.Strings(found)
	if len(found) > 0 {
		return found[0]
	}
	return ""
}

func resolveTS(files map[string]*sourceFile, base string) string {
	for _, p := range []string{base, base + ".ts", base + ".tsx", base + ".mts", base + ".cts", base + "/index.ts", base + "/index.tsx"} {
		if _, ok := files[p]; ok {
			return p
		}
	}
	return ""
}

func assignModule(path string, modules []ModuleConfig) (string, []string) {
	var matches []string
	for _, m := range modules {
		for _, pattern := range m.Paths {
			if glob(pattern, path) {
				matches = append(matches, m.Name)
				break
			}
		}
	}
	if len(matches) == 0 {
		return "", nil
	}
	return matches[0], matches
}

func excluded(path string, patterns []string) bool {
	for _, p := range patterns {
		if glob(p, path) {
			return true
		}
	}
	return false
}

func glob(pattern, value string) bool {
	q := regexp.QuoteMeta(filepath.ToSlash(pattern))
	q = strings.ReplaceAll(q, `\*\*`, `.*`)
	q = strings.ReplaceAll(q, `\*`, `[^/]*`)
	q = strings.ReplaceAll(q, `\?`, `[^/]`)
	ok, _ := regexp.MatchString("^"+q+"$", filepath.ToSlash(value))
	return ok
}

func buildReport(files map[string]*sourceFile, edges []edge, cfg Config) Report {
	r := Report{Version: "2", Files: len(files), Edges: len(edges), Gate: "pass"}
	allowed := map[string]map[string]bool{}
	for _, m := range cfg.Modules {
		allowed[m.Name] = map[string]bool{m.Name: true}
		for _, d := range m.MayDependOn {
			allowed[m.Name][d] = true
		}
	}
	for _, e := range edges {
		if e.FromModule == "" || e.ToModule == "" || e.FromModule == e.ToModule {
			continue
		}
		if !allowed[e.FromModule][e.ToModule] {
			r.Violations = append(r.Violations, Violation{"forbidden-dependency", e.FromModule, e.ToModule, e.FromFile, e.ToFile, e.Line, e.Import})
			continue
		}
		target := findModuleConfig(cfg.Modules, e.ToModule)
		if len(target.PublicPaths) > 0 && !matchesAny(e.ToFile, target.PublicPaths) {
			r.Violations = append(r.Violations, Violation{"private-dependency", e.FromModule, e.ToModule, e.FromFile, e.ToFile, e.Line, e.Import})
		}
	}
	r.Cycles = moduleCycles(edges)
	r.CycleDiagnostics = diagnoseCycles(r.Cycles, edges, cfg.Modules)
	if len(r.Violations) > 0 || len(r.Cycles) > 0 {
		r.Gate = "fail"
	}
	r.Modules = moduleReports(files, edges, cfg)
	for _, f := range files {
		if f.Module == "" {
			r.Unknown = append(r.Unknown, f.Path)
		}
	}
	sort.Strings(r.Unknown)
	dependencyPairs, violatingPairs := map[string]bool{}, map[string]bool{}
	for _, dependency := range edges {
		if dependency.FromModule != "" && dependency.ToModule != "" && dependency.FromModule != dependency.ToModule {
			dependencyPairs[directedKey(dependency.FromModule, dependency.ToModule)] = true
		}
	}
	for _, violation := range r.Violations {
		violatingPairs[violation.Kind+"\x00"+directedKey(violation.FromModule, violation.ToModule)] = true
	}
	boundary := 30
	if len(dependencyPairs) > 0 {
		boundary = int(math.Round(30 * (1 - float64(len(violatingPairs))/float64(len(dependencyPairs)))))
		boundary = maxInt(0, boundary)
	}
	activeModules, cyclicModules := 0, map[string]bool{}
	for _, module := range r.Modules {
		if module.Files > 0 {
			activeModules++
		}
	}
	for _, cycle := range r.Cycles {
		for _, module := range cycle {
			cyclicModules[module] = true
		}
	}
	cycles := 20
	if activeModules > 0 {
		cycles = int(math.Round(20 * float64(activeModules-len(cyclicModules)) / float64(activeModules)))
	}
	rawOver, loadOver, eligible := 0, 0, 0
	for _, m := range r.Modules {
		moduleCfg := findModuleConfig(cfg.Modules, m.Name)
		if moduleCfg.Role == "composition-root" {
			continue
		}
		eligible++
		rawThreshold := cfg.MaxFanOut
		if moduleCfg.MaxFanOut != nil {
			rawThreshold = *moduleCfg.MaxFanOut
		}
		loadThreshold := cfg.MaxCouplingLoad
		if moduleCfg.MaxCouplingLoad != nil {
			loadThreshold = *moduleCfg.MaxCouplingLoad
		}
		if rawThreshold > 0 && m.FanOut > rawThreshold {
			rawOver++
		}
		if loadThreshold > 0 && m.CouplingLoad > loadThreshold {
			loadOver++
		}
	}
	coupling := 15
	if eligible > 0 {
		rawCompliance := 1 - float64(rawOver)/float64(eligible)
		loadCompliance := 1 - float64(loadOver)/float64(eligible)
		coupling = int(math.Round(15 * (rawCompliance + loadCompliance) / 2))
	}
	cohesionWeighted, cohesionWeight, totalCohesionWeight := 0.0, 0, 0
	for _, m := range r.Modules {
		weight := m.InternalEdges + m.ExternalEdges
		totalCohesionWeight += weight
		if m.CohesionEvaluated {
			cohesionWeighted += m.Cohesion * float64(weight)
			cohesionWeight += weight
		}
	}
	cohesion, cohesionConfidence := 10, 0.0
	if cohesionWeight > 0 {
		cohesion = int(math.Round(10 * cohesionWeighted / float64(cohesionWeight)))
	}
	if totalCohesionWeight > 0 {
		cohesionConfidence = float64(cohesionWeight) / float64(totalCohesionWeight)
	}
	coverage := 10
	r.AnalysisCoverage = 1
	if len(files) > 0 {
		r.AnalysisCoverage = float64(len(files)-len(r.Unknown)) / float64(len(files))
		coverage = int(math.Round(10 * r.AnalysisCoverage))
	}
	publicWeightedScore, publicConfigured, publicCoveredWeight, publicTotalWeight := 0, 0, 0, 0
	for _, module := range r.Modules {
		publicTotalWeight += module.FanIn
		moduleCfg := findModuleConfig(cfg.Modules, module.Name)
		if len(moduleCfg.PublicPaths) == 0 {
			continue
		}
		if module.PublicFiles == 0 {
			continue
		}
		publicConfigured++
		if module.FanIn == 0 {
			continue
		}
		publicCoveredWeight += module.FanIn
		limit := 20
		if moduleCfg.MaxPublicSymbols != nil {
			limit = *moduleCfg.MaxPublicSymbols
		}
		if limit >= 0 && module.PublicSymbols > limit {
			publicWeightedScore += module.FanIn * maxInt(0, 10-int(math.Ceil(float64(module.PublicSymbols-limit)/5))*2)
		} else {
			publicWeightedScore += module.FanIn * 10
		}
	}
	publicScore := 10
	if publicCoveredWeight > 0 {
		publicScore = int(math.Round(float64(publicWeightedScore) / float64(publicCoveredWeight)))
	}
	publicConfidence := float64(publicCoveredWeight) / float64(maxInt(1, publicTotalWeight))
	boundaryConfidence := 1.0
	if cfg.PolicyMode == "observational" {
		boundaryConfidence = 0
	}
	r.Components = []ComponentScore{
		{"boundaries", boundary, 30, fmt.Sprintf("%d/%d observed dependency pairs violate policy across %d import edges; policy mode %s", len(violatingPairs), len(dependencyPairs), len(r.Violations), effectivePolicyMode(cfg.PolicyMode)), boundaryConfidence, 0},
		{"acyclicity", cycles, 20, fmt.Sprintf("%d/%d active modules participate in %d strongly connected components", len(cyclicModules), activeModules, len(r.Cycles)), 1, 0},
		{"public-contract", publicScore, 10, fmt.Sprintf("%d modules declare non-empty public paths covering %d/%d inbound module relationships", publicConfigured, publicCoveredWeight, publicTotalWeight), round(publicConfidence), 0},
		{"coupling", coupling, 15, fmt.Sprintf("%d/%d modules exceed fan-out %d; %d/%d exceed stability-weighted load %.1f; composition roots exempt", rawOver, eligible, cfg.MaxFanOut, loadOver, eligible, cfg.MaxCouplingLoad), 1, 0},
		{"cohesion", cohesion, 10, fmt.Sprintf("%d/%d dependency-edge weight is evaluable; single-package modules excluded", cohesionWeight, totalCohesionWeight), round(cohesionConfidence), 0},
		{"module-coverage", coverage, 10, fmt.Sprintf("%d unassigned files", len(r.Unknown)), 1, 0},
	}
	return r
}

func effectivePolicyMode(mode string) string {
	if mode == "" {
		return "enforced"
	}
	return mode
}

func matchesAny(path string, patterns []string) bool {
	for _, pattern := range patterns {
		if glob(pattern, path) {
			return true
		}
	}
	return false
}

func findModuleConfig(configs []ModuleConfig, name string) ModuleConfig {
	for _, config := range configs {
		if config.Name == name {
			return config
		}
	}
	return ModuleConfig{}
}

func moduleReports(files map[string]*sourceFile, edges []edge, cfg Config) []ModuleReport {
	reports := map[string]*ModuleReport{}
	for _, c := range cfg.Modules {
		reports[c.Name] = &ModuleReport{Name: c.Name}
	}
	for _, f := range files {
		if reports[f.Module] != nil {
			reports[f.Module].Files++
		}
	}
	unitSets := map[string]map[string]bool{}
	for _, f := range files {
		if reports[f.Module] == nil {
			continue
		}
		if unitSets[f.Module] == nil {
			unitSets[f.Module] = map[string]bool{}
		}
		unitSets[f.Module][filepath.ToSlash(filepath.Dir(f.Path))] = true
	}
	in, out := map[string]map[string]bool{}, map[string]map[string]bool{}
	for _, e := range edges {
		if reports[e.FromModule] == nil || reports[e.ToModule] == nil {
			continue
		}
		if e.FromModule == e.ToModule {
			reports[e.FromModule].InternalEdges++
		} else {
			reports[e.FromModule].ExternalEdges++
			targetCfg := findModuleConfig(cfg.Modules, e.ToModule)
			if len(targetCfg.PublicPaths) == 0 || matchesAny(e.ToFile, targetCfg.PublicPaths) {
				reports[e.ToModule].PublicInboundEdges++
			} else {
				reports[e.ToModule].PrivateInboundViolations++
			}
			if out[e.FromModule] == nil {
				out[e.FromModule] = map[string]bool{}
			}
			out[e.FromModule][e.ToModule] = true
			if in[e.ToModule] == nil {
				in[e.ToModule] = map[string]bool{}
			}
			in[e.ToModule][e.FromModule] = true
		}
	}
	result := make([]ModuleReport, 0, len(cfg.Modules))
	for _, c := range cfg.Modules {
		r := reports[c.Name]
		r.Units = len(unitSets[c.Name])
		publicUnits := map[string]bool{}
		for _, f := range files {
			if f.Module == c.Name && matchesAny(f.Path, c.PublicPaths) {
				r.PublicFiles++
				r.PublicSymbols += f.Exports
				publicUnits[filepath.ToSlash(filepath.Dir(f.Path))] = true
			}
		}
		r.PublicUnits = len(publicUnits)
		if len(c.PublicPaths) > 0 && r.PublicFiles == 0 {
			r.Warnings = append(r.Warnings, "public paths match no analyzed source files; contract not evaluated")
		}
		r.FanIn, r.FanOut = len(in[c.Name]), len(out[c.Name])
		if c.Role == "composition-root" && r.FanIn > 0 {
			r.Warnings = append(r.Warnings, fmt.Sprintf("composition root has %d inbound module dependencies; split reusable code from wiring", r.FanIn))
		}
		if r.FanIn+r.FanOut > 0 {
			r.Instability = round(float64(r.FanOut) / float64(r.FanIn+r.FanOut))
		}
		total := r.InternalEdges + r.ExternalEdges
		if r.Units <= 1 || total == 0 {
			r.Cohesion = 1
		} else {
			r.CohesionEvaluated = true
			r.Cohesion = round(float64(r.InternalEdges) / float64(total))
		}
		result = append(result, *r)
	}
	for i := range result {
		load := 0.0
		for target := range out[result[i].Name] {
			if reports[target] != nil {
				load += reports[target].Instability
			}
		}
		result[i].CouplingLoad = round(load)
	}
	return result
}

func moduleCycles(edges []edge) [][]string {
	graph := map[string]map[string]bool{}
	for _, e := range edges {
		if e.FromModule != "" && e.ToModule != "" && e.FromModule != e.ToModule {
			if graph[e.FromModule] == nil {
				graph[e.FromModule] = map[string]bool{}
			}
			graph[e.FromModule][e.ToModule] = true
		}
	}
	index, indices, low, stack, on := 0, map[string]int{}, map[string]int{}, []string{}, map[string]bool{}
	var cycles [][]string
	var visit func(string)
	visit = func(v string) {
		index++
		indices[v], low[v] = index, index
		stack = append(stack, v)
		on[v] = true
		for w := range graph[v] {
			if indices[w] == 0 {
				visit(w)
				low[v] = minInt(low[v], low[w])
			} else if on[w] {
				low[v] = minInt(low[v], indices[w])
			}
		}
		if low[v] == indices[v] {
			var c []string
			for {
				w := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				on[w] = false
				c = append(c, w)
				if w == v {
					break
				}
			}
			if len(c) > 1 {
				sort.Strings(c)
				cycles = append(cycles, c)
			}
		}
	}
	for v := range graph {
		if indices[v] == 0 {
			visit(v)
		}
	}
	sort.Slice(cycles, func(i, j int) bool { return strings.Join(cycles[i], ",") < strings.Join(cycles[j], ",") })
	return cycles
}

func diagnoseCycles(cycles [][]string, edges []edge, configs []ModuleConfig) []CycleDiagnostic {
	diagnostics := make([]CycleDiagnostic, 0, len(cycles))
	for _, cycle := range cycles {
		members := map[string]bool{}
		for _, module := range cycle {
			members[module] = true
		}
		pairs := map[string]bool{}
		in, out := map[string]map[string]bool{}, map[string]map[string]bool{}
		var induced []edge
		for _, dependency := range edges {
			if dependency.FromModule == dependency.ToModule || !members[dependency.FromModule] || !members[dependency.ToModule] {
				continue
			}
			key := directedKey(dependency.FromModule, dependency.ToModule)
			if pairs[key] {
				continue
			}
			pairs[key] = true
			induced = append(induced, dependency)
			if out[dependency.FromModule] == nil {
				out[dependency.FromModule] = map[string]bool{}
			}
			if in[dependency.ToModule] == nil {
				in[dependency.ToModule] = map[string]bool{}
			}
			out[dependency.FromModule][dependency.ToModule] = true
			in[dependency.ToModule][dependency.FromModule] = true
		}
		diagnostic := CycleDiagnostic{Modules: len(cycle), DependencyPairs: len(pairs)}
		for _, candidate := range cycle {
			filtered := make([]edge, 0, len(induced))
			for _, dependency := range induced {
				if dependency.FromModule != candidate && dependency.ToModule != candidate {
					filtered = append(filtered, dependency)
				}
			}
			remaining := 0
			for _, residual := range moduleCycles(filtered) {
				remaining = maxInt(remaining, len(residual))
			}
			config := findModuleConfig(configs, candidate)
			diagnostic.BreakCandidates = append(diagnostic.BreakCandidates, CycleBreakCandidate{
				Module: candidate, Role: config.Role, Inbound: len(in[candidate]), Outbound: len(out[candidate]),
				RemainingCycleModules: remaining, Reduction: len(cycle) - remaining,
			})
		}
		sort.Slice(diagnostic.BreakCandidates, func(i, j int) bool {
			a, b := diagnostic.BreakCandidates[i], diagnostic.BreakCandidates[j]
			if a.Reduction != b.Reduction {
				return a.Reduction > b.Reduction
			}
			if a.Inbound+a.Outbound != b.Inbound+b.Outbound {
				return a.Inbound+a.Outbound > b.Inbound+b.Outbound
			}
			return a.Module < b.Module
		})
		if len(diagnostic.BreakCandidates) > 5 {
			diagnostic.BreakCandidates = diagnostic.BreakCandidates[:5]
		}
		diagnostics = append(diagnostics, diagnostic)
	}
	return diagnostics
}

func applyHistory(root string, files map[string]*sourceFile, cfg Config, report *Report) {
	cmd := exec.Command("git", "-C", root, "log", "-n", strconv.Itoa(cfg.HistoryCommits), "--name-only", "--pretty=format:__COMMIT__")
	cmd.Args = append(cmd.Args, "--relative", "--", ".")
	b, err := cmd.Output()
	if err != nil {
		report.Components = append(report.Components, ComponentScore{"change-locality", 5, 5, "git history unavailable; not evaluated", 0, 0})
		report.Notes = append(report.Notes, "git history unavailable; change-coupling score omitted")
		return
	}
	counts, contained, pairs := map[string]int{}, map[string]int{}, map[string]map[string]int{}
	matchedCommits := 0
	for _, block := range strings.Split(string(b), "__COMMIT__") {
		mods := map[string]bool{}
		touched := map[string]int{}
		totalFiles := 0
		s := bufio.NewScanner(strings.NewReader(block))
		for s.Scan() {
			p := filepath.ToSlash(strings.TrimSpace(s.Text()))
			if f := files[p]; f != nil && f.Module != "" && (cfg.IncludeGeneratedHistory || !f.Generated) {
				mods[f.Module] = true
				touched[f.Module]++
				totalFiles++
			}
		}
		if len(mods) > 0 {
			matchedCommits++
		}
		for a := range mods {
			counts[a]++
			if totalFiles > 0 && float64(touched[a])/float64(totalFiles) >= cfg.MinLocalityRatio {
				contained[a]++
			}
			if pairs[a] == nil {
				pairs[a] = map[string]int{}
			}
			for z := range mods {
				if a != z {
					pairs[a][z]++
				}
			}
		}
	}
	if matchedCommits == 0 {
		report.Components = append(report.Components, ComponentScore{"change-locality", 5, 5, "no matching git history; not evaluated", 0, 0})
		report.Notes = append(report.Notes, "no analyzed source files were found in git history; change-coupling score omitted")
		return
	}
	evaluated, evaluatedChanges, localChanges := 0, 0, 0
	for i := range report.Modules {
		m := &report.Modules[i]
		m.ChangeCommits = counts[m.Name]
		m.ContainedChanges = contained[m.Name]
		if m.ChangeCommits > 0 {
			m.ChangeLocality = round(float64(m.ContainedChanges) / float64(m.ChangeCommits))
		}
		if m.ChangeCommits < cfg.MinModuleChanges {
			continue
		}
		m.ChangeEvaluated = true
		evaluated++
		evaluatedChanges += m.ChangeCommits
		best := 0.0
		for p, n := range pairs[m.Name] {
			if n < cfg.MinCochange {
				continue
			}
			ratio := float64(n) / float64(maxInt(1, counts[m.Name]))
			if ratio >= 0.5 {
				m.ChangePartners = append(m.ChangePartners, p)
			}
			if ratio > best {
				best = ratio
			}
		}
		sort.Strings(m.ChangePartners)
		m.ChangeCoupling = round(best)
		if m.ChangeLocality >= cfg.MinLocalityRatio {
			localChanges += m.ChangeCommits
		}
	}
	score := 5
	if evaluatedChanges > 0 {
		score = int(math.Round(5 * float64(localChanges) / float64(evaluatedChanges)))
	}
	confidence := float64(evaluated) / float64(maxInt(1, len(report.Modules)))
	report.Components = append(report.Components, ComponentScore{"change-locality", score, 5, fmt.Sprintf("evaluated %d/%d modules and %d changes; %d changes meet the %.0f%% locality target", evaluated, len(report.Modules), evaluatedChanges, localChanges, cfg.MinLocalityRatio*100), round(confidence), 0})
}

func finalizeScores(report *Report) {
	earned, evaluated := 0.0, 0.0
	report.NominalScore = 0
	analysisCoverage := report.AnalysisCoverage
	if report.Files == 0 && analysisCoverage == 0 {
		analysisCoverage = 1
	}
	for i := range report.Components {
		component := &report.Components[i]
		if component.Name != "module-coverage" {
			component.Confidence = round(component.Confidence * analysisCoverage)
		}
		component.EvaluatedPoints = round(float64(component.Max) * component.Confidence)
		evaluated += component.EvaluatedPoints
		earned += float64(component.Score) * component.Confidence
		report.NominalScore += component.Score
	}
	report.EvaluatedPoints = round(evaluated)
	report.Confidence = round(evaluated / 100)
	report.ScoreLowerBound = int(math.Round(earned))
	report.ScoreUpperBound = int(math.Round(earned + maxFloat(0, 100-evaluated)))
	if evaluated == 0 {
		report.Score = 0
		return
	}
	report.Score = int(math.Round(100 * earned / evaluated))
}

func round(v float64) float64 { return math.Round(v*1000) / 1000 }
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
