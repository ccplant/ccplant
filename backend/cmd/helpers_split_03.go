package cmd

import (
	_ "embed"
	"fmt"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/pkg/sessionsettings"
)

func runSetup(cmd *cobra.Command, args []string) error {
	opts := sessionsettings.SetupOptions{
		InputPath:                 setupInputPath,
		SettingsFile:              setupSettingsFile,
		CompileOptions:            sessionsettings.DefaultCompileOptions(),
		CredentialsFile:           setupCredentialsFile,
		NotificationSubscriptions: setupNotificationSubscriptions,
		NotificationsDir:          setupNotificationsDir,
		RegisterMarketplaces:      setupRegisterMarketplaces,
	}

	if err := sessionsettings.Setup(opts); err != nil {
		return fmt.Errorf("setup failed: %w", err)
	}

	fmt.Println("Session setup completed successfully!")
	return nil
}
