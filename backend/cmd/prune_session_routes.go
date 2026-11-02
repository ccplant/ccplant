package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/repositories"
)

var (
	orphanedRouteNamespace string
	orphanedWorkloadNS     string
	orphanedKubeconfig     string
	orphanedKubeContext    string
	orphanedRouteDryRun    bool
	orphanedRouteMinAge    time.Duration
	orphanedRouteVerbose   bool
)

type sessionRouteSecretData struct {
	SessionID       string `json:"session_id"`
	RemoteSessionID string `json:"remote_session_id"`
	ManagerID       string `json:"manager_id"`
}

type orphanedSessionRoute struct {
	secretName      string
	sessionID       string
	remoteSessionID string
}

var pruneOrphanedSessionRoutesCmd = &cobra.Command{
	Use:   "prune-orphaned-session-routes",
	Short: "Find and delete local session routes whose workloads no longer exist",
	Long: `Find orphaned agentapi-session-route Secrets.

Only local alias routes are eligible: manager_id must be empty and
remote_session_id must be present. A route is orphaned when neither the
Service, Deployment, nor a live standalone Pod for its remote session exists
and the route Secret is at least --min-age old. The Service check preserves
valid suspended PVC sessions, which intentionally have no workload. External-
manager routes and pending routes are skipped because their liveness cannot be
determined from local Kubernetes resources.

To inspect an external session-manager cluster, select it with --kubeconfig
and/or --context. The command then checks that cluster's local alias routes.
Use --workload-namespace when its route Secrets and workloads are stored in
different namespaces.

The command is a dry-run by default. Pass --dry-run=false to delete the
orphaned route Secrets.

Examples:
  agentapi-proxy helpers prune-orphaned-session-routes --namespace agentapi-ui
  agentapi-proxy helpers prune-orphaned-session-routes --kubeconfig ~/.kube/external-manager --context manager-a --namespace agentapi-ui --workload-namespace agentapi-runtimes
  agentapi-proxy helpers prune-orphaned-session-routes --namespace agentapi-ui --dry-run=false`,
	Args: cobra.NoArgs,
	RunE: runPruneOrphanedSessionRoutes,
}

func init() {
	flags := pruneOrphanedSessionRoutesCmd.Flags()
	flags.StringVar(&orphanedRouteNamespace, "namespace", "agentapi-ui", "Namespace containing session route Secrets")
	flags.StringVar(&orphanedWorkloadNS, "workload-namespace", "", "Namespace containing session workloads (defaults to --namespace)")
	flags.StringVar(&orphanedKubeconfig, "kubeconfig", "", "Path to the kubeconfig for the target cluster")
	flags.StringVar(&orphanedKubeContext, "context", "", "Kubeconfig context for the target cluster")
	flags.BoolVar(&orphanedRouteDryRun, "dry-run", true, "Show what would be deleted without deleting it")
	flags.DurationVar(&orphanedRouteMinAge, "min-age", 10*time.Minute, "Minimum route Secret age before it can be pruned")
	flags.BoolVarP(&orphanedRouteVerbose, "verbose", "v", false, "Show skipped routes")
	HelpersCmd.AddCommand(pruneOrphanedSessionRoutesCmd)
}

func findOrphanedSessionRoutes(ctx context.Context, client kubernetes.Interface, routeNamespace, workloadNamespace string, minAge time.Duration, now time.Time) ([]orphanedSessionRoute, error) {
	secrets, err := client.CoreV1().Secrets(routeNamespace).List(ctx, metav1.ListOptions{
		LabelSelector: repositories.LabelSessionRoute + "=true",
	})
	if err != nil {
		return nil, fmt.Errorf("list session route Secrets: %w", err)
	}

	orphans := make([]orphanedSessionRoute, 0)
	for i := range secrets.Items {
		secret := &secrets.Items[i]
		raw, ok := secret.Data[repositories.SessionRouteSecretKey]
		if !ok {
			if orphanedRouteVerbose {
				fmt.Printf("[SKIP] Secret %s: missing %s\n", secret.Name, repositories.SessionRouteSecretKey)
			}
			continue
		}

		var route sessionRouteSecretData
		if err := json.Unmarshal(raw, &route); err != nil {
			return nil, fmt.Errorf("decode session route Secret %s: %w", secret.Name, err)
		}
		if route.ManagerID != "" || route.RemoteSessionID == "" {
			if orphanedRouteVerbose {
				fmt.Printf("[SKIP] Secret %s: route is external or pending\n", secret.Name)
			}
			continue
		}
		if minAge > 0 && !secret.CreationTimestamp.IsZero() && now.Sub(secret.CreationTimestamp.Time) < minAge {
			if orphanedRouteVerbose {
				fmt.Printf("[SKIP] Secret %s: newer than %s\n", secret.Name, minAge)
			}
			continue
		}

		serviceExists, err := serviceExistsForSession(ctx, client, workloadNamespace, route.RemoteSessionID)
		if err != nil {
			return nil, fmt.Errorf("check Service for route %s: %w", route.SessionID, err)
		}
		if serviceExists {
			if orphanedRouteVerbose {
				fmt.Printf("[ACTIVE] route %s: Service for session %s exists\n", route.SessionID, route.RemoteSessionID)
			}
			continue
		}

		workloadName := "agentapi-session-" + route.RemoteSessionID
		exists, err := sessionWorkloadExists(ctx, client, workloadNamespace, workloadName)
		if err != nil {
			return nil, fmt.Errorf("check workload for route %s: %w", route.SessionID, err)
		}
		if exists {
			if orphanedRouteVerbose {
				fmt.Printf("[ACTIVE] route %s: workload %s exists\n", route.SessionID, workloadName)
			}
			continue
		}

		orphans = append(orphans, orphanedSessionRoute{
			secretName:      secret.Name,
			sessionID:       route.SessionID,
			remoteSessionID: route.RemoteSessionID,
		})
	}
	return orphans, nil
}

func pruneSessionRoutes(ctx context.Context, client kubernetes.Interface, namespace string, orphans []orphanedSessionRoute, dryRun bool) (int, error) {
	deleted := 0
	for _, orphan := range orphans {
		if dryRun {
			fmt.Printf("[DRY-RUN] Would delete Secret %s (session=%s, missing workload session=%s)\n", orphan.secretName, orphan.sessionID, orphan.remoteSessionID)
			continue
		}
		if err := client.CoreV1().Secrets(namespace).Delete(ctx, orphan.secretName, metav1.DeleteOptions{}); err != nil {
			if errors.IsNotFound(err) {
				continue
			}
			return deleted, fmt.Errorf("delete session route Secret %s: %w", orphan.secretName, err)
		}
		deleted++
		fmt.Printf("[DELETED] Secret %s (session=%s)\n", orphan.secretName, orphan.sessionID)
	}
	return deleted, nil
}

func runPruneOrphanedSessionRoutes(cmd *cobra.Command, _ []string) error {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if orphanedKubeconfig != "" {
		loadingRules.ExplicitPath = orphanedKubeconfig
	}
	overrides := &clientcmd.ConfigOverrides{CurrentContext: orphanedKubeContext}
	restConfig, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, overrides).ClientConfig()
	if err != nil {
		return fmt.Errorf("get Kubernetes config: %w", err)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("create Kubernetes client: %w", err)
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	workloadNamespace := orphanedWorkloadNS
	if workloadNamespace == "" {
		workloadNamespace = orphanedRouteNamespace
	}
	orphans, err := findOrphanedSessionRoutes(ctx, client, orphanedRouteNamespace, workloadNamespace, orphanedRouteMinAge, time.Now())
	if err != nil {
		return err
	}
	if len(orphans) == 0 {
		fmt.Println("No orphaned session routes found.")
		return nil
	}

	deleted, err := pruneSessionRoutes(ctx, client, orphanedRouteNamespace, orphans, orphanedRouteDryRun)
	if err != nil {
		return err
	}
	if orphanedRouteDryRun {
		fmt.Printf("Dry-run complete: %d orphaned session route(s) found. Run with --dry-run=false to delete them.\n", len(orphans))
	} else {
		fmt.Printf("Pruning complete: %d orphaned session route(s) deleted.\n", deleted)
	}
	return nil
}
