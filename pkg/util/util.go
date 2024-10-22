package util

import (
	"net/http"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/component-base/config"
	"k8s.io/component-base/metrics/legacyregistry"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

const (
	defaultSchedulerName       = "volcano"
	defaultLockObjectNamespace = "volcano-system"
)

var (
	defaultElectionLeaseDuration = metav1.Duration{Duration: 15 * time.Second}
	defaultElectionRenewDeadline = metav1.Duration{Duration: 10 * time.Second}
	defaultElectionRetryPeriod   = metav1.Duration{Duration: 2 * time.Second}
)

// GenerateComponentName generate component name volcano
func GenerateComponentName(schedulerNames []string) string {
	if len(schedulerNames) == 1 {
		return schedulerNames[0]
	}

	return defaultSchedulerName
}

// GenerateSchedulerName generate scheduler name for volcano job
func GenerateSchedulerName(schedulerNames []string) string {
	// choose the first scheduler name for volcano job if its schedulerName is empty
	if len(schedulerNames) > 0 {
		return schedulerNames[0]
	}

	return defaultSchedulerName
}

// LeaderElectionDefault set the LeaderElectionConfiguration  struct fields default value
func LeaderElectionDefault(l *config.LeaderElectionConfiguration) {
	l.LeaderElect = true
	l.LeaseDuration = defaultElectionLeaseDuration
	l.RenewDeadline = defaultElectionRenewDeadline
	l.RetryPeriod = defaultElectionRetryPeriod
	l.ResourceLock = resourcelock.LeasesResourceLock
	l.ResourceNamespace = defaultLockObjectNamespace
}

func PromHandler() http.Handler {
	// Unregister go and process related collector because it's duplicated and `legacyregistry.DefaultGatherer` also has registered them.
	prometheus.DefaultRegisterer.Unregister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))
	prometheus.DefaultRegisterer.Unregister(collectors.NewGoCollector())
	return promhttp.InstrumentMetricHandler(prometheus.DefaultRegisterer, promhttp.HandlerFor(prometheus.Gatherers{prometheus.DefaultGatherer, legacyregistry.DefaultGatherer}, promhttp.HandlerOpts{}))
}
