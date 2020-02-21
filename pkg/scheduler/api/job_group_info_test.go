package api

import (
	"testing"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func jobOrderFn(l, r interface{}) bool {

	lv := l.(*JobInfo)
	rv := r.(*JobInfo)
	return lv.Priority >= rv.Priority

}

type podConfig struct {
	name     string
	nodeName string
	podPhase v1.PodPhase
	memory   string
	cpu      string
}

type jobCofig struct {
	uid        JobID
	namespace  string
	priority   int32
	podconfigs []podConfig
}

func buildJob(jobconfig jobCofig) *JobInfo {
	job := NewJobInfo(jobconfig.uid)
	owner := buildOwnerReference(string(job.UID))
	for _, podconfig := range jobconfig.podconfigs {
		pod := buildPod(jobconfig.namespace, podconfig.name, podconfig.nodeName, podconfig.podPhase, buildResourceList(podconfig.cpu, podconfig.memory), []metav1.OwnerReference{owner}, make(map[string]string))
		task := NewTaskInfo(pod)
		job.AddTaskInfo(task)
	}
	job.Priority = jobconfig.priority
	return job
}

func TestAddJob(t *testing.T) {

	job1 := buildJob(jobCofig{
		uid:       JobID("uid1"),
		namespace: "c1",
		priority:  10,
		podconfigs: []podConfig{
			{
				name:     "p1-1",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p1-2",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p1-3",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
		},
	})

	job2 := buildJob(jobCofig{
		uid:       JobID("uid2"),
		namespace: "c1",
		priority:  1,
		podconfigs: []podConfig{
			{
				name:     "p2-1",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p2-2",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p2-3",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
		},
	})

	job3 := buildJob(jobCofig{
		uid:       JobID("uid3"),
		namespace: "c2",
		priority:  1,
		podconfigs: []podConfig{
			{
				name:     "p3-1",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p3-2",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p3-3",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
		},
	})
	group := NewJobGroupInfo(job1, jobOrderFn)
	group.AddJob(job1)
	group.AddJob(job2)
	group.AddJob(job3)
	if len(group.Jobs) != 2 {
		t.Errorf("JobGroup AddJob failed, expected job num: 2, got: %d", len(group.Jobs))
	}

	if group.TopJob == nil {
		t.Errorf("JobGroup AddJob failed, expected job TopJob: %s, got: nil", job1.UID)
	}
	if group.TopJob.UID != job1.UID {
		t.Errorf("JobGroup AddJob failed, expected job TopJob: %s, got: %s", job1.UID, group.TopJob.UID)
	}

	// tests := []struct{}{}

	// for i, test := range tests {

	// }
}

func TestJobGroupReady(t *testing.T) {
	job1 := buildJob(jobCofig{
		uid:       JobID("uid1"),
		namespace: "c1",
		priority:  10,
		podconfigs: []podConfig{
			{
				name:     "p1-1",
				nodeName: "",
				podPhase: v1.PodRunning,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p1-2",
				nodeName: "",
				podPhase: v1.PodRunning,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p1-3",
				nodeName: "",
				podPhase: v1.PodRunning,
				memory:   "1G",
				cpu:      "1000m",
			},
		},
	})
	job1.MinAvailable = 3
	job2 := buildJob(jobCofig{
		uid:       JobID("uid2"),
		namespace: "c1",
		priority:  1,
		podconfigs: []podConfig{
			{
				name:     "p2-1",
				nodeName: "",
				podPhase: v1.PodRunning,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p2-2",
				nodeName: "",
				podPhase: v1.PodRunning,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p2-3",
				nodeName: "",
				podPhase: v1.PodRunning,
				memory:   "1G",
				cpu:      "1000m",
			},
		},
	})
	job2.MinAvailable = 3

	job3 := buildJob(jobCofig{
		uid:       JobID("uid3"),
		namespace: "c1",
		priority:  2,
		podconfigs: []podConfig{
			{
				name:     "p3-1",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p3-2",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p3-3",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
		},
	})
	job3.MinAvailable = 2

	tests := []struct {
		name     string
		jobs     []*JobInfo
		expected bool
	}{
		{
			name:     "test JobGroup Ready function, all jobs are ready",
			jobs:     []*JobInfo{job1, job2},
			expected: true,
		},
		{
			name:     "test JobGroup Ready function, one job is not ready",
			jobs:     []*JobInfo{job1, job2, job3},
			expected: false,
		},
	}
	for _, test := range tests {
		var group *JobGroupInfo
		for _, job := range test.jobs {
			if group == nil {
				group = NewJobGroupInfo(job, jobOrderFn)
			}
			group.AddJob(job)
		}
		rdy := group.Ready()
		if rdy != test.expected {
			t.Errorf("test <%s> failed, expected <%t>, got <%t>", test.name, rdy, test.expected)
		}
	}

}

func TestJobGroupPipelined(t *testing.T) {
	job1 := buildJob(jobCofig{
		uid:       JobID("uid1"),
		namespace: "c1",
		priority:  10,
		podconfigs: []podConfig{
			{
				name:     "p1-1",
				nodeName: "",
				podPhase: v1.PodRunning,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p1-2",
				nodeName: "",
				podPhase: v1.PodRunning,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p1-3",
				nodeName: "",
				podPhase: v1.PodRunning,
				memory:   "1G",
				cpu:      "1000m",
			},
		},
	})
	job1.MinAvailable = 3
	for _, task := range job1.Tasks {
		job1.UpdateTaskStatus(task, Pipelined)
	}
	job2 := buildJob(jobCofig{
		uid:       JobID("uid2"),
		namespace: "c1",
		priority:  1,
		podconfigs: []podConfig{
			{
				name:     "p2-1",
				nodeName: "",
				podPhase: v1.PodRunning,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p2-2",
				nodeName: "",
				podPhase: v1.PodRunning,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p2-3",
				nodeName: "",
				podPhase: v1.PodRunning,
				memory:   "1G",
				cpu:      "1000m",
			},
		},
	})
	job2.MinAvailable = 3
	for _, task := range job1.Tasks {
		job1.UpdateTaskStatus(task, Pipelined)
	}

	job3 := buildJob(jobCofig{
		uid:       JobID("uid3"),
		namespace: "c1",
		priority:  2,
		podconfigs: []podConfig{
			{
				name:     "p3-1",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p3-2",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
			{
				name:     "p3-3",
				nodeName: "",
				podPhase: v1.PodPending,
				memory:   "1G",
				cpu:      "1000m",
			},
		},
	})
	job3.MinAvailable = 2

	tests := []struct {
		name     string
		jobs     []*JobInfo
		expected bool
	}{
		{
			name:     "test JobGroup Ready function, all jobs are Pipelined",
			jobs:     []*JobInfo{job1, job2},
			expected: true,
		},
		{
			name:     "test JobGroup Ready function, one job is not Pipelined",
			jobs:     []*JobInfo{job1, job2, job3},
			expected: false,
		},
	}
	for _, test := range tests {
		var group *JobGroupInfo
		for _, job := range test.jobs {
			if group == nil {
				group = NewJobGroupInfo(job, jobOrderFn)
			}
			group.AddJob(job)
		}
		rdy := group.Pipelined()
		if rdy != test.expected {
			t.Errorf("test <%s> failed, expected <%t>, got <%t>", test.name, rdy, test.expected)
		}
	}
}
