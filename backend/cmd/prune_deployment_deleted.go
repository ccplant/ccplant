package cmd

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
)

// prune-orphaned-resources command flags
var (
	orphanedNamespace string
	orphanedDryRun    bool
	orphanedVerbose   bool
	orphanedMinAge    time.Duration
)

func sessionWorkloadExists(ctx context.Context, client kubernetes.Interface, namespace, name string) (bool, error) {
	deployment, depErr := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if depErr == nil {
		return deployment.DeletionTimestamp == nil, nil
	}
	if !errors.IsNotFound(depErr) {
		return false, depErr
	}

	pod, podErr := client.CoreV1().Pods(namespace).Get(ctx, name, metav1.GetOptions{})
	if podErr == nil {
		if pod.DeletionTimestamp != nil {
			return false, nil
		}
		if pod.Status.Phase == corev1.PodFailed || pod.Status.Phase == corev1.PodSucceeded {
			return false, nil
		}
		return true, nil
	}
	if !errors.IsNotFound(podErr) {
		return false, podErr
	}

	return false, nil
}

var pruneOrphanedResourcesCmd = &cobra.Command{
	Use:   "prune-orphaned-resources",
	Short: "Delete stale Kubernetes resources left behind after a session workload was deleted",
	Long: `Delete stale Kubernetes resources for agentapi sessions whose workload no longer exists.

A session is considered orphaned when its Deployment or Pod (agentapi-session-{id})
no longer exists but other associated resources are still present.
This can happen when a workload is deleted manually or an incomplete cleanup
leaves residual objects.

Detection uses complementary passes so that every stale pattern is covered:

  Pass 1 – Service scan:
    Sessions whose Service (agentapi-session-{id}-svc) still exists but
    whose workload is gone are considered orphaned.

  Pass 2 – Settings Secret scan:
    Sessions whose settings Secret (agentapi-session-{id}-settings) still
    exists but whose workload is gone are considered orphaned.
    This catches the case where the Service was already cleaned up but the
    settings Secret was left behind.

  Pass 3 – Provision Request Secret scan:
    Sessions whose provision request Secret (agentapi-provision-request-{id})
    still exists, but whose Service and settings Secret are gone and whose
    workload is gone or terminal, are considered orphaned. This catches
    interrupted creates and partial cleanups that happened before settings
    Secret creation.

  Pass 4 – Workload scan:
    Sessions whose Deployment or standalone Pod still exists but whose Service
    is gone are considered orphaned. Active workloads are only pruned after
    --min-age to avoid racing a normal in-progress create.

Results from all passes are merged and deduplicated before deletion.

The following resources are deleted for each orphaned session:
  - Deployment  agentapi-session-{id}                     (may already be gone)
  - Pod         agentapi-session-{id}                     (may already be gone)
  - Service     agentapi-session-{id}-svc                 (if present)
  - PVC         agentapi-session-{id}-pvc                 (if present)
  - Secret      agentapi-session-{id}-settings             (if present)
  - Secret      agentapi-session-{id}-svc-webhook-payload  (if present)
  - Secret      agentapi-session-{id}-svc-oneshot-settings (if present)
  - Secret      agentapi-provision-request-{id}            (if present)

Use --dry-run to preview what would be deleted without making any changes.
Use --min-age to control how old an active workload without a Service must be
before it is considered orphaned. Terminal or deleting workloads do not wait.

Examples:
  # Preview orphaned sessions (no changes)
  agentapi-proxy helpers prune-orphaned-resources --namespace agentapi-ui --dry-run

  # Delete orphaned sessions
  agentapi-proxy helpers prune-orphaned-resources --namespace agentapi-ui

  # Delete with verbose output
  agentapi-proxy helpers prune-orphaned-resources --namespace agentapi-ui --verbose`,
	RunE: runPruneOrphanedResources,
}

func init() {
	pruneOrphanedResourcesCmd.Flags().StringVar(&orphanedNamespace, "namespace", "agentapi-ui",
		"Kubernetes namespace to operate in")
	pruneOrphanedResourcesCmd.Flags().BoolVar(&orphanedDryRun, "dry-run", false,
		"Show what would be deleted without actually deleting")
	pruneOrphanedResourcesCmd.Flags().BoolVarP(&orphanedVerbose, "verbose", "v", false,
		"Verbose output")
	pruneOrphanedResourcesCmd.Flags().DurationVar(&orphanedMinAge, "min-age", 10*time.Minute,
		"Minimum age before pruning active workloads that have no Service")

	HelpersCmd.AddCommand(pruneOrphanedResourcesCmd)
}

// orphanedSession holds the minimum information needed to delete all residual
// resources for a single session.
type orphanedSession struct {
	id     string
	source string // for logging only
}

func resourceExists(get func() error) (bool, error) {
	err := get()
	if err == nil {
		return true, nil
	}
	if errors.IsNotFound(err) {
		return false, nil
	}
	return false, err
}

func oldEnoughToPrune(obj metav1.Object) bool {
	if orphanedMinAge <= 0 {
		return true
	}
	createdAt := obj.GetCreationTimestamp()
	if createdAt.IsZero() {
		return true
	}
	return time.Since(createdAt.Time) >= orphanedMinAge
}

func serviceExistsForSession(ctx context.Context, client kubernetes.Interface, namespace, sessionID string) (bool, error) {
	svcName := fmt.Sprintf("agentapi-session-%s-svc", sessionID)
	return resourceExists(func() error {
		_, err := client.CoreV1().Services(namespace).Get(ctx, svcName, metav1.GetOptions{})
		return err
	})
}

func deploymentWorkloadActive(deployment appsv1.Deployment) bool {
	return deployment.DeletionTimestamp == nil
}

func podWorkloadActive(pod corev1.Pod) bool {
	if pod.DeletionTimestamp != nil {
		return false
	}
	return pod.Status.Phase != corev1.PodFailed && pod.Status.Phase != corev1.PodSucceeded
}

func runPruneOrphanedResources(_ *cobra.Command, _ []string) error {
	restConfig, err := ctrl.GetConfig()
	if err != nil {
		return fmt.Errorf("failed to get Kubernetes config: %w", err)
	}
	client, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return fmt.Errorf("failed to create Kubernetes client: %w", err)
	}
	if orphanedDryRun {
		fmt.Printf("[DRY-RUN] Scanning namespace: %s\n", orphanedNamespace)
	} else {
		fmt.Printf("Scanning namespace: %s\n", orphanedNamespace)
	}
	ctx := context.Background()
	orphaned, err := newOrphanScanner(ctx, client, orphanedNamespace).scan()
	if err != nil {
		return err
	}
	return pruneOrphanedSessions(ctx, client, orphanedNamespace, orphaned)
}
