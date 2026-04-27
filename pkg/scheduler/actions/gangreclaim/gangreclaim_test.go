/*
Copyright 2026 The Volcano Authors.

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

package gangreclaim

import (
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	schedulingapi "volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/actions/gangevict"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

func TestPickDomainsFromGradients_MaxDomainsAndDedup(t *testing.T) {
	gradients := [][]*api.HyperNodeInfo{
		{
			{Name: "d1"},
			{Name: "d2"},
		},
		{
			{Name: "d2"},
			{Name: "d3"},
		},
	}

	domains := gangevict.PickDomainsFromGradients(gradients, 3, "")
	assert.Equal(t, []string{"d1", "d2", "d3"}, domains)
}

func TestPickDomainsFromGradients_Fallback(t *testing.T) {
	domains := gangevict.PickDomainsFromGradients([][]*api.HyperNodeInfo{}, 8, "<cluster-top-hypernode>")
	assert.Equal(t, []string{"<cluster-top-hypernode>"}, domains)
}

func TestParseArguments(t *testing.T) {
	ssn := &framework.Session{
		Configurations: []conf.Configuration{
			{
				Name: "gangreclaim",
				Arguments: map[string]interface{}{
					MaxDomainsKey:       5,
					AllowWholeBundleKey: false,
				},
			},
		},
	}
	action := New()
	action.parseArguments(ssn)
	assert.Equal(t, 5, action.maxDomains)
	assert.False(t, action.allowWholeBundle)
}

func TestSelectDomainVictims_ReclaimableCrossQueueOnly(t *testing.T) {
	reclaimerJobID := api.JobID("ns/reclaimer")
	victimJobID := api.JobID("ns/victim")
	otherJobID := api.JobID("ns/other")
	nodeName := "n1"

	reclaimer := testTask(reclaimerJobID, "reclaimer", "", api.Pending, 100, 1000)
	reclaimerJob := api.NewJobInfo(reclaimerJobID, reclaimer)
	reclaimerJob.Queue = "q1"

	victim := testTask(victimJobID, "victim", nodeName, api.Running, 10, 1000)
	victimJob := api.NewJobInfo(victimJobID, victim)
	victimJob.Queue = "q2"
	victimJob.Preemptable = true

	nonReclaimableVictim := testTask(otherJobID, "other", nodeName, api.Running, 10, 1000)
	otherJob := api.NewJobInfo(otherJobID, nonReclaimableVictim)
	otherJob.Queue = "q3"
	otherJob.Preemptable = true

	reclaimable := true
	nonReclaimable := false
	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			reclaimerJobID: reclaimerJob,
			victimJobID:    victimJob,
			otherJobID:     otherJob,
		},
		Queues: map[api.QueueID]*api.QueueInfo{
			"q1": {UID: "q1", Name: "q1"},
			"q2": {UID: "q2", Name: "q2", Queue: &schedulingapi.Queue{Spec: schedulingapi.QueueSpec{Reclaimable: &reclaimable}}},
			"q3": {UID: "q3", Name: "q3", Queue: &schedulingapi.Queue{Spec: schedulingapi.QueueSpec{Reclaimable: &nonReclaimable}}},
		},
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{Name: "test-reclaim"},
				},
			},
		},
	}
	ssn.AddUnifiedEvictableFn("test-reclaim", func(_ *api.EvictionContext, candidates []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return candidates, 1
	})

	node := &api.NodeInfo{
		Name: nodeName,
		Tasks: map[api.TaskID]*api.TaskInfo{
			api.PodKey(victim.Pod):               victim,
			api.PodKey(nonReclaimableVictim.Pod): nonReclaimableVictim,
		},
	}
	ssn.RealNodesList = map[string][]*api.NodeInfo{
		"d1": {node},
	}

	action := New()
	action.allowWholeBundle = true
	victims := gangevict.FlattenBundles(action.selectDomainBundles(ssn, nil, reclaimerJob, []*api.TaskInfo{reclaimer}, "d1"))
	assert.Len(t, victims, 1)
	assert.Equal(t, api.TaskID(victim.UID), victims[0].UID)
}

func TestSelectDomainVictims_RespectVictimJobPreemptable(t *testing.T) {
	reclaimerJobID := api.JobID("ns/reclaimer")
	victimJobID := api.JobID("ns/victim")
	nodeName := "n1"

	reclaimer := testTask(reclaimerJobID, "reclaimer", "", api.Pending, 100, 1000)
	reclaimerJob := api.NewJobInfo(reclaimerJobID, reclaimer)
	reclaimerJob.Queue = "q1"

	victim := testTask(victimJobID, "victim", nodeName, api.Running, 10, 1000)
	victimJob := api.NewJobInfo(victimJobID, victim)
	victimJob.Queue = "q2"
	victimJob.Preemptable = false
	victimJob.PodGroup = &api.PodGroup{
		PodGroup: schedulingapi.PodGroup{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{
					v1beta1.PodPreemptable: "false",
				},
			},
		},
	}

	reclaimable := true
	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			reclaimerJobID: reclaimerJob,
			victimJobID:    victimJob,
		},
		Queues: map[api.QueueID]*api.QueueInfo{
			"q1": {UID: "q1", Name: "q1"},
			"q2": {UID: "q2", Name: "q2", Queue: &schedulingapi.Queue{Spec: schedulingapi.QueueSpec{Reclaimable: &reclaimable}}},
		},
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{Name: "test-reclaim"},
				},
			},
		},
	}
	ssn.AddUnifiedEvictableFn("test-reclaim", func(_ *api.EvictionContext, candidates []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return candidates, 1
	})

	node := &api.NodeInfo{
		Name: nodeName,
		Tasks: map[api.TaskID]*api.TaskInfo{
			api.PodKey(victim.Pod): victim,
		},
	}
	ssn.RealNodesList = map[string][]*api.NodeInfo{
		"d1": {node},
	}

	action := New()
	action.allowWholeBundle = true
	victims := gangevict.FlattenBundles(action.selectDomainBundles(ssn, nil, reclaimerJob, []*api.TaskInfo{reclaimer}, "d1"))
	assert.Len(t, victims, 0)
}

func TestSelectDomainVictims_AllowVictimJobWhenPreemptableUnset(t *testing.T) {
	reclaimerJobID := api.JobID("ns/reclaimer")
	victimJobID := api.JobID("ns/victim")
	nodeName := "n1"

	reclaimer := testTask(reclaimerJobID, "reclaimer", "", api.Pending, 100, 1000)
	reclaimerJob := api.NewJobInfo(reclaimerJobID, reclaimer)
	reclaimerJob.Queue = "q1"

	victim := testTask(victimJobID, "victim", nodeName, api.Running, 10, 1000)
	victimJob := api.NewJobInfo(victimJobID, victim)
	victimJob.Queue = "q2"
	// Unset podgroup-level preemptability defaults to allowed for gang actions.
	victimJob.Preemptable = false
	victimJob.PodGroup = &api.PodGroup{
		PodGroup: schedulingapi.PodGroup{},
	}

	reclaimable := true
	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			reclaimerJobID: reclaimerJob,
			victimJobID:    victimJob,
		},
		Queues: map[api.QueueID]*api.QueueInfo{
			"q1": {UID: "q1", Name: "q1"},
			"q2": {UID: "q2", Name: "q2", Queue: &schedulingapi.Queue{Spec: schedulingapi.QueueSpec{Reclaimable: &reclaimable}}},
		},
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{Name: "test-reclaim"},
				},
			},
		},
	}
	ssn.AddUnifiedEvictableFn("test-reclaim", func(_ *api.EvictionContext, candidates []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return candidates, 1
	})

	node := &api.NodeInfo{
		Name: nodeName,
		Tasks: map[api.TaskID]*api.TaskInfo{
			api.PodKey(victim.Pod): victim,
		},
	}
	ssn.RealNodesList = map[string][]*api.NodeInfo{
		"d1": {node},
	}

	action := New()
	action.allowWholeBundle = true
	victims := gangevict.FlattenBundles(action.selectDomainBundles(ssn, nil, reclaimerJob, []*api.TaskInfo{reclaimer}, "d1"))
	assert.Len(t, victims, 1)
	assert.Equal(t, api.TaskID(victim.UID), victims[0].UID)
}

func testTask(jobID api.JobID, name, node string, status api.TaskStatus, priority int32, milliCPU float64) *api.TaskInfo {
	res := (&api.Resource{MilliCPU: milliCPU}).Clone()
	return &api.TaskInfo{
		UID:         api.TaskID(name),
		Job:         jobID,
		Name:        name,
		Namespace:   "ns",
		Priority:    priority,
		Preemptable: true,
		Resreq:      res.Clone(),
		InitResreq:  res.Clone(),
		Pod: &v1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Name:      name,
				Namespace: "ns",
				UID:       types.UID(name),
			},
		},
		NumaInfo: &api.TopologyInfo{
			ResMap: map[int]v1.ResourceList{},
		},
		TransactionContext: api.TransactionContext{
			NodeName: node,
			Status:   status,
		},
	}
}

func boolPtr(v bool) *bool { return &v }
