package healthmonitor

import (
	"fmt"

	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/exp/maps"

	"github.com/armadaproject/armada/internal/common/armadacontext"
)

// MultiHealthMonitor wraps multiple HealthMonitors and itself implements the HealthMonitor interface.
type MultiHealthMonitor struct {
	name string
	// Map from name to the health monitor for each cluster.
	healthMonitorsByName map[string]HealthMonitor

	// Minimum replicas that must be available.
	minimumReplicasAvailable int
	// Prometheus metrics are prefixed with this.
	metricsPrefix string

	healthPrometheusDesc *prometheus.Desc
}

func NewMultiHealthMonitor(name string, healthMonitorsByName map[string]HealthMonitor) *MultiHealthMonitor {
	srv := &MultiHealthMonitor{
		name:                     name,
		minimumReplicasAvailable: len(healthMonitorsByName),
		healthMonitorsByName:     maps.Clone(healthMonitorsByName),
	}
	srv.initialiseMetrics()
	return srv
}

func (srv *MultiHealthMonitor) WithMinimumReplicasAvailable(v int) *MultiHealthMonitor {
	srv.minimumReplicasAvailable = v
	return srv
}

// WithMetricsPrefix adds a prefix to exported Prometheus metrics.
// Must be called before Describe or Collect.
func (srv *MultiHealthMonitor) WithMetricsPrefix(v string) *MultiHealthMonitor {
	srv.metricsPrefix = v
	srv.initialiseMetrics()
	return srv
}

func (srv *MultiHealthMonitor) initialiseMetrics() {
	metricsPrefix := srv.name
	if srv.metricsPrefix != "" {
		metricsPrefix = srv.metricsPrefix + srv.name
	}
	srv.healthPrometheusDesc = prometheus.NewDesc(
		metricsPrefix+"_health",
		fmt.Sprintf("Shows whether %s is healthy.", srv.name),
		nil,
		nil,
	)
}

// IsHealthy returns false if either
// 1. any child is unhealthy for a reason other than timeout or
// 2. at least len(healthMonitorsByName) - minReplicasAvailable + 1 children have timed out.
func (srv *MultiHealthMonitor) IsHealthy() (ok bool, reason string, err error) {
	replicasAvailable := 0
	for name, healthMonitor := range srv.healthMonitorsByName {
		ok, reason, err = healthMonitor.IsHealthy()
		if err != nil {
			return ok, reason, errors.WithMessagef(err, "failed to check health of %s in %s", name, srv.name)
		}

		// Cluster is unhealthy if any child is unhealthy for a reason other than timeout
		// or if too many have timed out.
		if !ok {
			if reason != UnavailableReason {
				return ok, reason, nil
			}
		} else {
			replicasAvailable++
		}
	}
	if replicasAvailable < srv.minimumReplicasAvailable {
		return false, InsufficientReplicasAvailableReason, nil
	}
	return true, "", nil
}

// Run initialises prometheus metrics and starts any child health checkers.
func (srv *MultiHealthMonitor) Run(ctx *armadacontext.Context) error {
	g, ctx := armadacontext.ErrGroup(ctx)
	for _, healthMonitor := range srv.healthMonitorsByName {
		healthMonitor := healthMonitor
		g.Go(func() error { return healthMonitor.Run(ctx) })
	}
	return g.Wait()
}

func (srv *MultiHealthMonitor) Describe(c chan<- *prometheus.Desc) {
	c <- srv.healthPrometheusDesc

	for _, healthMonitor := range srv.healthMonitorsByName {
		healthMonitor.Describe(c)
	}
}

func (srv *MultiHealthMonitor) Collect(c chan<- prometheus.Metric) {
	resultOfMostRecentHealthCheck := 0.0
	if ok, _, _ := srv.IsHealthy(); ok {
		resultOfMostRecentHealthCheck = 1.0
	}
	c <- prometheus.MustNewConstMetric(
		srv.healthPrometheusDesc,
		prometheus.GaugeValue,
		resultOfMostRecentHealthCheck,
	)

	for _, healthMonitor := range srv.healthMonitorsByName {
		healthMonitor.Collect(c)
	}
}
