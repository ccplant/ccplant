package analyzer

import (
	"bufio"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"
)

type discoveryUnit struct {
	name  string
	files int
}

type affinity struct {
	static, change, vocabulary, combined float64
}

type cluster struct{ units []string }

func Discover(root string, cfg DiscoverConfig) (DiscoveryReport, error) {
	if cfg.MinClusters <= 0 {
		cfg.MinClusters = 8
	}
	if cfg.MaxClusters <= 0 {
		cfg.MaxClusters = 30
	}
	if cfg.MinClusters > cfg.MaxClusters {
		return DiscoveryReport{}, fmt.Errorf("min-clusters must not exceed max-clusters")
	}
	if cfg.HistoryCommits <= 0 {
		cfg.HistoryCommits = 200
	}
	if cfg.MinCochange <= 0 {
		cfg.MinCochange = 3
	}
	if cfg.MaxBoundarySize <= 0 {
		cfg.MaxBoundarySize = 12
	}
	if cfg.MaxBoundaryFiles <= 0 {
		cfg.MaxBoundaryFiles = 100
	}

	abs, err := filepath.Abs(root)
	if err != nil {
		return DiscoveryReport{}, err
	}
	files, goMod, err := collect(abs, Config{Exclude: cfg.Exclude})
	if err != nil {
		return DiscoveryReport{}, err
	}
	edges := resolveEdges(files, goMod)
	units, fileUnit := discoveryUnits(files)
	historyFileUnit := make(map[string]string, len(fileUnit))
	generatedFiles := 0
	for path, unit := range fileUnit {
		if files[path].Generated {
			generatedFiles++
			if !cfg.IncludeGeneratedHistory {
				continue
			}
		}
		historyFileUnit[path] = unit
	}
	if len(units) == 0 {
		return DiscoveryReport{}, fmt.Errorf("no Go or TypeScript source units found")
	}
	autoScaleNote := ""
	minimumCapacity := int(math.Ceil(float64(len(units)) / float64(cfg.MaxBoundarySize)))
	if minimumCapacity > cfg.MaxClusters && !cfg.DisableAutoScale {
		old := cfg.MaxClusters
		cfg.MaxClusters = minimumCapacity
		autoScaleNote = fmt.Sprintf("raised max clusters from %d to %d so %d units fit the %d-unit boundary limit", old, cfg.MaxClusters, len(units), cfg.MaxBoundarySize)
	}

	static, edgeCounts, totalEdges := staticAffinities(edges, fileUnit)
	change, historyCount, historyNote := changeAffinities(abs, historyFileUnit, cfg)
	affinities := combinedAffinities(units, static, change)
	candidates := clusterCandidates(units, affinities, edgeCounts, totalEdges, cfg)
	report := DiscoveryReport{
		Version: "1", Units: len(units), Files: len(files), GeneratedFiles: generatedFiles, DependencyEdges: totalEdges,
		HistoryCommits: historyCount,
		Weights:        DiscoveryWeights{Static: 0.5, Change: 0.4, Vocabulary: 0.1},
		Candidates:     candidates,
	}
	if historyNote != "" {
		report.Notes = append(report.Notes, historyNote)
	}
	if autoScaleNote != "" {
		report.Notes = append(report.Notes, autoScaleNote)
	}
	if len(candidates) > 0 && candidates[len(candidates)-1].ClusterCount > cfg.MinClusters {
		report.Notes = append(report.Notes, fmt.Sprintf("stopped at %d clusters because no positive-affinity merge fit limits of %d units and %d files", candidates[len(candidates)-1].ClusterCount, cfg.MaxBoundarySize, cfg.MaxBoundaryFiles))
	}
	return report, nil
}

func discoveryUnits(files map[string]*sourceFile) (map[string]discoveryUnit, map[string]string) {
	units := map[string]discoveryUnit{}
	fileUnit := map[string]string{}
	for path := range files {
		name := filepath.ToSlash(filepath.Dir(path))
		if name == "." {
			name = "root"
		}
		u := units[name]
		u.name = name
		u.files++
		units[name] = u
		fileUnit[path] = name
	}
	return units, fileUnit
}

func staticAffinities(edges []edge, fileUnit map[string]string) (map[string]float64, map[string]int, int) {
	pairs, directed, degrees := map[string]int{}, map[string]int{}, map[string]int{}
	total := 0
	for _, e := range edges {
		a, b := fileUnit[e.FromFile], fileUnit[e.ToFile]
		if a == "" || b == "" || a == b {
			continue
		}
		pairs[pairKey(a, b)]++
		directed[directedKey(a, b)]++
		degrees[a]++
		degrees[b]++
		total++
	}
	out := map[string]float64{}
	for key, count := range pairs {
		a, b := splitPair(key)
		base := float64(count) / math.Sqrt(float64(degrees[a]*degrees[b]))
		forward, reverse := directed[directedKey(a, b)], directed[directedKey(b, a)]
		balance := 0.0
		if forward+reverse > 0 {
			balance = 2 * float64(minInt(forward, reverse)) / float64(forward+reverse)
		}
		out[key] = base * (0.25 + 0.75*balance)
	}
	return out, pairs, total
}

func changeAffinities(root string, fileUnit map[string]string, cfg DiscoverConfig) (map[string]float64, int, string) {
	cmd := exec.Command("git", "-C", root, "log", "-n", strconv.Itoa(cfg.HistoryCommits), "--name-only", "--pretty=format:__COMMIT__", "--relative", "--", ".")
	b, err := cmd.Output()
	if err != nil {
		return map[string]float64{}, 0, "git history unavailable; change affinity omitted"
	}
	counts, pairs := map[string]int{}, map[string]int{}
	commits := 0
	for _, block := range strings.Split(string(b), "__COMMIT__") {
		seen := map[string]bool{}
		s := bufio.NewScanner(strings.NewReader(block))
		for s.Scan() {
			if u := fileUnit[filepath.ToSlash(strings.TrimSpace(s.Text()))]; u != "" {
				seen[u] = true
			}
		}
		if len(seen) == 0 {
			continue
		}
		commits++
		var names []string
		for u := range seen {
			counts[u]++
			names = append(names, u)
		}
		sort.Strings(names)
		for i := 0; i < len(names); i++ {
			for j := i + 1; j < len(names); j++ {
				pairs[pairKey(names[i], names[j])]++
			}
		}
	}
	out := map[string]float64{}
	for key, together := range pairs {
		if together < cfg.MinCochange {
			continue
		}
		a, b := splitPair(key)
		union := counts[a] + counts[b] - together
		if union > 0 {
			out[key] = float64(together) / float64(union)
		}
	}
	if commits == 0 {
		return out, 0, "no matching git history; change affinity omitted"
	}
	return out, commits, ""
}

func combinedAffinities(units map[string]discoveryUnit, static, change map[string]float64) map[string]affinity {
	var names []string
	for name := range units {
		names = append(names, name)
	}
	sort.Strings(names)
	out := map[string]affinity{}
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			key := pairKey(names[i], names[j])
			a := affinity{static: static[key], change: change[key], vocabulary: vocabularyAffinity(names[i], names[j])}
			a.combined = 0.5*a.static + 0.4*a.change + 0.1*a.vocabulary
			if a.combined > 0 {
				out[key] = a
			}
		}
	}
	return out
}

func clusterCandidates(units map[string]discoveryUnit, affinities map[string]affinity, edgeCounts map[string]int, totalEdges int, cfg DiscoverConfig) []BoundaryCandidate {
	clusters := make([]cluster, 0, len(units))
	for name := range units {
		clusters = append(clusters, cluster{[]string{name}})
	}
	sortClusters(clusters)
	minCount := minInt(cfg.MinClusters, len(clusters))
	maxCount := minInt(cfg.MaxClusters, len(clusters))
	targets := map[int]bool{minCount: true, maxCount: true, (minCount + maxCount) / 2: true}
	snapshots := map[int][]cluster{}
	if targets[len(clusters)] {
		snapshots[len(clusters)] = copyClusters(clusters)
	}
	for len(clusters) > minCount {
		bi, bj, best := -1, -1, -1.0
		bestKey := ""
		for i := 0; i < len(clusters); i++ {
			for j := i + 1; j < len(clusters); j++ {
				if len(clusters[i].units)+len(clusters[j].units) > cfg.MaxBoundarySize {
					continue
				}
				if clusterFiles(clusters[i], units)+clusterFiles(clusters[j], units) > cfg.MaxBoundaryFiles {
					continue
				}
				score := clusterAffinity(clusters[i], clusters[j], affinities)
				key := clusterName(clusters[i]) + "|" + clusterName(clusters[j])
				if score > best || (score == best && (bestKey == "" || key < bestKey)) {
					bi, bj, best, bestKey = i, j, score, key
				}
			}
		}
		if bi < 0 || best <= 0 {
			break
		}
		merged := append(append([]string{}, clusters[bi].units...), clusters[bj].units...)
		sort.Strings(merged)
		next := make([]cluster, 0, len(clusters)-1)
		for i, c := range clusters {
			if i != bi && i != bj {
				next = append(next, c)
			}
		}
		clusters = append(next, cluster{merged})
		sortClusters(clusters)
		if targets[len(clusters)] {
			snapshots[len(clusters)] = copyClusters(clusters)
		}
	}
	if _, ok := snapshots[len(clusters)]; !ok {
		snapshots[len(clusters)] = copyClusters(clusters)
	}
	var counts []int
	for n := range snapshots {
		counts = append(counts, n)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(counts)))
	result := make([]BoundaryCandidate, 0, len(counts))
	for _, n := range counts {
		result = append(result, makeCandidate(snapshots[n], units, affinities, edgeCounts, totalEdges))
	}
	return result
}

func makeCandidate(clusters []cluster, units map[string]discoveryUnit, affinities map[string]affinity, edgeCounts map[string]int, totalEdges int) BoundaryCandidate {
	c := BoundaryCandidate{ClusterCount: len(clusters)}
	internalEdges, affinitySum, affinityPairs := 0, 0.0, 0
	for _, cl := range clusters {
		b := SuggestedBoundary{SuggestedName: suggestedName(cl.units), Units: append([]string{}, cl.units...), Paths: unitPatterns(cl.units)}
		var sums affinity
		pairs := 0
		for _, u := range cl.units {
			b.Files += units[u].files
		}
		for i := 0; i < len(cl.units); i++ {
			for j := i + 1; j < len(cl.units); j++ {
				key := pairKey(cl.units[i], cl.units[j])
				a := affinities[key]
				b.Evidence.InternalDependencyEdges += edgeCounts[key]
				sums.static += a.static
				sums.change += a.change
				sums.vocabulary += a.vocabulary
				sums.combined += a.combined
				if a.combined > 0 {
					affinitySum += a.combined
					affinityPairs++
				}
				pairs++
			}
		}
		if pairs > 0 {
			b.Evidence.StaticAffinity = round(sums.static / float64(pairs))
			b.Evidence.ChangeAffinity = round(sums.change / float64(pairs))
			b.Evidence.VocabularyAffinity = round(sums.vocabulary / float64(pairs))
			b.Evidence.CombinedAffinity = round(sums.combined / float64(pairs))
		}
		internalEdges += b.Evidence.InternalDependencyEdges
		c.Boundaries = append(c.Boundaries, b)
	}
	if totalEdges > 0 {
		c.DependencyCoverage = round(float64(internalEdges) / float64(totalEdges))
	}
	if affinityPairs > 0 {
		c.MeanInternalAffinity = round(affinitySum / float64(affinityPairs))
	}
	meanSize := float64(len(units)) / float64(maxInt(1, len(clusters)))
	variance := 0.0
	for _, cl := range clusters {
		d := float64(len(cl.units)) - meanSize
		variance += d * d
	}
	variance /= float64(maxInt(1, len(clusters)))
	c.Balance = round(1 / (1 + math.Sqrt(variance)/math.Max(1, meanSize)))
	c.Quality = round(0.5*c.DependencyCoverage + 0.3*c.MeanInternalAffinity + 0.2*c.Balance)
	sort.Slice(c.Boundaries, func(i, j int) bool { return c.Boundaries[i].SuggestedName < c.Boundaries[j].SuggestedName })
	return c
}

func unitPatterns(units []string) []string {
	extensions := []string{"*.go", "*.ts", "*.tsx", "*.mts", "*.cts"}
	patterns := make([]string, 0, len(units)*len(extensions))
	for _, unit := range units {
		prefix := unit + "/"
		if unit == "root" {
			prefix = ""
		}
		for _, extension := range extensions {
			patterns = append(patterns, prefix+extension)
		}
	}
	return patterns
}

func clusterAffinity(a, b cluster, affinities map[string]affinity) float64 {
	sum := 0.0
	pairs := 0
	for _, x := range a.units {
		for _, y := range b.units {
			sum += affinities[pairKey(x, y)].combined
			pairs++
		}
	}
	if pairs == 0 {
		return 0
	}
	return sum / float64(pairs)
}
func clusterName(c cluster) string { return strings.Join(c.units, ",") }
func sortClusters(c []cluster) {
	sort.Slice(c, func(i, j int) bool { return clusterName(c[i]) < clusterName(c[j]) })
}
func copyClusters(in []cluster) []cluster {
	out := make([]cluster, len(in))
	for i, c := range in {
		out[i].units = append([]string{}, c.units...)
	}
	return out
}
func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "\x00" + b
}
func directedKey(a, b string) string { return a + "\x00" + b }
func clusterFiles(c cluster, units map[string]discoveryUnit) int {
	total := 0
	for _, u := range c.units {
		total += units[u].files
	}
	return total
}
func splitPair(key string) (string, string) { p := strings.SplitN(key, "\x00", 2); return p[0], p[1] }

func vocabularyAffinity(a, b string) float64 {
	aWords, bWords := words(a), words(b)
	common := 0
	for w := range aWords {
		if bWords[w] {
			common++
		}
	}
	union := len(aWords) + len(bWords) - common
	if union == 0 {
		return 0
	}
	return float64(common) / float64(union)
}
func words(s string) map[string]bool {
	out := map[string]bool{}
	stop := map[string]bool{"internal": true, "pkg": true, "src": true, "cmd": true, "root": true}
	var word []rune
	flush := func() {
		if len(word) > 1 {
			w := strings.ToLower(string(word))
			if !stop[w] {
				out[w] = true
			}
		}
		word = nil
	}
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			word = append(word, r)
		} else {
			flush()
		}
	}
	flush()
	return out
}
func suggestedName(units []string) string {
	if len(units) == 1 {
		return strings.ReplaceAll(filepath.Base(units[0]), "_", "-")
	}
	counts := map[string]int{}
	for _, u := range units {
		for w := range words(u) {
			counts[w]++
		}
	}
	best := ""
	n := 0
	for w, c := range counts {
		if c > n || (c == n && (best == "" || w < best)) {
			best, n = w, c
		}
	}
	if best != "" {
		return best
	}
	return "boundary-" + strconv.Itoa(len(units))
}
