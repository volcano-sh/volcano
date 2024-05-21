package sla

import (
	"reflect"
	"testing"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
)

func TestSlaPlugin(t *testing.T) {
	var (
		tm   = time.Hour
		job1 = &api.JobInfo{
			Name:      "job1",
			Namespace: "default",
			Tasks: map[api.TaskID]*api.TaskInfo{
				"0": {
					Name: "job1-ps-0",
				},
				"1": {
					Name: "job1-ps-1",
				},
				"2": {
					Name: "job1-worker-0",
				},
			},

			PodGroup: &api.PodGroup{
				PodGroup: scheduling.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
				},
			},
		}
		job2 = &api.JobInfo{
			Name:      "job1",
			Namespace: "default",
			Tasks: map[api.TaskID]*api.TaskInfo{
				"0": {
					Name: "job1-ps-0",
				},
				"1": {
					Name: "job1-ps-1",
				},
			},
			WaitingTime: &tm,
			PodGroup: &api.PodGroup{
				PodGroup: scheduling.PodGroup{
					ObjectMeta: metav1.ObjectMeta{
						Annotations: map[string]string{},
					},
				},
			},
		}
	)

	tests := []struct {
		uthelper.TestCommonStruct
		arguments           framework.Arguments
		expectedOrder       bool
		expectedEnqueueAble bool
	}{
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:    "sla normal placement",
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			arguments: map[string]interface{}{
				"sla-waiting-time": "3m",
			},
			expectedOrder:       true,
			expectedEnqueueAble: true,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:    "sla error type placement",
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			arguments: map[string]interface{}{
				"sla-waiting-time": 1,
			},
			expectedOrder:       false,
			expectedEnqueueAble: true,
		},
		{
			TestCommonStruct: uthelper.TestCommonStruct{
				Name:    "none placement",
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			arguments:           map[string]interface{}{},
			expectedOrder:       false,
			expectedEnqueueAble: true,
		},
	}

	for _, test := range tests {
		trueValue := true
		t.Run(test.Name, func(t *testing.T) {
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{
							Name:               PluginName,
							EnabledJobOrder:    &trueValue,
							EnabledJobEnqueued: &trueValue,
							Arguments:          test.arguments,
						},
					},
				},
			}
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()
			isOrder := ssn.JobOrderFn(job1, job2)
			if !reflect.DeepEqual(test.expectedOrder, isOrder) {
				t.Errorf("case: %s error,  expect %v, but get %v", test.Name, test.expectedOrder, isOrder)
			}
			isEnqueue := ssn.JobEnqueueable(job1)
			if !reflect.DeepEqual(test.expectedEnqueueAble, isEnqueue) {
				t.Errorf("case: %s error,  expect %v, but get %v", test.Name, test.expectedEnqueueAble, isEnqueue)
			}
		})
	}

}
