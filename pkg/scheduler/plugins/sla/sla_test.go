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
		ttt  = time.Hour
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
			WaitingTime: &ttt,
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
		name string
		uthelper.TestCommonStruct
		arguments           framework.Arguments
		expectedOrder       bool
		expectedEnqueueAble bool
	}{
		{
			name: "sla normal placement",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			arguments: map[string]interface{}{
				"sla-waiting-time": "3m",
			},
			expectedOrder:       true,
			expectedEnqueueAble: true,
		},
		{
			name: "sla error type placement",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			arguments: map[string]interface{}{
				"sla-waiting-time": 1,
			},
			expectedOrder:       false,
			expectedEnqueueAble: true,
		},
		{
			name: "none placement",
			TestCommonStruct: uthelper.TestCommonStruct{
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
			},
			arguments:           map[string]interface{}{},
			expectedOrder:       false,
			expectedEnqueueAble: true,
		},
	}

	trueValue := true
	for _, test := range tests {
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

		isOrder := ssn.JobOrderFn(job1, job2)
		if !reflect.DeepEqual(test.expectedOrder, isOrder) {
			t.Errorf("case: %s error,  expect %v, but get %v", test.name, test.expectedOrder, isOrder)
		}
		isEnqueue := ssn.JobEnqueueable(job1)
		if !reflect.DeepEqual(test.expectedEnqueueAble, isEnqueue) {
			t.Errorf("case: %s error,  expect %v, but get %v", test.name, test.expectedEnqueueAble, isEnqueue)
		}
		test.Close()
	}
}
