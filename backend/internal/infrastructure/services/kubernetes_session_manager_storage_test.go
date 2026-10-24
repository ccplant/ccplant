package services

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	"github.com/takutakahashi/agentapi-proxy/internal/domain/entities"
)

func TestSharedStorageVolumesAndMounts(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	request := &entities.RunServerRequest{UserID: "alice", Storages: []entities.ResolvedStorageAttachment{
		{StorageAttachment: entities.StorageAttachment{StorageID: "object-one", MountPath: "/home/agentapi/cache", ReadOnly: true}, Kind: entities.StorageKindObjectSnapshot},
	}}
	session := NewKubernetesSession("session", request, "deployment", "service", "", "test-ns", 9000, nil, nil)

	volumes := manager.buildVolumes(session)
	require.Contains(t, volumes, corev1.Volume{Name: "shared-storage-0", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})

	mounts := manager.buildMainContainerVolumeMounts(session, request)
	require.Contains(t, mounts, corev1.VolumeMount{Name: "shared-storage-0", MountPath: "/home/agentapi/cache", ReadOnly: true})
}

func TestManagedStorageUsesExistingContainerPaths(t *testing.T) {
	manager := newWorkloadTestManager(t, false)
	request := &entities.RunServerRequest{UserID: "alice", Storages: []entities.ResolvedStorageAttachment{
		{StorageAttachment: entities.StorageAttachment{StorageID: "state", StoragePath: "session-state/codex", MountPath: "/home/agentapi/.codex", Managed: true}, Kind: entities.StorageKindObjectSnapshot},
	}}
	session := NewKubernetesSession("session", request, "deployment", "service", "", "test-ns", 9000, nil, nil)

	require.NotContains(t, manager.buildVolumes(session), corev1.Volume{Name: "shared-storage-0", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}})
	for _, mount := range manager.buildMainContainerVolumeMounts(session, request) {
		require.NotEqual(t, "/home/agentapi/.codex", mount.MountPath)
	}
}

func TestFilterManagedStoragesForAgent(t *testing.T) {
	storages := []entities.ResolvedStorageAttachment{
		{StorageAttachment: entities.StorageAttachment{StoragePath: "session-state/codex", Managed: true}},
		{StorageAttachment: entities.StorageAttachment{StoragePath: "session-state/claude", Managed: true}},
		{StorageAttachment: entities.StorageAttachment{StoragePath: "workspace", Managed: true}},
	}
	filtered := filterManagedStoragesForAgent(storages, "codex-acp")
	require.Len(t, filtered, 2)
	require.Equal(t, "session-state/codex", filtered[0].StoragePath)
	require.Equal(t, "workspace", filtered[1].StoragePath)
}
