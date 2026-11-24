package app

import (
	"log"

	"github.com/takutakahashi/agentapi-proxy/internal/infrastructure/services"
	"github.com/takutakahashi/agentapi-proxy/pkg/notification"
)

func (s *Server) initializeNotifications() {
	// Initialize notification service
	baseDir := notification.GetBaseDir()
	notificationSvc, err := notification.NewService(baseDir)
	if err != nil {
		log.Printf("Failed to initialize notification service: %v", err)
	} else {
		s.notificationSvc = notificationSvc
		notificationSvc.SetBaseURLResolver(func() string { return s.configProvider.String("notifications.base_url") })
		log.Printf("Notification service initialized successfully")

		// Notification subscriptions are application data owned by the API role.
		// persistenceClient is either the legacy Kubernetes client or the libSQL-backed
		// compatibility adapter, so this wiring remains independent of the concrete
		// session-manager implementation and requires no API ServiceAccount token.
		syncer := services.NewKubernetesSubscriptionSecretSyncer(
			s.persistenceClient,
			s.namespace,
			notificationSvc.GetStorage(),
			"", // Use default prefix
		)
		notificationSvc.SetSecretSyncer(syncer)
		notificationSvc.SetSubscriptionReader(syncer)
		notificationSvc.SetSubscriptionWriter(syncer)
		log.Printf("Notification subscription persistence configured (read+write)")
		if k8sMgr, ok := s.sessionManager.(*services.KubernetesSessionManager); ok {
			k8sMgr.SetCodexAuthNotifier(notificationSvc.SendNotificationToUser)
		}
	}

}
