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

	// Task Scheduling Stages (used for taskSchedulingLatency)
	TaskStageWatched  = "Watched"
	TaskStageAssumed  = "Assumed"  // The time when a task is logically allocated to a node(in the memory but hasn't been bound yet)
	TaskStagePreBound = "PreBound" // The time when a task finishes PreBind
	TaskStageBound    = "Bound"    // The time when a task is successfully bound to a node

	// Scheduling Stages
	SchedulingStagePredicate = "Predicate"
	SchedulingStageScoring   = "Scoring"
	SchedulingStagePreBind   = "PreBind"
	SchedulingStageBind      = "Bind"
)

var (
	e2eSchedulingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "e2e_scheduling_latency_milliseconds",
			Help:      "Duration of a single scheduling session execution in milliseconds. It covers the entire cycle from session open, action executions to session close.",
			Buckets:   prometheus.ExponentialBucketsRange(1, 5000, 20),
		},
	)

	openSessionDuration = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "open_session_duration_milliseconds",
			Help:      "Duration of opening a scheduling session in milliseconds, including cache snapshot and session initialization.",
			Buckets:   prometheus.ExponentialBucketsRange(1, 5000, 20),
		},
	)

	e2eJobSchedulingLatency = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "e2e_job_scheduling_latency_milliseconds",
			Help:      "End-to-end job scheduling latency in milliseconds (histogram of per-job duration from creation to last task assumed)",
			Buckets:   prometheus.ExponentialBucketsRange(100, 120000, 30),
		},
	)

	e2eJobSchedulingDuration = promauto.NewGaugeVec(
		prometheus.GaugeOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "e2e_job_scheduling_duration",
			Help:      "Per-job duration from job creation to last task assumed in scheduler cache, in milliseconds",
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
			Buckets:   prometheus.ExponentialBucketsRange(0.1, 100, 15),
		}, []string{"plugin", "OnSession"},
	)

	actionSchedulingLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "action_scheduling_latency_milliseconds",
			Help:      "Action scheduling latency in milliseconds",
			Buckets:   prometheus.ExponentialBucketsRange(1, 2000, 20),
		}, []string{"action"},
	)

	taskSchedulingLatency = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "task_scheduling_latency_milliseconds",
			Help:      "Task scheduling latency from creation to various stages in milliseconds",
			Buckets:   prometheus.ExponentialBucketsRange(50, 60000, 30),
		}, []string{"stage"},
	)

	schedulingStageDuration = promauto.NewHistogramVec(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "scheduling_stage_duration_milliseconds",
			Help:      "Duration of per-task scheduling stages (Predicate, Scoring, PreBind, Bind) in milliseconds",
			Buckets:   prometheus.ExponentialBucketsRange(0.1, 500, 20),
		}, []string{"stage"},
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

	reclaimVictims = promauto.NewHistogram(
		prometheus.HistogramOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "pod_reclaim_victims",
			Help:      "Distribution of reclaim victims actually evicted per successful task reclaim",
			Buckets:   []float64{0, 1, 2, 5, 10, 20, 50},
		},
	)

	reclaimAttempts = promauto.NewCounter(
		prometheus.CounterOpts{
			Subsystem: VolcanoSubSystemName,
			Name:      "total_reclaim_attempts",
			Help:      "Total task-level reclaim attempts by this scheduler instance since start",
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

// UpdateOpenSessionDuration updates the duration of opening a scheduling session
func UpdateOpenSessionDuration(duration time.Duration) {
	openSessionDuration.Observe(DurationInMilliseconds(duration))
}

// UpdateE2eSchedulingDurationByJob updates per-job duration from creation to last task assumed/pipelined
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

// UpdateTaskScheduleDuration updates single task scheduling latency (from creation to stage)
func UpdateTaskScheduleDuration(stage string, duration time.Duration) {
	taskSchedulingLatency.WithLabelValues(stage).Observe(DurationInMilliseconds(duration))
}

// UpdateSchedulingStageDuration updates duration for a scheduling stage
func UpdateSchedulingStageDuration(stage string, duration time.Duration) {
	schedulingStageDuration.WithLabelValues(stage).Observe(DurationInMilliseconds(duration))
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

// UpdateReclaimVictimsCount records the number of reclaim victims evicted in
// a single successful task reclaim.
func UpdateReclaimVictimsCount(victimsCount int) {
	reclaimVictims.Observe(float64(victimsCount))
}

// RegisterReclaimAttempts records a task-level reclaim attempt.
func RegisterReclaimAttempts() {
	reclaimAttempts.Inc()
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

// Duration get the time since specified start, clamped to >= 0.
func Duration(start time.Time) time.Duration {
	d := time.Since(start)
	if d < 0 {
		return 0
	}
	return d
}

// ConvertToUnix convert the time to Unix time
func ConvertToUnix(t time.Time) float64 {
	return float64(t.Unix())
}
