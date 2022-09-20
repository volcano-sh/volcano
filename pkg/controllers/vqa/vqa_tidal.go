package vqa

import (
	"fmt"
	"github.com/robfig/cron/v3"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	"strings"
	"time"
	autoscalingv1alpha1 "volcano.sh/apis/pkg/apis/autoscaling/v1alpha1"
)

func formatSchedule(vqa *autoscalingv1alpha1.VerticalQueueAutoscaler, sche string, recorder record.EventRecorder) string {
	if strings.Contains(sche, "TZ") {
		if recorder != nil {
			recorder.Eventf(vqa, corev1.EventTypeWarning, "UnsupportedSchedule", "CRON_TZ or TZ used in schedule %q is not officially supported, see https://kubernetes.io/docs/concepts/workloads/controllers/cron-jobs/ for more details", sche)
		}
		return sche
	}
	return sche
}

// getNextScheduleTime gets the time of next schedule after last scheduled and before now
//
//	it returns nil if no unmet schedule times.
//
// If there are too many (>100) unstarted times, it will raise a warning and but still return
// the list of missed times.
func getNextScheduleTime(vqa *autoscalingv1alpha1.VerticalQueueAutoscaler, now time.Time, schedule cron.Schedule, recorder record.EventRecorder) (*time.Time, error) {
	var (
		earliestTime time.Time
	)
	if vqa.Status.LastScaleTime != nil {
		earliestTime = vqa.Status.LastScaleTime.Time
	} else {
		// If none found, then this is either a recently created cronJob,
		// or the active/completed info was somehow lost (contract for status
		// in kubernetes says it may need to be recreated), or that we have
		// started a job, but have not noticed it yet (distributed systems can
		// have arbitrary delays).  In any case, use the creation time of the
		// CronJob as last known start time.
		earliestTime = vqa.ObjectMeta.CreationTimestamp.Time
	}
	if vqa.Spec.TidalSpec.StartingDeadlineSeconds != nil {
		// Controller is not going to schedule anything below this point
		schedulingDeadline := now.Add(-time.Second * time.Duration(*vqa.Spec.TidalSpec.StartingDeadlineSeconds))

		if schedulingDeadline.After(earliestTime) {
			earliestTime = schedulingDeadline
		}
	}
	if earliestTime.After(now) {
		return nil, nil
	}

	t, numberOfMissedSchedules, err := getMostRecentScheduleTime(earliestTime, now, schedule)

	if numberOfMissedSchedules > 100 {
		// An object might miss several starts. For example, if
		// controller gets wedged on friday at 5:01pm when everyone has
		// gone home, and someone comes in on tuesday AM and discovers
		// the problem and restarts the controller, then all the hourly
		// jobs, more than 80 of them for one hourly cronJob, should
		// all start running with no further intervention (if the cronJob
		// allows concurrency and late starts).
		//
		// However, if there is a bug somewhere, or incorrect clock
		// on controller's server or apiservers (for setting creationTimestamp)
		// then there could be so many missed start times (it could be off
		// by decades or more), that it would eat up all the CPU and memory
		// of this controller. In that case, we want to not try to list
		// all the missed start times.
		//
		// I've somewhat arbitrarily picked 100, as more than 80,
		// but less than "lots".
		recorder.Eventf(vqa, corev1.EventTypeWarning, "TooManyMissedTimes", "too many missed start times: %d. Set or decrease .spec.startingDeadlineSeconds or check clock skew", numberOfMissedSchedules)
		klog.InfoS("too many missed times", "vqa", vqa.GetName(), "missed times", numberOfMissedSchedules)
	}
	return t, err
}

// getMostRecentScheduleTime returns the latest schedule time between earliestTime and the count of number of
// schedules in between them
func getMostRecentScheduleTime(earliestTime time.Time, now time.Time, schedule cron.Schedule) (*time.Time, int64, error) {
	t1 := schedule.Next(earliestTime)
	t2 := schedule.Next(t1)

	if now.Before(t1) {
		return nil, 0, nil
	}
	if now.Before(t2) {
		return &t1, 1, nil
	}

	// It is possible for cron.ParseStandard("59 23 31 2 *") to return an invalid schedule
	// seconds - 59, minute - 23, hour - 31 (?!)  dom - 2, and dow is optional, clearly 31 is invalid
	// In this case the timeBetweenTwoSchedules will be 0, and we error out the invalid schedule
	timeBetweenTwoSchedules := int64(t2.Sub(t1).Round(time.Second).Seconds())
	if timeBetweenTwoSchedules < 1 {
		return nil, 0, fmt.Errorf("time difference between two schedules less than 1 second")
	}
	timeElapsed := int64(now.Sub(t1).Seconds())
	numberOfMissedSchedules := (timeElapsed / timeBetweenTwoSchedules) + 1
	t := time.Unix(t1.Unix()+((numberOfMissedSchedules-1)*timeBetweenTwoSchedules), 0).UTC()
	return &t, numberOfMissedSchedules, nil
}

// nextScheduledTimeDuration returns the time duration to requeue based on
// the schedule and last schedule time. It adds a 100ms padding to the next requeue to account
// for Network Time Protocol(NTP) time skews. If the time drifts are adjusted which in most
// realistic cases would be around 100s, scheduled cron will still be executed without missing
// the schedule.
func nextScheduledTimeDuration(vqa *autoscalingv1alpha1.VerticalQueueAutoscaler, sched cron.Schedule, now time.Time) *time.Duration {
	earliestTime := vqa.ObjectMeta.CreationTimestamp.Time
	if vqa.Status.LastScaleTime != nil {
		earliestTime = vqa.Status.LastScaleTime.Time
	}
	mostRecentTime, _, err := getMostRecentScheduleTime(earliestTime, now, sched)
	if err != nil {
		// we still have to requeue at some point, so aim for the next scheduling slot from now
		mostRecentTime = &now
	} else if mostRecentTime == nil {
		// no missed schedules since earliestTime
		mostRecentTime = &earliestTime
	}

	t := sched.Next(*mostRecentTime).Add(nextScheduleDelta).Sub(now)

	return &t
}
