package validate

import (
	"reflect"
	"testing"

	"volcano.sh/apis/pkg/apis/batch/v1alpha1"
)

func TestTopoSort(t *testing.T) {
	testCases := []struct {
		name        string
		job         *v1alpha1.Job
		sortedTasks []string
		isDag       bool
	}{
		{
			name: "test-1",
			job: &v1alpha1.Job{
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name: "t1",
							DependsOn: &v1alpha1.DependsOn{
								Name: []string{"t2", "t3"},
							},
						},
						{
							Name: "t2",
							DependsOn: &v1alpha1.DependsOn{
								Name: []string{"t3"},
							},
						},
						{
							Name: "t3",
							DependsOn: &v1alpha1.DependsOn{
								Name: []string{},
							},
						},
					},
				},
			},
			sortedTasks: []string{"t3", "t2", "t1"},
			isDag:       true,
		},
		{
			name: "test-2",
			job: &v1alpha1.Job{
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name: "t1",
							DependsOn: &v1alpha1.DependsOn{
								Name: []string{"t2"},
							},
						},
						{
							Name: "t2",
							DependsOn: &v1alpha1.DependsOn{
								Name: []string{"t1"},
							},
						},
						{
							Name:      "t3",
							DependsOn: nil,
						},
					},
				},
			},
			sortedTasks: nil,
			isDag:       false,
		},
		{
			name: "test-3",
			job: &v1alpha1.Job{
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name: "t1",
							DependsOn: &v1alpha1.DependsOn{
								Name: []string{"t2", "t3"},
							},
						},
						{
							Name: "t2",
							DependsOn: &v1alpha1.DependsOn{
								Name: []string{"t2"},
							},
						},
						{
							Name:      "t3",
							DependsOn: nil,
						},
					},
				},
			},
			sortedTasks: nil,
			isDag:       false,
		},
		{
			name: "test-4",
			job: &v1alpha1.Job{
				Spec: v1alpha1.JobSpec{
					Tasks: []v1alpha1.TaskSpec{
						{
							Name: "t1",
							DependsOn: &v1alpha1.DependsOn{
								Name: []string{"t2", "t3"},
							},
						},
						{
							Name:      "t2",
							DependsOn: nil,
						},
						{
							Name: "t3",
							DependsOn: &v1alpha1.DependsOn{
								Name: []string{"t2"},
							},
						},
					},
				},
			},
			sortedTasks: []string{"t2", "t3", "t1"},
			isDag:       true,
		},
	}

	for _, testcase := range testCases {
		tasks, isDag := topoSort(testcase.job)
		if isDag != testcase.isDag || !reflect.DeepEqual(tasks, testcase.sortedTasks) {
			t.Errorf("%s failed, expect sortedTasks: %v, got: %v, expected isDag: %v, got: %v",
				testcase.name, testcase.sortedTasks, tasks, testcase.isDag, isDag)
		}
	}
}
