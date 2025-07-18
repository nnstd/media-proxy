package metrics

import "github.com/prometheus/client_golang/prometheus"

type Metrics struct {
	SuccessfullyServed *prometheus.CounterVec
	ServedCached       *prometheus.CounterVec
}

func InitializeMetrics(registry prometheus.Registerer, constLabels prometheus.Labels) *Metrics {
	metrics := &Metrics{
		SuccessfullyServed: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "successfully_served",
			Help:        "Number of successfully served requests",
			ConstLabels: constLabels,
		}, []string{"type", "hostname", "url"}),
		ServedCached: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name:        "served_cached",
			Help:        "Number of served responses from cache",
			ConstLabels: constLabels,
		}, []string{"type", "hostname", "url"}),
	}

	// Register the custom metrics with the Prometheus registry
	registry.MustRegister(metrics.SuccessfullyServed)
	registry.MustRegister(metrics.ServedCached)

	return metrics
}
