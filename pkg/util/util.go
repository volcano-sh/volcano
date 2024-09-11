package util

import (
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/component-base/config"
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
