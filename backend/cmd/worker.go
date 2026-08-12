package cmd

import (
	"context"
	"log"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/takutakahashi/agentapi-proxy/internal/app"
	"github.com/takutakahashi/agentapi-proxy/internal/modules/schedule"
	"github.com/takutakahashi/agentapi-proxy/pkg/config"
	slackbotcleanup "github.com/takutakahashi/agentapi-proxy/pkg/slackbot_cleanup"
	stockinventory "github.com/takutakahashi/agentapi-proxy/pkg/stock_inventory"
)

var (
	workerConfigPath string
	workerVerbose    bool
)

// WorkerCmd runs background controllers without exposing the proxy HTTP API.
var WorkerCmd = &cobra.Command{
	Use:   "worker",
	Short: "Start AgentAPI background workers",
	Args:  cobra.NoArgs,
	RunE:  runWorkers,
}

func init() {
	WorkerCmd.Flags().StringVarP(&workerConfigPath, "config", "c", "config.json", "Configuration file path")
	WorkerCmd.Flags().BoolVarP(&workerVerbose, "verbose", "v", false, "Enable verbose logging")
}

func runWorkers(_ *cobra.Command, _ []string) error {
	cfg, err := loadRuntimeConfig(workerConfigPath)
	if err != nil {
		return err
	}
	if workerVerbose {
		log.SetFlags(log.LstdFlags | log.Lshortfile)
	}

	runtime := app.NewServer(cfg, workerVerbose)
	cfg = runtime.GetConfig()
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	var mu sync.Mutex
	var scheduleWorker *schedule.LeaderWorker
	var cleanupWorker *slackbotcleanup.LeaderCleanupWorker
	var stockWorker *stockinventory.LeaderWorker
	reconcile := func(current *config.Config) {
		mu.Lock()
		defer mu.Unlock()
		if scheduleWorker != nil {
			scheduleWorker.Stop()
			scheduleWorker = nil
		}
		if cleanupWorker != nil {
			cleanupWorker.Stop()
			cleanupWorker = nil
		}
		if stockWorker != nil {
			stockWorker.Stop()
			stockWorker = nil
		}
		if current.ScheduleWorker.Enabled {
			scheduleWorker = startScheduleWorker(current, runtime)
		}
		if current.SlackbotCleanupWorker.Enabled {
			cleanupWorker = startSlackbotCleanupWorker(current, runtime)
		}
		if current.StockInventoryWorker.Enabled {
			stockWorker = startStockInventoryWorker(current, runtime)
		}
	}
	reconcile(cfg)
	runtime.GetConfigProvider().Subscribe(reconcile)
	startSessionAllocator(cfg, runtime)
	startSlackSocketManager(cfg, runtime)

	<-ctx.Done()
	mu.Lock()
	if scheduleWorker != nil {
		scheduleWorker.Stop()
	}
	if cleanupWorker != nil {
		cleanupWorker.Stop()
	}
	if stockWorker != nil {
		stockWorker.Stop()
	}
	mu.Unlock()
	return runtime.Shutdown(0)
}

func loadRuntimeConfig(path string) (*config.Config, error) {
	cfg, err := config.LoadConfig(path)
	if err == nil {
		return cfg, nil
	}
	log.Printf("Failed to load config from %s, trying environment: %v", path, err)
	return config.LoadConfig("")
}
