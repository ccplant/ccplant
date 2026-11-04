package metrics

import (
	"context"

	"github.com/prometheus/client_golang/prometheus"
)

// StatusCounter returns the current number of resources grouped by status.
type StatusCounter func(context.Context) (map[string]int, error)

// StatusCollector exports a snapshot returned by a StatusCounter as a gauge.
type StatusCollector struct {
	desc          *prometheus.Desc
	count         StatusCounter
	knownStatuses []string
}

func NewStatusCollector(name, help string, knownStatuses []string, count StatusCounter) *StatusCollector {
	return &StatusCollector{
		desc:          prometheus.NewDesc(name, help, []string{"status"}, nil),
		count:         count,
		knownStatuses: append([]string(nil), knownStatuses...),
	}
}

func (c *StatusCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }

func (c *StatusCollector) Collect(ch chan<- prometheus.Metric) {
	counts, err := c.count(context.Background())
	if err != nil {
		ch <- prometheus.NewInvalidMetric(c.desc, err)
		return
	}
	if counts == nil {
		counts = make(map[string]int)
	}
	for _, status := range c.knownStatuses {
		if _, ok := counts[status]; !ok {
			counts[status] = 0
		}
	}
	for status, count := range counts {
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(count), status)
	}
}
