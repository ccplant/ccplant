package cmd

import (
	"context"
	"fmt"
	"strings"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

func isSensitiveLiteralKey(key string) bool {
	switch strings.ToLower(key) {
	case "clientsecret", "token", "password", "privatekey", "encryptionkey", "apikey":
		return true
	default:
		return false
	}
}

func sensitiveEnvironmentName(name string) bool {
	upper := strings.ToUpper(name)
	return strings.Contains(upper, "TOKEN") || strings.Contains(upper, "SECRET") || strings.Contains(upper, "PASSWORD") || strings.Contains(upper, "PRIVATE_KEY")
}

func inspectOptionalWorkload(ctx context.Context, client kubernetes.Interface, namespace, releaseName string) []doctorFinding {
	findings := make([]doctorFinding, 0, 2)
	deployment, err := client.AppsV1().Deployments(namespace).Get(ctx, releaseName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		findings = append(findings, doctorFinding{OK: false, Subject: "deployment", Message: fmt.Sprintf("Deployment %q was not found", releaseName)})
	case err != nil:
		findings = append(findings, doctorFinding{OK: false, Subject: "deployment", Message: fmt.Sprintf("read Deployment %q: %v", releaseName, err)})
	default:
		desired := int32(1)
		if deployment.Spec.Replicas != nil {
			desired = *deployment.Spec.Replicas
		}
		if deployment.Status.ReadyReplicas != desired {
			findings = append(findings, doctorFinding{OK: false, Subject: "deployment", Message: fmt.Sprintf("Deployment %q has %d/%d ready replicas", releaseName, deployment.Status.ReadyReplicas, desired)})
		} else {
			findings = append(findings, doctorFinding{OK: true, Subject: "deployment", Message: fmt.Sprintf("Deployment %q has %d/%d ready replicas", releaseName, deployment.Status.ReadyReplicas, desired)})
		}
	}

	endpoints, err := client.CoreV1().Endpoints(namespace).Get(ctx, releaseName, metav1.GetOptions{})
	switch {
	case apierrors.IsNotFound(err):
		findings = append(findings, doctorFinding{Warning: true, Subject: "optional service endpoints", Message: fmt.Sprintf("Endpoints %q were not found", releaseName)})
	case err != nil:
		findings = append(findings, doctorFinding{Warning: true, Subject: "optional service endpoints", Message: fmt.Sprintf("read Endpoints %q: %v", releaseName, err)})
	default:
		ready := 0
		for _, subset := range endpoints.Subsets {
			ready += len(subset.Addresses)
		}
		if ready == 0 {
			findings = append(findings, doctorFinding{Warning: true, Subject: "optional service endpoints", Message: fmt.Sprintf("Endpoints %q have no ready addresses", releaseName)})
		} else {
			findings = append(findings, doctorFinding{OK: true, Subject: "optional service endpoints", Message: fmt.Sprintf("Endpoints %q have %d ready addresses", releaseName, ready)})
		}
	}
	return findings
}

func nonEmptyString(value any) (string, bool) {
	text, ok := value.(string)
	return text, ok && text != ""
}

func joinPath(parent, child string) string {
	if parent == "" {
		return child
	}
	return parent + "." + child
}
