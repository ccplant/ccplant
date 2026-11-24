package app

import (
	"context"
	"errors"
	"strings"

	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/util/retry"
)

type sessionManagerUpgrade struct {
	ctx       context.Context
	config    *config.Config
	client    kubernetes.Interface
	namespace string
	desired   string
}

type sessionManagerImageUpdate struct {
	previousImage string
	image         string
	desired       string
	injectedCLI   bool
}

func reconcileSessionManagerVersion(upgrade sessionManagerUpgrade) error {
	cfg, desired := upgrade.config, upgrade.desired
	if !cfg.SessionManager.AutoUpgrade || desired == "" || cfg.SessionManager.CurrentVersion == "" {
		return nil
	}
	required, err := sessionManagerUpgradeRequired(cfg.SessionManager.CurrentVersion, desired)
	if err != nil || !required {
		return err
	}
	if cfg.SessionManager.DeploymentName == "" || cfg.SessionManager.ImageRepository == "" {
		return errors.New("deployment name and image repository are required for auto-upgrade")
	}
	image := strings.TrimSuffix(cfg.SessionManager.ImageRepository, ":") + ":" + desired
	return retry.RetryOnConflict(retry.DefaultRetry, func() error { return applySessionManagerUpgrade(upgrade, image) })
}

func applySessionManagerUpgrade(upgrade sessionManagerUpgrade, image string) error {
	deployment, err := upgrade.client.AppsV1().Deployments(upgrade.namespace).Get(upgrade.ctx, upgrade.config.SessionManager.DeploymentName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if !updateSessionManagerContainer(deployment.Spec.Template.Spec.Containers, image, upgrade.desired) {
		return errors.New("session-manager container not found in deployment")
	}
	updated, err := upgrade.client.AppsV1().Deployments(upgrade.namespace).Update(upgrade.ctx, deployment, metav1.UpdateOptions{})
	if err != nil {
		return err
	}
	if updated == nil {
		return errors.New("session-manager deployment update returned no object")
	}
	return nil
}

func updateSessionManagerContainer(containers []corev1.Container, image, desired string) bool {
	for index := range containers {
		container := &containers[index]
		if container.Name != "session-manager" {
			continue
		}
		update := sessionManagerImageUpdate{
			previousImage: container.Image, image: image, desired: desired,
			injectedCLI: containerEnvConfigured(container, "AGENTAPI_K8S_SESSION_CLI_IMAGE"),
		}
		container.Image = image
		for envIndex := range container.Env {
			update.apply(&container.Env[envIndex])
		}
		return true
	}
	return false
}

func containerEnvConfigured(container *corev1.Container, name string) bool {
	for _, env := range container.Env {
		if env.Name == name && env.Value != "" {
			return true
		}
	}
	return false
}

func (update sessionManagerImageUpdate) apply(env *corev1.EnvVar) {
	imageVariable := env.Name == "AGENTAPI_K8S_SESSION_CLI_IMAGE" || !update.injectedCLI && env.Name == "AGENTAPI_K8S_SESSION_IMAGE"
	if imageVariable && env.Value == update.previousImage {
		env.Value = update.image
	}
	if env.Name == "AGENTAPI_SESSION_MANAGER_CURRENT_VERSION" {
		env.Value = update.desired
	}
}
