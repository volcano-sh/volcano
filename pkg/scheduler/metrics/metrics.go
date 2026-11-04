/*
Copyright 2020 The Volcano Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto" // auto-registry collectors in default registry
	"k8s.io/component-base/metrics"
	k8smetrics "k8s.io/kubernetes/pkg/scheduler/metrics"
)

const (
	// VolcanoSubSystemName - subsystem name in prometheus used by volcano
	VolcanoSubSystemName = "volcano"

	// OnSessionOpen label
	OnSessionOpen = "OnSessionOpen"

	// OnSessionClose label
	OnSessionClose = "OnSessionClose"
)

var (
	e2eSchedulingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "e2e_scheduling_latency_milliseconds",
			Help:      "E2e scheduling latency in milliseconds (scheduling algorithm + binding)",
			Buckets:   prometheus.ExponentialBuckets(5, 2, 15),
		},
	)

	e2eJobSchedulingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "e2e_job_scheduling_latency_milliseconds",
			Help:      "E2e job scheduling latency in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(32, 2, 10),
		},
	)

	e2eJobSchedulingDuration = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "e2e_job_scheduling_duration",
			Help:      "E2E job scheduling duration",
		},
		[]string{"job_name", "queue", "job_namespace"},
	)

	e2eJobSchedulingStartTime = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "e2e_job_scheduling_start_time",
			Help:      "E2E job scheduling start time",
		},
		[]string{"job_name", "queue", "job_namespace"},
	)

	e2eJobSchedulingLastTime = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "e2e_job_scheduling_last_time",
			Help:      "E2E job scheduling last time",
		},
		[]string{"job_name", "queue", "job_namespace"},
	)

	pluginSchedulingLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "plugin_scheduling_latency_milliseconds",
			Help:      "Plugin scheduling latency in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(5, 2, 15),
		}, []string{"plugin", "OnSession"},
	)

	actionSchedulingLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "action_scheduling_latency_milliseconds",
			Help:      "Action scheduling latency in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(5, 2, 15),
		}, []string{"action"},
	)

	taskSchedulingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "task_scheduling_latency_milliseconds",
			Help:      "Task scheduling latency in milliseconds",
			Buckets:   prometheus.ExponentialBuckets(5, 2, 15),
		},
	)

	scheduleAttempts = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "schedule_attempts_total",
			Help:      "Number of attempts to schedule pods, by the result. 'unschedulable' means a pod could not be scheduled, while 'error' means an internal scheduler problem.",
		}, []string{"result"},
	)

	preemptionVictims = promauto.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "pod_preemption_victims",
			Help:      "Number of selected preemption victims",
		},
	)

	preemptionAttempts = promauto.NewCounter(
		prometheus.CounterOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "total_preemption_attempts",
			Help:      "Total preemption attempts in the cluster till now",
		},
	)

	preemptionBackoffSkips = promauto.NewCounter(
		prometheus.CounterOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "preemption_backoff_skips_total",
			Help:      "Number of times a starving job was skipped by the preempt action because its previous preemption attempts failed and it is backing off",
		},
	)

	preemptIneligible = promauto.NewCounterVec(
		prometheus.CounterOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "preempt_task_ineligible_total",
			Help:      "Number of times a pending task was not allowed to preempt in a session, by reason",
		}, []string{"reason"},
	)

	preemptStaleNominations = promauto.NewCounter(
		prometheus.CounterOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "preempt_stale_nominations_total",
			Help:      "Number of times a pending task's nominated node was not in the session (gone or not ready), so the nomination was ignored and cleared",
		},
	)

	unscheduleTaskCount = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "unschedule_task_count",
			Help:      "Number of tasks could not be scheduled",
		}, []string{"job_id"},
	)

	unscheduleJobCount = promauto.NewGauge(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "unschedule_job_count",
			Help:      "Number of jobs could not be scheduled",
		},
	)
)

// InitKubeSchedulerRelatedMetrics is used to init metrics global variables in k8s.io/kubernetes/pkg/scheduler/metrics/metrics.go.
// We don't use InitMetrics() to init all global variables because currently only "Goroutines" is required when calling kube-scheduler
// related plugins. And there is no need to export these metrics, therefore currently initialization is enough.
func InitKubeSchedulerRelatedMetrics() {
	k8smetrics.Goroutines = metrics.NewGaugeVec(
		&metrics.GaugeOpts{
			Subsystem:      VolcanoSubSystemName,
			Name:           "goroutines",
			Help:           "Number of running goroutines split by the work they do such as binding.",
			StabilityLevel: metrics.ALPHA,
		}, []string{"operation"})
}

// UpdatePluginDuration updates latency for every plugin
func UpdatePluginDuration(pluginName, onSessionStatus string, duration time.Duration) {
	pluginSchedulingLatency.WithLabelValues(pluginName, onSessionStatus).Observe(DurationInMilliseconds(duration))
}

// UpdateActionDuration updates latency for every action
func UpdateActionDuration(actionName string, duration time.Duration) {
	actionSchedulingLatency.WithLabelValues(actionName).Observe(DurationInMilliseconds(duration))
}

// UpdateE2eDuration updates entire end to end scheduling latency
func UpdateE2eDuration(duration time.Duration) {
	e2eSchedulingLatency.Observe(DurationInMilliseconds(duration))
}

// UpdateE2eSchedulingDurationByJob updates entire end to end scheduling duration
func UpdateE2eSchedulingDurationByJob(jobName string, queue string, namespace string, duration time.Duration) {
	e2eJobSchedulingDuration.WithLabelValues(jobName, queue, namespace).Set(DurationInMilliseconds(duration))
	e2eJobSchedulingLatency.Observe(DurationInMilliseconds(duration))
}

// UpdateE2eSchedulingStartTimeByJob updates the start time of scheduling
func UpdateE2eSchedulingStartTimeByJob(jobName string, queue string, namespace string, t time.Time) {
	e2eJobSchedulingStartTime.WithLabelValues(jobName, queue, namespace).Set(ConvertToUnix(t))
}

// UpdateE2eSchedulingLastTimeByJob updates the last time of scheduling
func UpdateE2eSchedulingLastTimeByJob(jobName string, queue string, namespace string, t time.Time) {
	e2eJobSchedulingLastTime.WithLabelValues(jobName, queue, namespace).Set(ConvertToUnix(t))
}

// UpdateTaskScheduleDuration updates single task scheduling latency
func UpdateTaskScheduleDuration(duration time.Duration) {
	taskSchedulingLatency.Observe(DurationInMilliseconds(duration))
}

// UpdatePodScheduleStatus update pod schedule decision, could be Success, Failure, Error
func UpdatePodScheduleStatus(label string, count int) {
	scheduleAttempts.WithLabelValues(label).Add(float64(count))
}

// UpdatePreemptionVictimsCount updates count of preemption victims
func UpdatePreemptionVictimsCount(victimsCount int) {
	preemptionVictims.Set(float64(victimsCount))
}

// RegisterPreemptionAttempts records number of attempts for preemtion
func RegisterPreemptionAttempts() {
	preemptionAttempts.Inc()
}

// RegisterPreemptionBackoffSkip records a starving job skipped by preemption backoff
func RegisterPreemptionBackoffSkip() {
	preemptionBackoffSkips.Inc()
}

// Reasons a pending task is not allowed to preempt (preempt_task_ineligible_total).
const (
	PreemptIneligiblePolicyNever       = "preemption_policy_never"
	PreemptIneligiblePredicateError    = "nominated_node_predicate_error"
	PreemptIneligibleTerminatingVictim = "nominated_node_terminating_victim"
)

func init() {
	for _, reason := range []string{PreemptIneligiblePolicyNever, PreemptIneligiblePredicateError, PreemptIneligibleTerminatingVictim} {
		preemptIneligible.WithLabelValues(reason)
	}
}

// RegisterPreemptIneligible records a pending task refused preemption for reason
func RegisterPreemptIneligible(reason string) {
	preemptIneligible.WithLabelValues(reason).Inc()
}

// RegisterPreemptStaleNomination records a nomination to a node the session does not hold
func RegisterPreemptStaleNomination() {
	preemptStaleNominations.Inc()
}

// UpdateUnscheduleTaskCount records total number of unscheduleable tasks
func UpdateUnscheduleTaskCount(jobID string, taskCount int) {
	unscheduleTaskCount.WithLabelValues(jobID).Set(float64(taskCount))
}

// UpdateUnscheduleJobCount records total number of unscheduleable jobs
func UpdateUnscheduleJobCount(jobCount int) {
	unscheduleJobCount.Set(float64(jobCount))
}

// DurationInMicroseconds gets the time in microseconds.
func DurationInMicroseconds(duration time.Duration) float64 {
	return float64(duration.Nanoseconds()) / float64(time.Microsecond.Nanoseconds())
}

// DurationInMilliseconds gets the time in milliseconds.
func DurationInMilliseconds(duration time.Duration) float64 {
	return float64(duration.Nanoseconds()) / float64(time.Millisecond.Nanoseconds())
}

// DurationInSeconds gets the time in seconds.
func DurationInSeconds(duration time.Duration) float64 {
	return duration.Seconds()
}

// Duration get the time since specified start
func Duration(start time.Time) time.Duration {
	return time.Since(start)
}

// ConvertToUnix convert the time to Unix time
func ConvertToUnix(t time.Time) float64 {
	return float64(t.Unix())
}
