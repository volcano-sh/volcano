package metrics

import (
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestDeleteJobMetrics(t *testing.T) {
	jobName := "test-job"
	queue := "test-queue"
	namespace := "test-ns"

	UpdateE2eSchedulingDurationByJob(jobName, queue, namespace, 100*time.Millisecond)
	UpdateE2eSchedulingStartTimeByJob(jobName, queue, namespace, time.Now())
	UpdateE2eSchedulingLastTimeByJob(jobName, queue, namespace, time.Now())
	UpdateJobShare(namespace, jobName, 1)
	RegisterJobRetries(jobName)
	unscheduleTaskCount.WithLabelValues(jobName).Set(3)

	if got := testutil.ToFloat64(jobShare.WithLabelValues(namespace, jobName)); got != 1 {
		t.Fatalf("expected jobShare to be 1 before delete, got %v", got)
	}
	if got := testutil.ToFloat64(unscheduleTaskCount.WithLabelValues(jobName)); got != 3 {
		t.Fatalf("expected unscheduleTaskCount to be 3 before delete, got %v", got)
	}

	DeleteJobMetrics(jobName, queue, namespace)

	if got := testutil.ToFloat64(e2eJobSchedulingDuration.WithLabelValues(jobName, queue, namespace)); got != 0 {
		t.Errorf("expected e2eJobSchedulingDuration to be reset after delete, got %v", got)
	}
	if got := testutil.ToFloat64(e2eJobSchedulingStartTime.WithLabelValues(jobName, queue, namespace)); got != 0 {
		t.Errorf("expected e2eJobSchedulingStartTime to be reset after delete, got %v", got)
	}
	if got := testutil.ToFloat64(e2eJobSchedulingLastTime.WithLabelValues(jobName, queue, namespace)); got != 0 {
		t.Errorf("expected e2eJobSchedulingLastTime to be reset after delete, got %v", got)
	}
	if got := testutil.ToFloat64(unscheduleTaskCount.WithLabelValues(jobName)); got != 0 {
		t.Errorf("expected unscheduleTaskCount to be reset after delete, got %v", got)
	}
	if got := testutil.ToFloat64(jobShare.WithLabelValues(namespace, jobName)); got != 0 {
		t.Errorf("expected jobShare to be reset after delete, got %v", got)
	}
	if got := testutil.ToFloat64(jobRetryCount.WithLabelValues(jobName)); got != 0 {
		t.Errorf("expected jobRetryCount to be reset after delete, got %v", got)
	}
}
