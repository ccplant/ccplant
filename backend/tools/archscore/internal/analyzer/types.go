package analyzer

type Config struct {
	Modules                 []ModuleConfig `json:"modules"`
	MaxFanOut               int            `json:"maxFanOut"`
	MaxCouplingLoad         float64        `json:"maxCouplingLoad"`
	HistoryCommits          int            `json:"historyCommits"`
	MinModuleChanges        int            `json:"minModuleChanges"`
	MinCochange             int            `json:"minCochange"`
	MinLocalityRatio        float64        `json:"minLocalityRatio"`
	Exclude                 []string       `json:"exclude"`
	IncludeTests            bool           `json:"includeTests,omitempty"`
	IncludeGeneratedHistory bool           `json:"includeGeneratedHistory,omitempty"`
	PolicyMode              string         `json:"policyMode,omitempty"`
}

type ModuleConfig struct {
	Name             string   `json:"name"`
	Paths            []string `json:"paths"`
	MayDependOn      []string `json:"mayDependOn"`
	Role             string   `json:"role,omitempty"`
	MaxFanOut        *int     `json:"maxFanOut,omitempty"`
	MaxCouplingLoad  *float64 `json:"maxCouplingLoad,omitempty"`
	PublicPaths      []string `json:"publicPaths,omitempty"`
	MaxPublicSymbols *int     `json:"maxPublicSymbols,omitempty"`
}

type Report struct {
	Version                  string            `json:"version"`
	Score                    int               `json:"score"`
	ScoreLowerBound          int               `json:"scoreLowerBound"`
	ScoreUpperBound          int               `json:"scoreUpperBound"`
	NominalScore             int               `json:"nominalScore"`
	EvaluatedPoints          float64           `json:"evaluatedPoints"`
	Confidence               float64           `json:"confidence"`
	AnalysisCoverage         float64           `json:"analysisCoverage"`
	Gate                     string            `json:"gate"`
	Files                    int               `json:"files"`
	GeneratedFiles           int               `json:"generatedFiles"`
	HistoryGeneratedExcluded int               `json:"historyGeneratedExcluded"`
	Edges                    int               `json:"edges"`
	Components               []ComponentScore  `json:"components"`
	Violations               []Violation       `json:"violations"`
	Cycles                   [][]string        `json:"cycles"`
	CycleDiagnostics         []CycleDiagnostic `json:"cycleDiagnostics,omitempty"`
	Modules                  []ModuleReport    `json:"modules"`
	Unknown                  []string          `json:"unassignedFiles,omitempty"`
	Notes                    []string          `json:"notes,omitempty"`
}

type CycleDiagnostic struct {
	Modules         int                   `json:"modules"`
	DependencyPairs int                   `json:"dependencyPairs"`
	BreakCandidates []CycleBreakCandidate `json:"breakCandidates,omitempty"`
}

type CycleBreakCandidate struct {
	Module                string `json:"module"`
	Role                  string `json:"role,omitempty"`
	Inbound               int    `json:"inbound"`
	Outbound              int    `json:"outbound"`
	RemainingCycleModules int    `json:"remainingCycleModules"`
	Reduction             int    `json:"reduction"`
}

type ComponentScore struct {
	Name            string  `json:"name"`
	Score           int     `json:"score"`
	Max             int     `json:"max"`
	Detail          string  `json:"detail"`
	Confidence      float64 `json:"confidence"`
	EvaluatedPoints float64 `json:"evaluatedPoints"`
}

type Violation struct {
	Kind       string `json:"kind"`
	FromModule string `json:"fromModule"`
	ToModule   string `json:"toModule"`
	FromFile   string `json:"fromFile"`
	ToFile     string `json:"toFile,omitempty"`
	Line       int    `json:"line"`
	Import     string `json:"import"`
}

type ModuleReport struct {
	Name                     string   `json:"name"`
	Files                    int      `json:"files"`
	Units                    int      `json:"units"`
	FanIn                    int      `json:"fanIn"`
	FanOut                   int      `json:"fanOut"`
	CouplingLoad             float64  `json:"couplingLoad"`
	Instability              float64  `json:"instability"`
	InternalEdges            int      `json:"internalEdges"`
	ExternalEdges            int      `json:"externalEdges"`
	Cohesion                 float64  `json:"cohesion"`
	CohesionEvaluated        bool     `json:"cohesionEvaluated"`
	ChangePartners           []string `json:"changePartners,omitempty"`
	ChangeCoupling           float64  `json:"changeCoupling"`
	ChangeCommits            int      `json:"changeCommits"`
	ChangeEvaluated          bool     `json:"changeEvaluated"`
	ContainedChanges         int      `json:"containedChanges"`
	ChangeLocality           float64  `json:"changeLocality"`
	PublicFiles              int      `json:"publicFiles"`
	PublicUnits              int      `json:"publicUnits"`
	PublicSymbols            int      `json:"publicSymbols"`
	PublicInboundEdges       int      `json:"publicInboundEdges"`
	PrivateInboundViolations int      `json:"privateInboundViolations"`
	Warnings                 []string `json:"warnings,omitempty"`
}

type sourceFile struct {
	Path      string
	Module    string
	Imports   []sourceImport
	Exports   int
	Generated bool
}

type sourceImport struct {
	Path string
	Line int
}

type edge struct {
	FromFile, ToFile     string
	FromModule, ToModule string
	Import               string
	Line                 int
}

type DiscoverConfig struct {
	MinClusters             int      `json:"minClusters"`
	MaxClusters             int      `json:"maxClusters"`
	HistoryCommits          int      `json:"historyCommits"`
	MinCochange             int      `json:"minCochange"`
	MaxBoundarySize         int      `json:"maxBoundarySize"`
	MaxBoundaryFiles        int      `json:"maxBoundaryFiles"`
	Exclude                 []string `json:"exclude"`
	DisableAutoScale        bool     `json:"disableAutoScale"`
	IncludeGeneratedHistory bool     `json:"includeGeneratedHistory"`
}

type DiscoveryReport struct {
	Version         string              `json:"version"`
	Units           int                 `json:"units"`
	Files           int                 `json:"files"`
	GeneratedFiles  int                 `json:"generatedFiles"`
	DependencyEdges int                 `json:"dependencyEdges"`
	HistoryCommits  int                 `json:"historyCommits"`
	Weights         DiscoveryWeights    `json:"weights"`
	Candidates      []BoundaryCandidate `json:"candidates"`
	Notes           []string            `json:"notes,omitempty"`
}

type DiscoveryWeights struct {
	Static     float64 `json:"staticDependency"`
	Change     float64 `json:"changeCoupling"`
	Vocabulary float64 `json:"pathVocabulary"`
}

type BoundaryCandidate struct {
	ClusterCount         int                 `json:"clusterCount"`
	Quality              float64             `json:"quality"`
	DependencyCoverage   float64             `json:"dependencyCoverage"`
	MeanInternalAffinity float64             `json:"meanInternalAffinity"`
	Balance              float64             `json:"balance"`
	Boundaries           []SuggestedBoundary `json:"boundaries"`
}

type SuggestedBoundary struct {
	SuggestedName string           `json:"suggestedName"`
	Units         []string         `json:"units"`
	Paths         []string         `json:"paths"`
	Files         int              `json:"files"`
	Evidence      BoundaryEvidence `json:"evidence"`
}

type BoundaryEvidence struct {
	InternalDependencyEdges int     `json:"internalDependencyEdges"`
	StaticAffinity          float64 `json:"staticAffinity"`
	ChangeAffinity          float64 `json:"changeAffinity"`
	VocabularyAffinity      float64 `json:"vocabularyAffinity"`
	CombinedAffinity        float64 `json:"combinedAffinity"`
}

type ComparisonReport struct {
	BaselineScore      int          `json:"baselineScore"`
	CurrentScore       int          `json:"currentScore"`
	ScoreDelta         int          `json:"scoreDelta"`
	BaselineConfidence float64      `json:"baselineConfidence"`
	CurrentConfidence  float64      `json:"currentConfidence"`
	Regressions        []Regression `json:"regressions"`
	Improvements       []Regression `json:"improvements,omitempty"`
	Passed             bool         `json:"passed"`
}

type Regression struct {
	Kind   string `json:"kind"`
	Detail string `json:"detail"`
}
