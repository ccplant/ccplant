package metrics

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"
)

func TestStatusCollectorExportsCountsAndKnownZeroStatuses(t *testing.T) {
	collector := NewStatusCollector("agentapi_test_resources", "Test resources.", []string{"idle", "running", "offline"}, func(context.Context) (map[string]int, error) {
		return map[string]int{"idle": 2, "running": 1}, nil
	})
	require.NoError(t, testutil.CollectAndCompare(collector, strings.NewReader(`# HELP agentapi_test_resources Test resources.
# TYPE agentapi_test_resources gauge
agentapi_test_resources{status="idle"} 2
agentapi_test_resources{status="offline"} 0
agentapi_test_resources{status="running"} 1
`)))
}

func TestStatusCollectorReportsCollectionErrors(t *testing.T) {
	collector := NewStatusCollector("agentapi_test_resources", "Test resources.", nil, func(context.Context) (map[string]int, error) {
		return nil, errors.New("unavailable")
	})
	registry := prometheus.NewPedanticRegistry()
	registry.MustRegister(collector)
	_, err := registry.Gather()
	require.ErrorContains(t, err, "unavailable")
}
