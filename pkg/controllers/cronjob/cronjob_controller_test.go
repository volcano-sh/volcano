/*
Copyright 2018 The Kubernetes Authors.
Copyright 2025 The Volcano Authors.

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

package cronjob

import (
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	kubeclient "k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"

	batchv1 "volcano.sh/apis/pkg/apis/batch/v1alpha1"
	volcanoclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	informerfactory "volcano.sh/apis/pkg/client/informers/externalversions"

	"volcano.sh/volcano/pkg/controllers/framework"
)

var (
	sixtySecondsDead int64 = 60
	threeMinutesDead int64 = 180
	longDead         int64 = 60060
)

func createTestJob(cronJob *batchv1.CronJob, spec jobSpec) *batchv1.Job {
	job, err := getJobFromTemplate(cronJob, spec.time)
	if err != nil {
		panic(fmt.Sprintf("error creating job: %v", err))
	}
	job.ObjectMeta.CreationTimestamp = metav1.Time{Time: spec.time}
	job.UID = types.UID("test-uid-" + spec.time.Format("20060102150405"))
	job.Status.State.Phase = spec.status
	job.Status.State.LastTransitionTime = metav1.Time{Time: spec.time.Add(spec.processTime)}
	job.TypeMeta = metav1.TypeMeta{
		Kind:       "Job",
		APIVersion: "batch.volcano.sh/v1alpha1",
	}
	return job
}

func newFakeController() *cronjobcontroller {

	kubeClientSet := kubeclient.NewSimpleClientset()
	vcClientSet := volcanoclient.NewSimpleClientset()
	vcSharedInformers := informerfactory.NewSharedInformerFactory(vcClientSet, 0)

	controller := &cronjobcontroller{}
	opt := &framework.ControllerOption{
		VolcanoClient:           vcClientSet,
		KubeClient:              kubeClientSet,
		VCSharedInformerFactory: vcSharedInformers,
		WorkerNum:               3,
	}
	controller.Initialize(opt)
	controller.cronjobClient = &fakeCronjobClient{}
	controller.jobClient = &fakeJobClient{}
	return controller
}
func setupTestController() (*cronjobcontroller, *record.FakeRecorder) {
	kubeClientSet := kubeclient.NewSimpleClientset()
	vcClientSet := volcanoclient.NewSimpleClientset()
	vcSharedInformers := informerfactory.NewSharedInformerFactory(vcClientSet, 0)
	controller := &cronjobcontroller{}
	opt := &framework.ControllerOption{
		VolcanoClient:           vcClientSet,
		KubeClient:              kubeClientSet,
		VCSharedInformerFactory: vcSharedInformers,
		WorkerNum:               3,
	}
	controller.Initialize(opt)
	controller.cronjobClient = &fakeCronjobClient{}
	controller.jobClient = &fakeJobClient{}
	recorder := record.NewFakeRecorder(10)
	controller.recorder = recorder
	return controller, recorder
}

func justTen() time.Time {
	T, err := time.Parse(time.RFC3339, "2025-03-11T10:00:00Z")
	if err != nil {
		panic("test setup error")
	}
	return T
}
func tomorrowTen() time.Time {
	T, err := time.Parse(time.RFC3339, "2025-03-12T10:00:00Z")
	if err != nil {
		panic("test setup error")
	}
	return T
}
func fiveMinutesAfterTen() time.Time {
	return justTen().Add(5 * time.Minute)
}

func threeMinutesAfterTen() time.Time {
	return justTen().Add(3 * time.Minute)
}

func sevenMinutesAfterTen() time.Time {
	return justTen().Add(7 * time.Minute)
}

func tenHoursAfterTen() time.Time {
	return justTen().Add(10 * time.Hour)
}
func durationPtr(d time.Duration) *time.Duration {
	return &d
}
func getEventsFromRecorder(recorder *record.FakeRecorder) []string {
	close(recorder.Events)
	var events []string
	for event := range recorder.Events {
		events = append(events, event)
	}
	return events
}
func TestSyncCronJob(t *testing.T) {
	testCases := []struct {
		name             string
		now              time.Time
		cronjob          cjSpec
		createJobErr     error
		getJobErr        error
		jobs             []jobSpec
		lastScheduleTime *metav1.Time
		expect           testExpect
	}{
		{
			name: "Abnormal situation, error schedule",
			cronjob: cjSpec{
				schedule: "wrong shcedlue",
			},
			expect: testExpect{
				expectRequeueAfter: nil,
				expectUpdateStatus: false,
				expectError:        true,
			},
		},
		{
			name: "Abnormal situation, error TimeZone",
			cronjob: cjSpec{
				timeZone: "error TZ",
			},
			expect: testExpect{
				expectRequeueAfter: nil,
				expectUpdateStatus: false,
				expectError:        true,
				expectEventsNum:    1,
			},
		},
		{
			name: "Abnormal situation, suspend",
			cronjob: cjSpec{
				suspend: true,
			},
			expect: testExpect{
				expectRequeueAfter: nil,
				expectUpdateStatus: false,
				expectError:        false,
			},
		},
		{
			name: "First run, is not time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			now: threeMinutesAfterTen(),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(2*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: nil,
			},
		},
		{
			name: "First run, is not time, Forbid",
			cronjob: cjSpec{
				concurrency: "Forbid",
			},
			now: threeMinutesAfterTen(),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(2*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: nil,
			},
		},
		{
			name: "First run, is not time, Replace",
			cronjob: cjSpec{
				concurrency: "Replace",
			},
			now: threeMinutesAfterTen(),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(2*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: nil,
			},
		},
		{
			name: "First run, is not time in TZ",
			now:  justTen(),
			cronjob: cjSpec{
				concurrency: "Allow",
				schedule:    "0 10 * * *",
				timeZone:    "Pacific/Honolulu",
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(10*time.Hour + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: nil,
			},
		},
		{
			name: "First run, is time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			now: fiveMinutesAfterTen(),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "First run, is time, Forbid",
			cronjob: cjSpec{
				concurrency: "Forbid",
			},
			now: fiveMinutesAfterTen(),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "First run, is time, Replace",
			cronjob: cjSpec{
				concurrency: "Forbid",
			},
			now: fiveMinutesAfterTen(),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "First run, is time, has startingDeadlineSeconds, Allow",
			cronjob: cjSpec{
				concurrency:             "Allow",
				startingDeadlineSeconds: &sixtySecondsDead,
			},
			now: fiveMinutesAfterTen(),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "First run, is time in TZ, Allow",
			now:  tenHoursAfterTen(),
			cronjob: cjSpec{
				concurrency: "Allow",
				schedule:    "0 10 * * *",
				timeZone:    "Pacific/Honolulu",
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(24*time.Hour + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: tenHoursAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "First run, is time, uses schedule's TZ despite conflict with TimeZone setting",
			now:  tenHoursAfterTen(),
			cronjob: cjSpec{
				schedule: "TZ=UTC 0 20 * * *",
				timeZone: "Pacific/Honolulu",
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(24*time.Hour + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: tenHoursAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        2,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "First run, miss Time, no startingDeadlineSeconds, Allow",
			now:  sevenMinutesAfterTen(),
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "First run, miss Time, no miss deadline, Allow",
			now:  sevenMinutesAfterTen(),
			cronjob: cjSpec{
				concurrency:             "Allow",
				startingDeadlineSeconds: &threeMinutesDead,
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "First run, miss deadline, Allow",
			now:  sevenMinutesAfterTen(),
			cronjob: cjSpec{
				concurrency:             "Allow",
				startingDeadlineSeconds: &sixtySecondsDead,
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: nil,
			},
		},
		{
			name: "First run, manymiss, no startingDeadlineSeconds, Allow",
			now:  tenHoursAfterTen(),
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: tenHoursAfterTen()},
				expectEventsNum:        2,
				expectInActiveNum:      1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "First run, manymiss, has startingDeadlineSeconds, Allow",
			now:  tomorrowTen(),
			cronjob: cjSpec{
				concurrency:             "Allow",
				schedule:                "* * * * *",
				startingDeadlineSeconds: &longDead,
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(1*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: tomorrowTen()},
				expectEventsNum:        2,
				expectInActiveNum:      1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "First run, is time, job exist and done, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			now:          fiveMinutesAfterTen(),
			createJobErr: errors.NewAlreadyExists(schema.GroupResource{Resource: "job", Group: "batch.volcano.sh"}, ""),
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        fiveMinutesAfterTen(),
					inLister:    false,
					inClient:    true,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: nil,
				expectInActiveNum:      0,
				expectEventsNum:        0,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "First run, is time, job exist but not owned by this CronJob, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			now:          fiveMinutesAfterTen(),
			createJobErr: errors.NewAlreadyExists(schema.GroupResource{Resource: "job", Group: "batch.volcano.sh"}, ""),
			jobs: []jobSpec{
				{
					status:       batchv1.Running,
					time:         fiveMinutesAfterTen(),
					inLister:     false,
					inClient:     true,
					processTime:  time.Minute,
					notOwnedByCj: true,
				},
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: nil,
				expectInActiveNum:      0,
				expectEventsNum:        1,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "First run, is time, job in active but not in lister, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			now: fiveMinutesAfterTen(),
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        fiveMinutesAfterTen(),
					inLister:    false,
					inClient:    false,
					active:      true,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        2,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     1,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "First run, is time, nameSpace is terminating",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			now: fiveMinutesAfterTen(),
			createJobErr: &errors.StatusError{
				ErrStatus: metav1.Status{
					Status: metav1.StatusFailure,
					Reason: metav1.StatusReasonInvalid,
					Details: &metav1.StatusDetails{
						Causes: []metav1.StatusCause{
							{
								Type:    corev1.NamespaceTerminatingCause,
								Message: "namespace is being terminated",
								Field:   "metadata.namespace",
							},
						},
					},
				},
			},
			expect: testExpect{
				expectRequeueAfter:     nil,
				expectUpdateStatus:     false,
				expectError:            true,
				expectLastScheduleTime: nil,
				expectInActiveNum:      0,
				expectEventsNum:        1,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "First run, @every schedule, not time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
				schedule:    "@every 1h",
			},
			now: fiveMinutesAfterTen(),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(55*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: nil,
				expectInActiveNum:      0,
				expectEventsNum:        0,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "First run, @every schedule, is time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
				schedule:    "@every 1h",
			},
			now: justTen().Add(time.Hour),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(time.Hour + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: justTen().Add(time.Hour)},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     1,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "First run, @every schedule, is time, miss deadline, Allow",
			cronjob: cjSpec{
				concurrency:             "Allow",
				schedule:                "@every 1h",
				startingDeadlineSeconds: &sixtySecondsDead,
			},
			now: justTen().Add(time.Hour + 30*time.Minute),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(30*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: nil,
				expectInActiveNum:      0,
				expectEventsNum:        0,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "First run, @every schedule, is time, no miss deadline, Allow",
			cronjob: cjSpec{
				concurrency:             "Allow",
				schedule:                "@every 1h",
				startingDeadlineSeconds: &longDead,
			},
			now: justTen().Add(time.Hour + 30*time.Minute),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(30*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: justTen().Add(time.Hour)},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     1,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, @every schedule, pre all done, is not time, Allow",
			now:  justTen().Add(30 * time.Minute),
			cronjob: cjSpec{
				concurrency:             "Allow",
				schedule:                "@every 1h",
				startingDeadlineSeconds: &longDead,
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(30*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: nil,
				expectInActiveNum:      0,
				expectEventsNum:        0,
				expectLastSuccessTime:  &metav1.Time{Time: justTen().Add(time.Minute)},
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, @every schedule, pre all done, is time, Allow",
			now:  justTen().Add(time.Hour),
			cronjob: cjSpec{
				concurrency:             "Allow",
				schedule:                "@every 1h",
				startingDeadlineSeconds: &longDead,
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(time.Hour + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: justTen().Add(time.Hour)},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectLastSuccessTime:  &metav1.Time{Time: justTen().Add(time.Minute)},
				expectCreateJobNum:     1,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, @every schedule, pre active, is not time, Allow",
			now:  justTen().Add(30 * time.Minute),
			cronjob: cjSpec{
				concurrency:             "Allow",
				schedule:                "@every 1h",
				startingDeadlineSeconds: &longDead,
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        justTen(),
					inLister:    true,
					inClient:    true,
					active:      true,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(30*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: nil,
				expectInActiveNum:      1,
				expectEventsNum:        0,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, @every schedule, pre activee all done, is time, Allow",
			now:  justTen().Add(time.Hour),
			cronjob: cjSpec{
				concurrency:             "Allow",
				schedule:                "@every 1h",
				startingDeadlineSeconds: &longDead,
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        justTen(),
					inLister:    true,
					inClient:    true,
					active:      true,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(time.Hour + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: justTen().Add(time.Hour)},
				expectInActiveNum:      2,
				expectEventsNum:        1,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     1,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "noy first run, successfulJobHistoryLimit is 0",
			now:  justTen().Add(7 * time.Minute),
			cronjob: cjSpec{
				concurrency:               "Allow",
				successfulJobHistoryLimit: ptr.To[int32](0),
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
			expect: testExpect{

				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      0,
				expectEventsNum:        1,
				expectLastSuccessTime:  &metav1.Time{Time: fiveMinutesAfterTen().Add(time.Minute)},
				expectCreateJobNum:     0,
				expectDeleteJobNum:     1,
			},
		},
		//TODO: 增加默认值
		// {
		// 	name:        "noy first run, successfulJobHistoryLimit is nil",
		// 	concurrency: "Allow",
		// 	now:         justTen().Add(22 * time.Minute),
		// 	jobs: []struct {
		// 		status      batchv1.JobPhase
		// 		time        time.Time
		// 		inLister    bool
		// 		inClient    bool
		// 		active      bool
		// 		processTime time.Duration
		// 	}{
		// 		{
		// 			status:      batchv1.Completed,
		// 			time:        fiveMinutesAfterTen(),
		// 			inLister:    true,
		// 			inClient:    false,
		// 			active:      false,
		// 			processTime: time.Minute,
		// 		},
		// 		{
		// 			status:      batchv1.Completed,
		// 			time:        fiveMinutesAfterTen().Add(5 * time.Minute),
		// 			inLister:    true,
		// 			inClient:    false,
		// 			active:      false,
		// 			processTime: time.Minute,
		// 		},
		// 		{
		// 			status:      batchv1.Completed,
		// 			time:        fiveMinutesAfterTen().Add(10 * time.Minute),
		// 			inLister:    true,
		// 			inClient:    false,
		// 			active:      false,
		// 			processTime: time.Minute,
		// 		},
		// 		{
		// 			status:      batchv1.Completed,
		// 			time:        fiveMinutesAfterTen().Add(15 * time.Minute),
		// 			inLister:    true,
		// 			inClient:    false,
		// 			active:      false,
		// 			processTime: time.Minute,
		// 		},
		// 	},
		// 	lastScheduleTime:       &metav1.Time{Time: fiveMinutesAfterTen().Add(15 * time.Minute)},
		// 	expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
		// 	expectUpdateStatus:     true,
		// 	expectError:            false,
		// 	expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen().Add(15 * time.Minute)},
		// 	expectInActiveNum:      0,
		// 	expectEventsNum:        1,
		// 	expectLastSuccessTime:  &metav1.Time{Time: fiveMinutesAfterTen().Add(16 * time.Minute)},
		// 	expectCreateJobNum:     0,
		// 	expectDeleteJobNum:     1,
		// },
		{
			name: "noy first run, successfulJobHistoryLimit is 5",
			now:  justTen().Add(22 * time.Minute),
			cronjob: cjSpec{
				concurrency:               "Allow",
				successfulJobHistoryLimit: ptr.To[int32](5),
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen().Add(15 * time.Minute)},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
				{
					status:      batchv1.Completed,
					time:        fiveMinutesAfterTen().Add(5 * time.Minute),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
				{
					status:      batchv1.Completed,
					time:        fiveMinutesAfterTen().Add(10 * time.Minute),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
				{
					status:      batchv1.Completed,
					time:        fiveMinutesAfterTen().Add(15 * time.Minute),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			expect: testExpect{

				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen().Add(15 * time.Minute)},
				expectInActiveNum:      0,
				expectEventsNum:        0,
				expectLastSuccessTime:  &metav1.Time{Time: fiveMinutesAfterTen().Add(16 * time.Minute)},
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, pre all done, is not time, Allow",
			now:  threeMinutesAfterTen(),
			cronjob: cjSpec{
				concurrency:             "Allow",
				startingDeadlineSeconds: &longDead,
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectRequeueAfter:    durationPtr(2*time.Minute + nextScheduleDelta),
				expectUpdateStatus:    true,
				expectError:           false,
				expectLastSuccessTime: &metav1.Time{Time: justTen().Add(60 * time.Second)},
			},
		},
		{
			name: "not first run, pre all done, is not time, Forbid",
			now:  threeMinutesAfterTen(),
			cronjob: cjSpec{
				concurrency:             "Forbid",
				startingDeadlineSeconds: &longDead,
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectRequeueAfter:    durationPtr(2*time.Minute + nextScheduleDelta),
				expectUpdateStatus:    true,
				expectError:           false,
				expectLastSuccessTime: &metav1.Time{Time: justTen().Add(60 * time.Second)},
			},
		},
		{
			name: "not first run, pre all done, is not time, Replace",
			now:  threeMinutesAfterTen(),
			cronjob: cjSpec{
				concurrency:             "Replace",
				startingDeadlineSeconds: &longDead,
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectRequeueAfter:    durationPtr(2*time.Minute + nextScheduleDelta),
				expectUpdateStatus:    true,
				expectError:           false,
				expectLastSuccessTime: &metav1.Time{Time: justTen().Add(60 * time.Second)},
			},
		},
		{
			name: "not first run, pre all done, is not time, Replace",
			now:  threeMinutesAfterTen(),
			cronjob: cjSpec{
				concurrency:             "Replace",
				startingDeadlineSeconds: &longDead,
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Failed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectRequeueAfter:    durationPtr(2*time.Minute + nextScheduleDelta),
				expectUpdateStatus:    false,
				expectError:           false,
				expectLastSuccessTime: nil,
			},
		},
		{
			name: "not first run, pre all done, one suc and one fail, is not time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			now:              justTen().Add(7 * time.Minute),
			lastScheduleTime: &metav1.Time{Time: justTen().Add(5 * time.Minute)},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
				{
					status:      batchv1.Failed,
					time:        justTen().Add(5 * time.Minute),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectLastScheduleTime: &metav1.Time{Time: justTen().Add(5 * time.Minute)},
				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastSuccessTime:  &metav1.Time{Time: justTen().Add(60 * time.Second)},
			},
		},

		{
			name: "not first run, pre all done but in active, is not time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      true,
					processTime: time.Minute,
				},
			},
			now: threeMinutesAfterTen(),
			expect: testExpect{
				expectRequeueAfter:    durationPtr(2*time.Minute + nextScheduleDelta),
				expectUpdateStatus:    true,
				expectError:           false,
				expectInActiveNum:     0,
				expectLastSuccessTime: &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectEventsNum:       1,
			},
		},
		{
			name: "not first run, pre all done but in active, is not time, Forbid",
			cronjob: cjSpec{
				concurrency: "Forbid",
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      true,
					processTime: time.Minute,
				},
			},
			now: threeMinutesAfterTen(),
			expect: testExpect{
				expectRequeueAfter:    durationPtr(2*time.Minute + nextScheduleDelta),
				expectUpdateStatus:    true,
				expectError:           false,
				expectInActiveNum:     0,
				expectLastSuccessTime: &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectEventsNum:       1,
			},
		},
		{
			name: "not first run, pre all done but in active, is not time, Replace",
			cronjob: cjSpec{
				concurrency: "Replace",
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      true,
					processTime: time.Minute,
				},
			},
			now: threeMinutesAfterTen(),
			expect: testExpect{
				expectRequeueAfter:    durationPtr(2*time.Minute + nextScheduleDelta),
				expectUpdateStatus:    true,
				expectError:           false,
				expectInActiveNum:     0,
				expectLastSuccessTime: &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectEventsNum:       1,
			},
		},
		{
			name: "not first run, pre all done, is time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			now: justTen().Add(5 * time.Minute),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastSuccessTime:  &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "not first run, pre all done, is time, Forbid",
			cronjob: cjSpec{
				concurrency: "Forbid",
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			now: justTen().Add(5 * time.Minute),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastSuccessTime:  &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "not first run, pre all done, is time, Replace",
			cronjob: cjSpec{
				concurrency: "Replace",
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			now: justTen().Add(5 * time.Minute),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastSuccessTime:  &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "not first run, pre all done, is time, suspend",
			cronjob: cjSpec{
				concurrency: "Allow",
				suspend:     true,
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			now: justTen().Add(5 * time.Minute),
			expect: testExpect{
				expectRequeueAfter:    nil,
				expectUpdateStatus:    true,
				expectError:           false,
				expectLastSuccessTime: &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectInActiveNum:     0,
				expectEventsNum:       0,
				expectCreateJobNum:    0,
			},
		},
		{
			name: "not first run, pre all done fail, is time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow"},
			jobs: []jobSpec{
				{
					status:      batchv1.Failed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			now: justTen().Add(5 * time.Minute),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1},
		},
		{
			name: "not first run, pre all done, one suc and one fail, is time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow"},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
				{
					status:      batchv1.Failed,
					time:        justTen().Add(5 * time.Minute),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			now: justTen().Add(15 * time.Minute),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastSuccessTime:  &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectLastScheduleTime: &metav1.Time{Time: justTen().Add(15 * time.Minute)},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1},
		},
		{
			name: "not first run, pre all done but in active, is time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      true,
					processTime: time.Minute,
				},
			},
			now: justTen().Add(5 * time.Minute),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastSuccessTime:  &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        2,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "not first run, pre all done but in active, is time, Forbid",
			cronjob: cjSpec{
				concurrency: "Forbid",
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      true,
					processTime: time.Minute,
				},
			},
			now: justTen().Add(5 * time.Minute),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastSuccessTime:  &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        2,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "not first run, pre all done but in active, is time, Replace",
			cronjob: cjSpec{
				concurrency: "Replace",
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      true,
					processTime: time.Minute,
				},
			},
			now: justTen().Add(5 * time.Minute),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastSuccessTime:  &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        2,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "not first run, pre all done, job exist and in active, is time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			now: justTen().Add(5 * time.Minute),
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    true,
					active:      true,
					processTime: time.Minute,
				},
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			createJobErr: errors.NewAlreadyExists(schema.GroupResource{Resource: "job", Group: "batch.volcano.sh"}, ""),
			expect: testExpect{
				expectRequeueAfter:    durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:    true,
				expectError:           false,
				expectLastSuccessTime: &metav1.Time{Time: justTen().Add(1 * time.Minute)},
				expectInActiveNum:     1,
				expectEventsNum:       0,
				expectCreateJobNum:    0,
			},
		},
		{
			name: "not first run, pre all done, miss deadline, Allow",
			cronjob: cjSpec{
				concurrency:             "Allow",
				startingDeadlineSeconds: &sixtySecondsDead,
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},

			now: justTen().Add(7 * time.Minute),
			expect: testExpect{
				expectRequeueAfter:    durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:    true,
				expectError:           false,
				expectLastSuccessTime: &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectInActiveNum:     0,
				expectEventsNum:       0,
				expectCreateJobNum:    0,
			},
		},
		{
			name: "not first run, pre all done, no miss deadline, Allow",
			cronjob: cjSpec{
				concurrency:             "Allow",
				startingDeadlineSeconds: &threeMinutesDead,
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        justTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			now: justTen().Add(7 * time.Minute),
			expect: testExpect{
				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastSuccessTime:  &metav1.Time{Time: justTen().Add(60 * time.Second)},
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectCreateJobNum:     1,
			},
		},
		{
			name: "not first run, pre active, not time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			now: justTen().Add(7 * time.Minute),
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    false,
					active:      true,
					processTime: time.Minute,
				},
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        0,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, pre active, not time, Forbid",
			cronjob: cjSpec{
				concurrency: "Forbid",
			},
			now: justTen().Add(7 * time.Minute),
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    false,
					active:      true,
					processTime: time.Minute,
				},
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        0,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, pre active, not time, Replace",
			cronjob: cjSpec{
				concurrency: "Replace",
			},
			now: justTen().Add(7 * time.Minute),
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    false,
					active:      true,
					processTime: time.Minute,
				},
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        0,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, pre active, not time, suspend",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			now: justTen().Add(7 * time.Minute),
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    false,
					active:      true,
					processTime: time.Minute,
				},
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        0,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, pre active, is time, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			now: justTen().Add(10 * time.Minute),
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    true,
					active:      true,
					processTime: time.Minute,
				},
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: justTen().Add(10 * time.Minute)},
				expectInActiveNum:      2,
				expectEventsNum:        1,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     1,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, pre active, is time, Forbid",
			cronjob: cjSpec{
				concurrency: "Forbid",
			},
			now: justTen().Add(10 * time.Minute),
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    true,
					active:      true,
					processTime: time.Minute,
				},
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        1,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, pre active, is time, Replace",
			cronjob: cjSpec{
				concurrency: "Replace",
			},
			now: justTen().Add(10 * time.Minute),
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    true,
					active:      true,
					processTime: time.Minute,
				},
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: justTen().Add(10 * time.Minute)},
				expectInActiveNum:      1,
				expectEventsNum:        3,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     1,
				expectDeleteJobNum:     1,
			},
		},
		{
			name: "not first run, pre active, is time, suspend",
			cronjob: cjSpec{
				concurrency: "Allow",
				suspend:     true,
			},
			now: justTen().Add(10 * time.Minute),

			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    true,
					active:      true,
					processTime: time.Minute,
				},
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
			expect: testExpect{
				expectRequeueAfter:     nil,
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        0,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, pre active, is time, Replace, get job failed",
			cronjob: cjSpec{
				concurrency: "Replace",
			},
			now: justTen().Add(10 * time.Minute),
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    true,
					active:      true,
					processTime: time.Minute,
				},
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
			getJobErr:        errors.NewBadRequest("get job error"),
			expect: testExpect{
				expectRequeueAfter:     nil,
				expectUpdateStatus:     false,
				expectError:            true,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        2,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, pre active, is time, miss deadline",
			cronjob: cjSpec{
				concurrency:             "Allow",
				startingDeadlineSeconds: &sixtySecondsDead,
			},
			now: justTen().Add(12 * time.Minute),
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    true,
					active:      true,
					processTime: time.Minute,
				},
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     false,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        0,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     0,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, pre active, is time, no miss deadline",
			cronjob: cjSpec{
				concurrency:             "Allow",
				startingDeadlineSeconds: &threeMinutesDead,
			},
			now: justTen().Add(12 * time.Minute),
			jobs: []jobSpec{
				{
					status:      batchv1.Running,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    true,
					active:      true,
					processTime: time.Minute,
				},
			},
			lastScheduleTime: &metav1.Time{Time: fiveMinutesAfterTen()},
			expect: testExpect{

				expectRequeueAfter:     durationPtr(3*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: justTen().Add(10 * time.Minute)},
				expectInActiveNum:      2,
				expectEventsNum:        1,
				expectLastSuccessTime:  nil,
				expectCreateJobNum:     1,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, pre all done, manymiss, Allow",
			cronjob: cjSpec{
				concurrency: "Allow",
			},
			now: tenHoursAfterTen(),
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(5*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: tenHoursAfterTen()},
				expectInActiveNum:      1,
				expectEventsNum:        2,
				expectLastSuccessTime:  &metav1.Time{Time: (justTen().Add(6 * time.Minute))},
				expectCreateJobNum:     1,
				expectDeleteJobNum:     0,
			},
		},
		{
			name: "not first run, pre all done, manymiss, has startingDeadlineSeconds, Allow",
			now:  tomorrowTen(),
			cronjob: cjSpec{
				concurrency:             "Allow",
				schedule:                "* * * * *",
				startingDeadlineSeconds: &longDead,
			},
			jobs: []jobSpec{
				{
					status:      batchv1.Completed,
					time:        fiveMinutesAfterTen(),
					inLister:    true,
					inClient:    false,
					active:      false,
					processTime: time.Minute,
				},
			},
			expect: testExpect{
				expectRequeueAfter:     durationPtr(1*time.Minute + nextScheduleDelta),
				expectUpdateStatus:     true,
				expectError:            false,
				expectLastScheduleTime: &metav1.Time{Time: tomorrowTen()},
				expectInActiveNum:      1,
				expectEventsNum:        2,
				expectLastSuccessTime:  &metav1.Time{Time: (justTen().Add(6 * time.Minute))},
				expectCreateJobNum:     1,
				expectDeleteJobNum:     0,
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cronJob := createTestCronJob(tc.cronjob)
			controller, recorder := setupTestController()
			fakeJobClient := setupFakeJobClient(controller, tc.createJobErr, tc.getJobErr)
			if !tc.now.IsZero() {
				controller.now = func() time.Time { return tc.now }
			}
			jobsByCronJob := prepareTestJobs(t, cronJob, tc.jobs, fakeJobClient)
			setupCronJobStatus(cronJob, tc.lastScheduleTime, tc.jobs)
			fakeCronjobClient, _ := controller.cronjobClient.(*fakeCronjobClient)
			fakeCronjobClient.CronJob = cronJob
			requeueAfter, updateStatus, syncErr := controller.syncCronJob(cronJob, jobsByCronJob)

			validateTestResults(t, tc.expect, cronJob, fakeJobClient, recorder, requeueAfter, updateStatus, syncErr)
		})
	}
}

type jobSpec struct {
	status       batchv1.JobPhase
	time         time.Time
	inLister     bool
	inClient     bool
	active       bool
	processTime  time.Duration
	notOwnedByCj bool
}
type cjSpec struct {
	schedule                  string
	timeZone                  string
	suspend                   bool
	concurrency               batchv1.ConcurrencyPolicy
	creationTime              metav1.Time
	startingDeadlineSeconds   *int64
	successfulJobHistoryLimit *int32
	failedJobsHistoryLimit    *int32
}
type testExpect struct {
	expectRequeueAfter     *time.Duration
	expectUpdateStatus     bool
	expectError            bool
	expectLastScheduleTime *metav1.Time
	expectInActiveNum      int
	expectEventsNum        int
	expectLastSuccessTime  *metav1.Time
	expectCreateJobNum     int
	expectDeleteJobNum     int
}

func createTestCronJob(tc cjSpec) *batchv1.CronJob {
	cronJob := &batchv1.CronJob{
		ObjectMeta: metav1.ObjectMeta{
			Name:              "vccronjob",
			Namespace:         "volcano",
			UID:               types.UID("test-uid-123"),
			CreationTimestamp: metav1.Time{Time: justTen()},
		},
		Spec: batchv1.CronJobSpec{
			Schedule:                   "*/5 * * * *",
			TimeZone:                   stringPtr(tc.timeZone),
			Suspend:                    &tc.suspend,
			ConcurrencyPolicy:          "Allow",
			StartingDeadlineSeconds:    tc.startingDeadlineSeconds,
			SuccessfulJobsHistoryLimit: tc.successfulJobHistoryLimit,
			FailedJobsHistoryLimit:     tc.failedJobsHistoryLimit,
			JobTemplate: batchv1.JobTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      map[string]string{"volcano": "yes"},
					Annotations: map[string]string{"cronjob": "yes"},
				},
			},
		},
	}
	if tc.schedule != "" {
		cronJob.Spec.Schedule = tc.schedule
	}
	if tc.concurrency != "" {
		cronJob.Spec.ConcurrencyPolicy = tc.concurrency
	}
	if !tc.creationTime.IsZero() {
		cronJob.ObjectMeta.CreationTimestamp = tc.creationTime
	}
	return cronJob
}

func setupFakeJobClient(controller *cronjobcontroller, createErr, getErr error) *fakeJobClient {
	fakeClient, _ := controller.jobClient.(*fakeJobClient)
	fakeClient.CreateErr = createErr
	fakeClient.Err = getErr
	return fakeClient
}

func prepareTestJobs(t *testing.T, cronJob *batchv1.CronJob, jobs []jobSpec, fakeClient *fakeJobClient) []*batchv1.Job {
	var jobsByCronJob []*batchv1.Job

	for i, spec := range jobs {
		job := createTestJob(cronJob, spec)

		if i == 0 && spec.inClient {
			fakeClient.Job = job
		}

		if spec.inLister {
			jobsByCronJob = append(jobsByCronJob, job)
		}
		if spec.notOwnedByCj {
			job.ObjectMeta.OwnerReferences = []metav1.OwnerReference{}
		}
	}

	return jobsByCronJob
}

func setupCronJobStatus(cronJob *batchv1.CronJob, lastScheduleTime *metav1.Time, jobSpecs []jobSpec) {
	if lastScheduleTime != nil {
		cronJob.Status.LastScheduleTime = lastScheduleTime
	}

	for _, spec := range jobSpecs {
		if spec.active {
			job := createTestJob(cronJob, spec)
			ref, err := getRef(job)
			if err != nil {
				continue
			}
			cronJob.Status.Active = append(cronJob.Status.Active, *ref)
		}
	}
}

func validateTestResults(t *testing.T, expect testExpect, cronJob *batchv1.CronJob,
	fakeClient *fakeJobClient, recorder *record.FakeRecorder,
	requeueAfter *time.Duration, updateStatus bool, syncErr error) {

	if expect.expectError {
		require.Error(t, syncErr, "Expected error but got nil")
	} else {
		require.NoError(t, syncErr, "Expected no error but got error")
	}

	if expect.expectRequeueAfter == nil {
		require.Nil(t, requeueAfter, "Expected nil requeue time")
	} else {
		require.NotNil(t, requeueAfter, "Requeue time should not be nil")
		require.Equal(t, *expect.expectRequeueAfter, *requeueAfter, "Requeue time mismatch")
	}

	require.Equal(t, expect.expectUpdateStatus, updateStatus, "Status update mismatch")

	validateTimeField(t, "LastScheduleTime", expect.expectLastScheduleTime, cronJob.Status.LastScheduleTime)
	validateTimeField(t, "LastSuccessfulTime", expect.expectLastSuccessTime, cronJob.Status.LastSuccessfulTime)

	require.Len(t, cronJob.Status.Active, expect.expectInActiveNum, "Active jobs count mismatch")

	actualEvents := getEventsFromRecorder(recorder)
	require.Len(t, actualEvents, expect.expectEventsNum, "Events count mismatch")

	require.Len(t, fakeClient.Jobs, expect.expectCreateJobNum, "Created jobs count mismatch")
	for _, job := range fakeClient.Jobs {
		validateControllerRef(t, job, cronJob)
	}

	require.Len(t, fakeClient.DeleteJobName, expect.expectDeleteJobNum, "Deleted jobs count mismatch")
}

func validateTimeField(t *testing.T, fieldName string, expected, actual *metav1.Time) {
	if expected == nil {
		require.Nil(t, actual, "Expected %s to be nil", fieldName)
	} else {
		require.NotNil(t, actual, "Expected %s to be not nil", fieldName)
		require.True(t, expected.Equal(actual),
			"%s mismatch: expected %v, got %v", fieldName, expected, actual)
	}
}

func validateControllerRef(t *testing.T, job batchv1.Job, cronJob *batchv1.CronJob) {
	controllerRef := metav1.GetControllerOf(&job)
	require.NotNil(t, controllerRef, "Job should have ControllerRef")

	assert.Equal(t, "batch.volcano.sh/v1alpha1", controllerRef.APIVersion)
	assert.Equal(t, "CronJob", controllerRef.Kind)
	assert.Equal(t, cronJob.Name, controllerRef.Name)
	assert.Equal(t, cronJob.UID, controllerRef.UID)
	require.NotNil(t, controllerRef.Controller, "Controller field should be set")
	assert.True(t, *controllerRef.Controller, "Controller field should be true")
}

func stringPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
