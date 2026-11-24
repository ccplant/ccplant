package cmd

import (
	"context"
	"fmt"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type orphanResource struct {
	kind   string
	name   string
	delete func() error
}

func resourcesForOrphan(ctx context.Context, client kubernetes.Interface, namespace, sessionID string) []orphanResource {
	workload := fmt.Sprintf("agentapi-session-%s", sessionID)
	service := workload + "-svc"
	names := struct{ pvc, settings, webhook, oneshot, provision string }{
		pvc: workload + "-pvc", settings: workload + "-settings",
		webhook: service + "-webhook-payload", oneshot: service + "-oneshot-settings",
		provision: fmt.Sprintf("agentapi-provision-request-%s", sessionID),
	}
	return []orphanResource{
		{kind: "Deployment", name: workload, delete: func() error {
			return client.AppsV1().Deployments(namespace).Delete(ctx, workload, metav1.DeleteOptions{})
		}},
		{kind: "Pod", name: workload, delete: func() error { return client.CoreV1().Pods(namespace).Delete(ctx, workload, metav1.DeleteOptions{}) }},
		{kind: "Service", name: service, delete: func() error { return client.CoreV1().Services(namespace).Delete(ctx, service, metav1.DeleteOptions{}) }},
		{kind: "PVC", name: names.pvc, delete: func() error {
			return client.CoreV1().PersistentVolumeClaims(namespace).Delete(ctx, names.pvc, metav1.DeleteOptions{})
		}},
		{kind: "Secret (settings)", name: names.settings, delete: func() error {
			return client.CoreV1().Secrets(namespace).Delete(ctx, names.settings, metav1.DeleteOptions{})
		}},
		{kind: "Secret (webhook-payload)", name: names.webhook, delete: func() error {
			return client.CoreV1().Secrets(namespace).Delete(ctx, names.webhook, metav1.DeleteOptions{})
		}},
		{kind: "Secret (oneshot-settings)", name: names.oneshot, delete: func() error {
			return client.CoreV1().Secrets(namespace).Delete(ctx, names.oneshot, metav1.DeleteOptions{})
		}},
		{kind: "Secret (provision-request)", name: names.provision, delete: func() error {
			return client.CoreV1().Secrets(namespace).Delete(ctx, names.provision, metav1.DeleteOptions{})
		}},
	}
}

func deleteOrphanResource(resource orphanResource) (deleted, failed int) {
	if orphanedDryRun {
		fmt.Printf("    [DRY-RUN] Would delete %s: %s\n", resource.kind, resource.name)
		return 1, 0
	}
	if err := resource.delete(); err != nil {
		if apierrors.IsNotFound(err) {
			if orphanedVerbose {
				fmt.Printf("    [SKIP] %s %s: not found\n", resource.kind, resource.name)
			}
			return 0, 0
		}
		fmt.Printf("    [ERROR] Failed to delete %s %s: %v\n", resource.kind, resource.name, err)
		return 0, 1
	}
	fmt.Printf("    [DELETED] %s: %s\n", resource.kind, resource.name)
	return 1, 0
}

func pruneOrphanedSessions(ctx context.Context, client kubernetes.Interface, namespace string, sessions []orphanedSession) error {
	if len(sessions) == 0 {
		fmt.Println("\nNo orphaned sessions found.")
		return nil
	}
	fmt.Printf("\nFound %d orphaned session(s) in total.\n", len(sessions))
	if orphanedDryRun {
		fmt.Println("[DRY-RUN] The following resources would be deleted:")
	} else {
		fmt.Println("Deleting orphaned resources...")
	}
	deleted, failed := 0, 0
	for _, session := range sessions {
		fmt.Printf("\n  Session: %s (detected via %s)\n", session.id, session.source)
		for _, resource := range resourcesForOrphan(ctx, client, namespace, session.id) {
			count, errors := deleteOrphanResource(resource)
			deleted += count
			failed += errors
		}
	}
	fmt.Println()
	if orphanedDryRun {
		fmt.Printf("Dry-run complete: %d resource(s) would be deleted across %d orphaned session(s).\n", deleted, len(sessions))
		fmt.Println("Run without --dry-run to actually delete them.")
		return nil
	}
	fmt.Printf("Pruning complete: %d resource(s) deleted across %d orphaned session(s).\n", deleted, len(sessions))
	if failed > 0 {
		fmt.Printf("Errors encountered: %d\n", failed)
		return fmt.Errorf("%d deletion error(s) occurred", failed)
	}
	return nil
}
