package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime/pkg/client/config"
)

var exactSemver = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?$`)

type helmMigratePlanOptions struct {
	namespace       string
	backendRelease  string
	frontendRelease string
	targetRelease   string
	chart           string
	version         string
	valuesOut       string
	output          string
}

type migrationFinding struct {
	Level   string `json:"level" yaml:"level"`
	Subject string `json:"subject" yaml:"subject"`
	Message string `json:"message" yaml:"message"`
}

type migrationInventory struct {
	Sessions       int `json:"sessions" yaml:"sessions"`
	LegacySessions int `json:"legacySessions" yaml:"legacySessions"`
	RuntimeSecrets int `json:"runtimeSecrets" yaml:"runtimeSecrets"`
	SessionPVCs    int `json:"sessionPVCs" yaml:"sessionPVCs"`
}

type migrationPlan struct {
	Ready             bool               `json:"ready" yaml:"ready"`
	Namespace         string             `json:"namespace" yaml:"namespace"`
	BackendRelease    string             `json:"backendRelease" yaml:"backendRelease"`
	FrontendRelease   string             `json:"frontendRelease" yaml:"frontendRelease"`
	TargetRelease     string             `json:"targetRelease" yaml:"targetRelease"`
	Chart             string             `json:"chart" yaml:"chart"`
	Version           string             `json:"version" yaml:"version"`
	ValuesFile        string             `json:"valuesFile" yaml:"valuesFile"`
	Inventory         migrationInventory `json:"inventory" yaml:"inventory"`
	SecretFingerprint string             `json:"secretFingerprint" yaml:"secretFingerprint"`
	Findings          []migrationFinding `json:"findings" yaml:"findings"`
	Commands          []string           `json:"commands" yaml:"commands"`
}

type migrationVerify struct {
	OK            bool                   `json:"ok" yaml:"ok"`
	Phase         string                 `json:"phase" yaml:"phase"`
	Namespace     string                 `json:"namespace" yaml:"namespace"`
	TargetRelease string                 `json:"targetRelease" yaml:"targetRelease"`
	Inventory     migrationInventory     `json:"inventory" yaml:"inventory"`
	Findings      []migrationFinding     `json:"findings" yaml:"findings"`
	LeaderLeases  []migrationLeaderLease `json:"leaderLeases,omitempty" yaml:"leaderLeases,omitempty"`
}

type migrationLeaderLease struct {
	Worker string `json:"worker" yaml:"worker"`
	Name   string `json:"name" yaml:"name"`
	Holder string `json:"holder,omitempty" yaml:"holder,omitempty"`
}

type serviceProbe func(context.Context, string, string) error

func newHelmMigrateCommand() *cobra.Command {
	o := &helmMigratePlanOptions{}
	root := &cobra.Command{
		Use:   "migrate",
		Short: "Preflight migration from split releases to the ccplant chart",
		Long: `Inspect a split agentapi-proxy/agentapi-ui installation before migration.

This command is deliberately read-only with respect to the Kubernetes cluster.
It validates retained resources and generates shadow-install values and commands;
it never installs, patches, switches traffic, or uninstalls a release.`,
	}
	f := root.PersistentFlags()
	f.StringVarP(&o.namespace, "namespace", "n", "default", "Kubernetes namespace")
	f.StringVar(&o.backendRelease, "backend-release", "agentapi-proxy", "existing backend Helm release")
	f.StringVar(&o.frontendRelease, "frontend-release", "agentapi-ui", "existing frontend Helm release")
	f.StringVar(&o.targetRelease, "target-release", "ccplant", "target ccplant Helm release")
	f.StringVar(&o.chart, "chart", "oci://ghcr.io/ccplant/charts/ccplant", "target chart reference")
	f.StringVar(&o.version, "version", "", "exact target chart version")
	f.StringVar(&o.valuesOut, "values-out", "ccplant-shadow-values.yaml", "generated shadow-install values file")
	f.StringVarP(&o.output, "output", "o", "text", "output format: text, json, or yaml")
	root.AddCommand(&cobra.Command{
		Use: "plan", Short: "Run migration preflight and generate shadow values", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateHelmMigratePlanOptions(o); err != nil {
				return err
			}
			config, err := ctrl.GetConfig()
			if err != nil {
				return fmt.Errorf("get Kubernetes config: %w", err)
			}
			client, err := kubernetes.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("create Kubernetes client: %w", err)
			}
			return runHelmMigratePlan(cmd.Context(), cmd.OutOrStdout(), client, o)
		},
	})
	root.AddCommand(&cobra.Command{
		Use: "verify", Short: "Verify a running shadow or cut-over ccplant release", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if err := validateHelmMigrateVerifyOptions(o); err != nil {
				return err
			}
			config, err := ctrl.GetConfig()
			if err != nil {
				return fmt.Errorf("get Kubernetes config: %w", err)
			}
			client, err := kubernetes.NewForConfig(config)
			if err != nil {
				return fmt.Errorf("create Kubernetes client: %w", err)
			}
			probe := func(ctx context.Context, service, path string) error {
				_, probeErr := client.CoreV1().Services(o.namespace).ProxyGet("http", service, "http", path, nil).DoRaw(ctx)
				return probeErr
			}
			verification := migrationVerificationContext{out: cmd.OutOrStdout(), client: client, options: o, probe: probe}
			return runHelmMigrateVerify(cmd.Context(), verification)
		},
	})
	return root
}

func validateHelmMigratePlanOptions(o *helmMigratePlanOptions) error {
	if strings.TrimSpace(o.namespace) == "" {
		return errors.New("namespace must not be empty")
	}
	if o.backendRelease == "" || o.frontendRelease == "" || o.targetRelease == "" {
		return errors.New("release names must not be empty")
	}
	if o.targetRelease == o.backendRelease || o.targetRelease == o.frontendRelease {
		return errors.New("target release must differ from both source releases")
	}
	if !exactSemver.MatchString(o.version) {
		return errors.New("--version must be an exact semantic version, for example 0.3.2")
	}
	if o.output != "text" && o.output != "json" && o.output != "yaml" {
		return fmt.Errorf("unsupported output format %q", o.output)
	}
	return nil
}

func validateHelmMigrateVerifyOptions(o *helmMigratePlanOptions) error {
	if strings.TrimSpace(o.namespace) == "" {
		return errors.New("namespace must not be empty")
	}
	if o.backendRelease == "" || o.targetRelease == "" {
		return errors.New("backend and target release names must not be empty")
	}
	if o.backendRelease == o.targetRelease {
		return errors.New("target release must differ from the source backend release")
	}
	if o.output != "text" && o.output != "json" && o.output != "yaml" {
		return fmt.Errorf("unsupported output format %q", o.output)
	}
	return nil
}

func runHelmMigratePlan(ctx context.Context, out io.Writer, client kubernetes.Interface, o *helmMigratePlanOptions) error {
	backend, err := loadMigrationRelease(ctx, client, o.namespace, o.backendRelease)
	if err != nil {
		return err
	}
	frontend, err := loadMigrationRelease(ctx, client, o.namespace, o.frontendRelease)
	if err != nil {
		return err
	}

	values := buildMigrationShadowValues(backend.Config, frontend.Config)
	valuesData, err := yaml.Marshal(values)
	if err != nil {
		return fmt.Errorf("encode shadow values: %w", err)
	}
	if err := os.WriteFile(o.valuesOut, valuesData, 0o600); err != nil {
		return fmt.Errorf("write shadow values: %w", err)
	}

	plan := migrationPlan{
		Namespace: o.namespace, BackendRelease: o.backendRelease, FrontendRelease: o.frontendRelease,
		TargetRelease: o.targetRelease, Chart: o.chart, Version: o.version, ValuesFile: o.valuesOut,
	}
	addFinding(&plan, "pass", "backend release", fmt.Sprintf("revision %d is %s", backend.Version, backend.Info.Status))
	addFinding(&plan, "pass", "frontend release", fmt.Sprintf("revision %d is %s", frontend.Version, frontend.Info.Status))
	if backend.Info.Status != "deployed" {
		addFinding(&plan, "block", "backend release", fmt.Sprintf("status is %q, expected deployed", backend.Info.Status))
	}
	if frontend.Info.Status != "deployed" {
		addFinding(&plan, "block", "frontend release", fmt.Sprintf("status is %q, expected deployed", frontend.Info.Status))
	}

	if _, targetErr := loadMigrationRelease(ctx, client, o.namespace, o.targetRelease); targetErr == nil {
		addFinding(&plan, "block", "target release", fmt.Sprintf("%s already exists", o.targetRelease))
	} else if !strings.Contains(targetErr.Error(), "was not found") {
		return targetErr
	} else {
		addFinding(&plan, "pass", "target release", fmt.Sprintf("%s is available", o.targetRelease))
	}

	inspectControlService(ctx, client, o, &plan)
	inspectSharedRBAC(ctx, client, o, &plan)
	if err := inspectSecretReferences(ctx, secretReferenceInspection{client: client, namespace: o.namespace, values: backend.Config, prefix: "backend", plan: &plan}); err != nil {
		return err
	}
	if err := inspectSecretReferences(ctx, secretReferenceInspection{client: client, namespace: o.namespace, values: frontend.Config, prefix: "frontend", plan: &plan}); err != nil {
		return err
	}
	if err := inspectRuntimeResources(ctx, client, o, &plan); err != nil {
		return err
	}

	plan.SecretFingerprint = referencedSecretsFingerprint(ctx, client, o.namespace, append(discoverSecretReferences(backend.Config), discoverSecretReferences(frontend.Config)...))
	plan.Ready = !hasBlockingFinding(plan.Findings)
	plan.Commands = migrationCommands(o, plan.Inventory.LegacySessions)
	if err := writeMigrationPlan(out, o.output, plan); err != nil {
		return err
	}
	if !plan.Ready {
		return errors.New("migration preflight found blocking problems")
	}
	return nil
}

func loadMigrationRelease(ctx context.Context, client kubernetes.Interface, namespace, name string) (*helmStoredRelease, error) {
	release, secretName, err := loadLatestHelmRelease(ctx, client, namespace, name)
	if err != nil {
		return nil, err
	}
	if release == nil || secretName == "" {
		return nil, fmt.Errorf("Helm release %s/%s returned incomplete storage metadata", namespace, name)
	}
	return release, nil
}

type migrationVerificationContext struct {
	out     io.Writer
	client  kubernetes.Interface
	options *helmMigratePlanOptions
	probe   serviceProbe
	verify  *migrationVerify
}

func runHelmMigrateVerify(ctx context.Context, inspection migrationVerificationContext) error {
	o := inspection.options
	target, err := loadMigrationRelease(ctx, inspection.client, o.namespace, o.targetRelease)
	if err != nil {
		return err
	}
	verify := migrationVerify{Namespace: o.namespace, TargetRelease: o.targetRelease, Phase: "shadow"}
	inspection.verify = &verify
	if target.Info.Status != "deployed" {
		addVerifyFinding(&verify, "block", "target release", fmt.Sprintf("status is %q, expected deployed", target.Info.Status))
	} else {
		addVerifyFinding(&verify, "pass", "target release", fmt.Sprintf("revision %d is deployed", target.Version))
	}

	backendService := o.targetRelease + "-backend"
	frontendService := o.targetRelease + "-frontend"
	inspection.inspectShadowWorkload(ctx, backendService, "backend")
	inspection.inspectShadowWorkload(ctx, frontendService, "frontend")
	if err := inspection.probe(ctx, backendService, "/health"); err != nil {
		addVerifyFinding(&verify, "block", "backend API probe", err.Error())
	} else {
		addVerifyFinding(&verify, "pass", "backend API probe", backendService+"/health returned successfully through the Kubernetes API proxy")
	}

	control, controlErr := inspection.client.CoreV1().Services(o.namespace).Get(ctx, "control", metav1.GetOptions{})
	oldSelector := map[string]string{"app.kubernetes.io/name": "agentapi-proxy", "app.kubernetes.io/instance": o.backendRelease, "app.kubernetes.io/component": "proxy"}
	targetSelector := map[string]string{"app.kubernetes.io/name": "backend", "app.kubernetes.io/instance": o.targetRelease, "app.kubernetes.io/component": "proxy"}
	if controlErr != nil {
		addVerifyFinding(&verify, "block", "control Service", controlErr.Error())
	} else if selectorContains(control.Spec.Selector, targetSelector) {
		verify.Phase = "cutover"
		addVerifyFinding(&verify, "pass", "control routing", "points to the target backend")
	} else if selectorContains(control.Spec.Selector, oldSelector) {
		addVerifyFinding(&verify, "pass", "control routing", "still points to the source backend (shadow phase)")
	} else {
		addVerifyFinding(&verify, "block", "control routing", "selector points to neither the source nor target backend: "+formatStringMap(control.Spec.Selector))
	}

	inspection.inspectWorkerLeaderElection(ctx, target.Config)
	if err := inspectVerifyRuntime(ctx, inspection.client, o, &verify); err != nil {
		return err
	}
	verify.OK = !hasBlockingFinding(verify.Findings)
	if err := writeMigrationVerify(inspection.out, o.output, verify); err != nil {
		return err
	}
	if !verify.OK {
		return errors.New("shadow migration verification failed")
	}
	return nil
}

func (inspection migrationVerificationContext) inspectShadowWorkload(ctx context.Context, name, component string) {
	inspection.inspectShadowDeployment(ctx, name, component)
	endpointSlices, err := inspection.client.DiscoveryV1().EndpointSlices(inspection.options.namespace).List(ctx, metav1.ListOptions{LabelSelector: "kubernetes.io/service-name=" + name})
	if err != nil {
		addVerifyFinding(inspection.verify, "block", component+" Service endpoints", err.Error())
		return
	}
	ready := 0
	for _, endpointSlice := range endpointSlices.Items {
		for _, endpoint := range endpointSlice.Endpoints {
			if endpoint.Conditions.Ready == nil || *endpoint.Conditions.Ready {
				ready += len(endpoint.Addresses)
			}
		}
	}
	if ready == 0 {
		addVerifyFinding(inspection.verify, "block", component+" Service endpoints", "no ready addresses")
	} else {
		addVerifyFinding(inspection.verify, "pass", component+" Service endpoints", fmt.Sprintf("%d ready address(es)", ready))
	}
}

func (inspection migrationVerificationContext) inspectShadowDeployment(ctx context.Context, name, component string) {
	deployment, err := inspection.client.AppsV1().Deployments(inspection.options.namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		addVerifyFinding(inspection.verify, "block", component+" Deployment", err.Error())
	} else {
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		if desired == 0 || deployment.Status.ReadyReplicas < desired {
			addVerifyFinding(inspection.verify, "block", component+" Deployment", fmt.Sprintf("%d/%d replicas are Ready", deployment.Status.ReadyReplicas, desired))
		} else {
			addVerifyFinding(inspection.verify, "pass", component+" Deployment", fmt.Sprintf("%d/%d replicas are Ready", deployment.Status.ReadyReplicas, desired))
		}
	}
}

var migrationWorkerLeases = []struct {
	valuePath string
	worker    string
	lease     string
	always    bool
}{
	{"scheduleWorker", "schedule", schedule.ScheduleWorkerLeaseName, false},
	{"slackbotCleanupWorker", "slackbot cleanup", schedule.SlackbotCleanupWorkerLeaseName, false},
	{"", "session allocator", schedule.SessionAllocatorLeaseName, true},
}

func (inspection migrationVerificationContext) inspectWorkerLeaderElection(ctx context.Context, targetValues map[string]any) {
	backendValues, _ := targetValues["backend"].(map[string]any)
	if backendValues == nil {
		backendValues = targetValues
	}
	for _, worker := range migrationWorkerLeases {
		config, _ := backendValues[worker.valuePath].(map[string]any)
		enabled := worker.always
		if configured, ok := config["enabled"].(bool); ok {
			enabled = configured
		}
		if !enabled {
			continue
		}
		lease, err := inspection.client.CoordinationV1().Leases(inspection.options.namespace).Get(ctx, worker.lease, metav1.GetOptions{})
		if err != nil {
			addVerifyFinding(inspection.verify, "block", worker.worker+" leader election", fmt.Sprintf("shared Lease/%s is missing: %v", worker.lease, err))
			continue
		}
		holder := ""
		if lease.Spec.HolderIdentity != nil {
			holder = *lease.Spec.HolderIdentity
		}
		inspection.verify.LeaderLeases = append(inspection.verify.LeaderLeases, migrationLeaderLease{Worker: worker.worker, Name: worker.lease, Holder: holder})
		if holder == "" {
			addVerifyFinding(inspection.verify, "warn", worker.worker+" leader election", fmt.Sprintf("Lease/%s exists but has no holder", worker.lease))
		} else {
			addVerifyFinding(inspection.verify, "pass", worker.worker+" leader election", fmt.Sprintf("uses shared Lease/%s; current holder is %s", worker.lease, holder))
		}
	}
	addVerifyFinding(inspection.verify, "pass", "leader-election contract", "worker Lease names are release-independent, so source and target contend for the same locks in this namespace")
}
