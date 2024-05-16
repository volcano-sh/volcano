/*
Copyright 2021 The Volcano Authors.

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

package tasktopology

import (
	"fmt"
	"reflect"
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/volcano/pkg/scheduler/api"
)

func Test_readTopologyFromPgAnnotations(t *testing.T) {
	cases := []struct {
		description string
		job         *api.JobInfo
		topology    *TaskTopology
		err         error
	}{
		{
			description: "correct annotation",
			job: &api.JobInfo{
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
					"3": {
						Name: "job1-worker-1",
					},
					"4": {
						Name: "job1-chief-0",
					},
					"5": {
						Name: "job1-evaluator-0",
					},
				},
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								JobAffinityAnnotations:     "ps,worker;ps,chief",
								JobAntiAffinityAnnotations: "ps;worker,chief",
								TaskOrderAnnotations:       "ps,worker,chief,evaluator",
							},
						},
					},
				},
			},
			topology: &TaskTopology{
				Affinity: [][]string{
					{
						"ps",
						"worker",
					},
					{
						"ps",
						"chief",
					},
				},
				AntiAffinity: [][]string{
					{
						"ps",
					},
					{
						"worker",
						"chief",
					},
				},
				TaskOrder: []string{
					"ps",
					"worker",
					"chief",
					"evaluator",
				},
			},
			err: nil,
		},
		{
			description: "correct annotation with tasks whose names contain `-`",
			job: &api.JobInfo{
				Name:      "job1",
				Namespace: "default",
				Tasks: map[api.TaskID]*api.TaskInfo{
					"0": {
						Name: "job1-ps-some-0",
					},
					"1": {
						Name: "job1-ps-some-1",
					},
					"2": {
						Name: "job1-worker-another-some-0",
					},
					"3": {
						Name: "job1-worker-another-some-1",
					},
					"4": {
						Name: "job1-chief-kk-0",
					},
					"5": {
						Name: "job1-evaluator-tt-0",
					},
				},
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								JobAffinityAnnotations:     "ps-some,worker-another-some;ps-some,chief-kk",
								JobAntiAffinityAnnotations: "ps-some;worker-another-some,chief-kk",
								TaskOrderAnnotations:       "ps-some,worker-another-some,chief-kk,evaluator-tt",
							},
						},
					},
				},
			},
			topology: &TaskTopology{
				Affinity: [][]string{
					{
						"ps-some",
						"worker-another-some",
					},
					{
						"ps-some",
						"chief-kk",
					},
				},
				AntiAffinity: [][]string{
					{
						"ps-some",
					},
					{
						"worker-another-some",
						"chief-kk",
					},
				},
				TaskOrder: []string{
					"ps-some",
					"worker-another-some",
					"chief-kk",
					"evaluator-tt",
				},
			},
			err: nil,
		},
		{
			description: "nil annotation",
			job: &api.JobInfo{
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: nil,
						},
					},
				},
			},
			topology: nil,
			err:      nil,
		},
		{
			description: "invalid annotation",
			job: &api.JobInfo{
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
					"3": {
						Name: "job1-worker-1",
					},
					"4": {
						Name: "job1-chief-0",
					},
					"5": {
						Name: "job1-evaluator-0",
					},
				},
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								JobAffinityAnnotations:     "$%GF$^trtqwrg^",
								JobAntiAffinityAnnotations: "ps;worker,chief;#@FGEW^vfa897bgs;;",
								TaskOrderAnnotations:       "ps,worker,chief,evaluator,,,",
							},
						},
					},
				},
			},
			topology: nil,
			err:      fmt.Errorf("task %s do not exist in job <%s/%s>", "$%GF$^trtqwrg^", "default", "job1"),
		},
		{
			description: "invalid task name",
			job: &api.JobInfo{
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
					"3": {
						Name: "job1-worker-1",
					},
				},
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								JobAffinityAnnotations:     "ps,worker",
								JobAntiAffinityAnnotations: "ps",
								TaskOrderAnnotations:       "ps,worker,chief",
							},
						},
					},
				},
			},
			topology: nil,
			err:      fmt.Errorf("task %s do not exist in job <%s/%s>", "chief", "default", "job1"),
		},
		{
			description: "duplicated task name",
			job: &api.JobInfo{
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
					"3": {
						Name: "job1-worker-1",
					},
				},
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								JobAffinityAnnotations:     "ps,worker",
								JobAntiAffinityAnnotations: "ps,ps",
								TaskOrderAnnotations:       "ps,worker",
							},
						},
					},
				},
			},
			topology: nil,
			err:      fmt.Errorf("task %s is duplicated in job <%s/%s>", "ps", "default", "job1"),
		},
		{
			description: "redundant punctuations",
			job: &api.JobInfo{
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
					"3": {
						Name: "job1-worker-1",
					},
					"4": {
						Name: "job1-chief-0",
					},
					"5": {
						Name: "job1-evaluator-0",
					},
				},
				PodGroup: &api.PodGroup{
					PodGroup: scheduling.PodGroup{
						ObjectMeta: metav1.ObjectMeta{
							Annotations: map[string]string{
								JobAffinityAnnotations:     "ps,worker;ps,,chief,;",
								JobAntiAffinityAnnotations: "ps;worker,chief;;;",
								TaskOrderAnnotations:       "ps,worker,chief,evaluator,,,",
							},
						},
					},
				},
			},
			topology: &TaskTopology{
				Affinity: [][]string{
					{
						"ps",
						"worker",
					},
					{
						"ps",
						"",
						"chief",
						"",
					},
					{
						"",
					},
				},
				AntiAffinity: [][]string{
					{
						"ps",
					},
					{
						"worker",
						"chief",
					},
					{
						"",
					},
					{
						"",
					},
					{
						"",
					},
				},
				TaskOrder: []string{
					"ps",
					"worker",
					"chief",
					"evaluator",
					"",
					"",
					"",
				},
			},
			err: nil,
		},
	}

	for i, c := range cases {
		t.Run(fmt.Sprintf("case %d", i), func(t *testing.T) {
			t.Logf("case: %s", c.description)
			topology, err := readTopologyFromPgAnnotations(c.job)
			if !reflect.DeepEqual(err, c.err) {
				t.Errorf("want %v ,got %v", c.err, err)
			}
			if !reflect.DeepEqual(topology, c.topology) {
				t.Errorf("want %v ,got %v", c.topology, topology)
			}
		})
	}
}
