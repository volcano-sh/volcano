package exporter

import (
	"encoding/json"
	"log/slog"
	"strconv"

	"github.com/prometheus/client_golang/prometheus"
	auditv1 "k8s.io/apiserver/pkg/apis/audit/v1"
)

// Metric definitions
var (
	registry = prometheus.NewRegistry()

	apiRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "api_requests_total",
		Help: "Total number of API requests to the scheduler",
	}, []string{"cluster", "namespace", "user", "verb", "resource", "code"})

	podDeletedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pod_deleted_total",
		Help: "Total number of pods deleted",
	}, []string{"cluster", "namespace", "user", "phase"})

	podCompletedTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "pod_completed_total",
		Help: "Total number of pods transitioned to completed status",
	}, []string{"cluster", "namespace", "user", "phase"})

	podSchedulingLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "pod_scheduling_latency_seconds",
		Help:    "Duration from pod creation to scheduled on node in seconds",
		Buckets: prometheus.ExponentialBuckets(0.001, 2, 20),
	}, []string{"cluster", "namespace", "user"})

	batchJobCompleteLatency = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "batchjob_completion_latency_seconds",
		Help:    "Time from job creation to complete condition in seconds",
		Buckets: prometheus.ExponentialBuckets(1, 2, 12),
	}, []string{"cluster", "namespace", "user"})
)

func init() {
	registry.MustRegister(
		apiRequests,
		podSchedulingLatency,
		podDeletedTotal,
		batchJobCompleteLatency,
		podCompletedTotal,
	)
}

// updateMetrics processes audit event and updates metrics
func (p *Exporter) updateMetrics(clusterLabel string, event auditv1.Event) {
	if event.ResponseStatus == nil ||
		(event.ResponseStatus.Code < 200 || event.ResponseStatus.Code >= 300) {
		return
	}

	var ns string
	if event.ObjectRef != nil {
		ns = event.ObjectRef.Namespace
	}

	if event.Stage == auditv1.StageResponseComplete {
		labels := []string{
			clusterLabel,
			ns,
			extractUserAgent(event.UserAgent),
			event.Verb,
			extractResourceName(event),
			strconv.Itoa(int(event.ResponseStatus.Code)),
		}
		apiRequests.WithLabelValues(labels...).Inc()
	}

	if event.ObjectRef != nil {
		switch event.ObjectRef.Resource {
		case "pods":
			if event.ObjectRef.Subresource == "binding" && event.Verb == "create" {
				target := buildTarget(event.ObjectRef)
				createTime, exists := p.podCreationTimes[target]
				if !exists {
					// Kueue's audit events may create pod/binding events before pod creation events
					user := extractUserAgent(event.UserAgent)
					podSchedulingLatency.WithLabelValues(
						clusterLabel,
						ns,
						user,
					).Observe(0)
					p.podCreationTimes[target] = nil
					return
				}

				if createTime == nil {
					return
				}
				latency := event.StageTimestamp.Sub(*createTime).Seconds()

				user := extractUserAgent(event.UserAgent)
				podSchedulingLatency.WithLabelValues(
					clusterLabel,
					ns,
					user,
				).Observe(latency)
				p.podCreationTimes[target] = nil

			} else {
				if event.Verb == "create" {
					var pod Pod
					err := json.Unmarshal(event.ResponseObject.Raw, &pod)
					if err != nil {
						slog.Error("failed to unmarshal", "err", err)
						return
					}

					target := target{
						Name:      pod.Metadata.Name,
						Namespace: pod.Metadata.Namespace,
					}
					if pod.Spec.NodeName == "" {
						p.podCreationTimes[target] = &event.StageTimestamp.Time
					} else {
						p.podCreationTimes[target] = nil
					}
				} else if event.Verb == "delete" && event.ResponseObject != nil {
					target := buildTarget(event.ObjectRef)
					_, ok := p.podCreationTimes[target]
					if ok {
						var pod Pod
						if err := json.Unmarshal(event.ResponseObject.Raw, &pod); err != nil {
							slog.Error("failed to unmarshal pod during delete", "err", err)
							return
						}

						delete(p.podCreationTimes, target)
						user := extractUserAgent(event.UserAgent)
						podDeletedTotal.WithLabelValues(
							clusterLabel,
							ns,
							user,
							pod.Status.Phase,
						).Inc()
					}
				} else if (event.Verb == "update" || event.Verb == "patch") &&
					event.ObjectRef.Subresource == "status" &&
					event.ResponseObject != nil {

					target := buildTarget(event.ObjectRef)
					t, ok := p.podCreationTimes[target]
					if ok && t == nil {
						var pod Pod
						if err := json.Unmarshal(event.ResponseObject.Raw, &pod); err != nil {
							slog.Error("failed to unmarshal new pod during update", "err", err)
							return
						}

						phase := pod.Status.Phase
						if podCompletedPhases[phase] {
							delete(p.podCreationTimes, target)
							user := extractUserAgent(event.UserAgent)
							podCompletedTotal.WithLabelValues(
								clusterLabel,
								ns,
								user,
								phase,
							).Inc()
						}
					}
				}
			}

		case "jobs":
			if event.Verb == "create" && event.ResponseObject != nil {
				var job BatchJob
				err := json.Unmarshal(event.ResponseObject.Raw, &job)
				if err != nil {
					slog.Error("failed to unmarshal", "err", err)
					return
				}

				target := target{
					Name:      job.Metadata.Name,
					Namespace: job.Metadata.Namespace,
				}
				p.batchJobCreationTimes[target] = &event.StageTimestamp.Time
			} else if event.Verb == "delete" {
				target := buildTarget(event.ObjectRef)
				delete(p.batchJobCreationTimes, target)
			} else {
				target := buildTarget(event.ObjectRef)
				if createTime, ok := p.batchJobCreationTimes[target]; ok && createTime != nil && event.ResponseObject != nil {
					var job BatchJob
					err := json.Unmarshal(event.ResponseObject.Raw, &job)
					if err != nil {
						slog.Error("failed to unmarshal job", "err", err)
						return
					}
					if job.Status.IsCompleted() {
						latency := event.StageTimestamp.Sub(job.Metadata.CreationTimestamp).Seconds()
						user := extractUserAgent(event.UserAgent)
						batchJobCompleteLatency.WithLabelValues(
							clusterLabel,
							ns,
							user,
						).Observe(latency)
						p.batchJobCreationTimes[target] = nil
					}
				}
			}
		}
	}
}

var podCompletedPhases = map[string]bool{"Succeeded": true, "Failed": true}
