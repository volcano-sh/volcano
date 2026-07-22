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
	fixedTime := time.Unix(1700000000, 0)
	wantUnixTime := ConvertToUnix(fixedTime)

	UpdateE2eSchedulingDurationByJob(jobName, queue, namespace, 100*time.Millisecond)
	UpdateE2eSchedulingStartTimeByJob(jobName, queue, namespace, fixedTime)
	UpdateE2eSchedulingLastTimeByJob(jobName, queue, namespace, fixedTime)
	UpdateJobShare(namespace, jobName, 1)
	RegisterJobRetries(jobName)
	unscheduleTaskCount.WithLabelValues(jobName).Set(3)

	if got := testutil.ToFloat64(e2eJobSchedulingDuration.WithLabelValues(jobName, queue, namespace)); got != 100 {
		t.Fatalf("expected e2eJobSchedulingDuration to be 100 before delete, got %v", got)
	}
	if got := testutil.ToFloat64(e2eJobSchedulingStartTime.WithLabelValues(jobName, queue, namespace)); got != wantUnixTime {
		t.Fatalf("expected e2eJobSchedulingStartTime to be %v before delete, got %v", wantUnixTime, got)
	}
	if got := testutil.ToFloat64(e2eJobSchedulingLastTime.WithLabelValues(jobName, queue, namespace)); got != wantUnixTime {
		t.Fatalf("expected e2eJobSchedulingLastTime to be %v before delete, got %v", wantUnixTime, got)
	}
	if got := testutil.ToFloat64(jobShare.WithLabelValues(namespace, jobName)); got != 1 {
		t.Fatalf("expected jobShare to be 1 before delete, got %v", got)
	}
	if got := testutil.ToFloat64(jobRetryCount.WithLabelValues(jobName)); got != 1 {
		t.Fatalf("expected jobRetryCount to be 1 before delete, got %v", got)
	}
	if got := testutil.ToFloat64(unscheduleTaskCount.WithLabelValues(jobName)); got != 3 {
		t.Fatalf("expected unscheduleTaskCount to be 3 before delete, got %v", got)
	}

	DeleteJobMetrics(jobName, queue, namespace)

	// WithLabelValues would silently recreate a zero-value series here even
	// if DeleteJobMetrics failed to remove it, masking a regression. Check
	// the vec is actually empty instead of reading a (possibly fresh) value.
	if count := testutil.CollectAndCount(e2eJobSchedulingDuration); count != 0 {
		t.Errorf("expected no e2eJobSchedulingDuration series after delete, got %d", count)
	}
	if count := testutil.CollectAndCount(e2eJobSchedulingStartTime); count != 0 {
		t.Errorf("expected no e2eJobSchedulingStartTime series after delete, got %d", count)
	}
	if count := testutil.CollectAndCount(e2eJobSchedulingLastTime); count != 0 {
		t.Errorf("expected no e2eJobSchedulingLastTime series after delete, got %d", count)
	}
	if count := testutil.CollectAndCount(unscheduleTaskCount); count != 0 {
		t.Errorf("expected no unscheduleTaskCount series after delete, got %d", count)
	}
	if count := testutil.CollectAndCount(jobShare); count != 0 {
		t.Errorf("expected no jobShare series after delete, got %d", count)
	}
	if count := testutil.CollectAndCount(jobRetryCount); count != 0 {
		t.Errorf("expected no jobRetryCount series after delete, got %d", count)
	}
}
