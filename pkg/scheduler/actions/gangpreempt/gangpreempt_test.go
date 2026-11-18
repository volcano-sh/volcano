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

package gangpreempt

import (
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	v1 "k8s.io/api/core/v1"
	pdbPolicy "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes/fake"
	schedulingapi "volcano.sh/apis/pkg/apis/scheduling"
	"volcano.sh/apis/pkg/apis/scheduling/v1beta1"

	"volcano.sh/volcano/pkg/scheduler/actions/utils"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/cache"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/pdb"
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

	domains := utils.PickDomainsFromGradients(gradients, 2, "")
	assert.Equal(t, []string{"d1", "d2"}, domains)
}

func TestPickDomainsFromGradients_Fallback(t *testing.T) {
	domains := utils.PickDomainsFromGradients(nil, 8, "<cluster-top-hypernode>")
	assert.Equal(t, []string{"<cluster-top-hypernode>"}, domains)
}

func TestParseArguments(t *testing.T) {
	ssn := &framework.Session{
		Configurations: []conf.Configuration{
			{
				Name: "gangpreempt",
				Arguments: map[string]interface{}{
					MaxDomainsKey:       3,
					AllowWholeBundleKey: false,
				},
			},
		},
	}
	action := New()
	action.parseArguments(ssn)
	assert.Equal(t, 3, action.maxDomains)
	assert.False(t, action.allowWholeBundle)
}

func TestParseArguments_InvalidMaxDomainsFallsBackToDefault(t *testing.T) {
	cases := []struct {
		name  string
		value int
	}{
		{name: "zero", value: 0},
		{name: "negative", value: -1},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ssn := &framework.Session{
				Configurations: []conf.Configuration{
					{
						Name: "gangpreempt",
						Arguments: map[string]interface{}{
							MaxDomainsKey: tc.value,
						},
					},
				},
			}
			action := New()
			action.parseArguments(ssn)
			assert.Equal(t, defaultMaxDomains, action.maxDomains)
		})
	}
}

func TestSelectDomainVictims_RespectAllowWholeBundle(t *testing.T) {
	preemptorJobID := api.JobID("ns/preemptor")
	victimJobID := api.JobID("ns/victim")
	nodeName := "n1"

	preemptor := testTask(preemptorJobID, "preemptor", nodeName, api.Pending, 100, 1000)
	victim := testTask(victimJobID, "victim", nodeName, api.Running, 10, 1000)

	preemptorJob := api.NewJobInfo(preemptorJobID, preemptor)
	preemptorJob.Queue = "q1"
	preemptorJob.Priority = 100
	victimJob := api.NewJobInfo(victimJobID, victim)
	victimJob.Queue = "q1"
	victimJob.Priority = 10
	victimJob.Preemptable = true
	victimJob.MinAvailable = 1 // one running task => whole bundle only

	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			preemptorJobID: preemptorJob,
			victimJobID:    victimJob,
		},
		Queues: map[api.QueueID]*api.QueueInfo{
			"q1": {UID: "q1", Name: "q1"},
		},
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{Name: "test-preempt"},
				},
			},
		},
	}
	ssn.AddUnifiedEvictableFn("test-preempt", func(_ *api.EvictionContext, candidates []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return candidates, 1
	})

	node := &api.NodeInfo{Name: nodeName, Tasks: map[api.TaskID]*api.TaskInfo{api.PodKey(victim.Pod): victim}}
	ssn.RealNodesList = map[string][]*api.NodeInfo{
		"d1": {node},
	}
	action := New()

	pending := []*api.TaskInfo{preemptor}
	jobNeed := utils.SumInitResreq(pending)

	action.allowWholeBundle = false
	victimsNoWhole := utils.FlattenBundles(action.selectDomainBundles(ssn, preemptorJob, pending, jobNeed, "d1"))
	assert.Len(t, victimsNoWhole, 0)

	action.allowWholeBundle = true
	victimsWhole := utils.FlattenBundles(action.selectDomainBundles(ssn, preemptorJob, pending, jobNeed, "d1"))
	assert.Len(t, victimsWhole, 1)
	assert.Equal(t, api.TaskID(victim.UID), victimsWhole[0].UID)
}

func TestSelectDomainVictims_RespectVictimJobPreemptable(t *testing.T) {
	preemptorJobID := api.JobID("ns/preemptor")
	victimJobID := api.JobID("ns/victim")
	nodeName := "n1"

	preemptor := testTask(preemptorJobID, "preemptor", nodeName, api.Pending, 100, 1000)
	victim := testTask(victimJobID, "victim", nodeName, api.Running, 10, 1000)

	preemptorJob := api.NewJobInfo(preemptorJobID, preemptor)
	preemptorJob.Queue = "q1"
	preemptorJob.Priority = 100
	victimJob := api.NewJobInfo(victimJobID, victim)
	victimJob.Queue = "q1"
	victimJob.Priority = 10
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

	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			preemptorJobID: preemptorJob,
			victimJobID:    victimJob,
		},
		Queues: map[api.QueueID]*api.QueueInfo{
			"q1": {UID: "q1", Name: "q1"},
		},
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{Name: "test-preempt"},
				},
			},
		},
	}
	ssn.AddUnifiedEvictableFn("test-preempt", func(_ *api.EvictionContext, candidates []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return candidates, 1
	})

	node := &api.NodeInfo{Name: nodeName, Tasks: map[api.TaskID]*api.TaskInfo{api.PodKey(victim.Pod): victim}}
	ssn.RealNodesList = map[string][]*api.NodeInfo{
		"d1": {node},
	}
	action := New()
	action.allowWholeBundle = true

	pending := []*api.TaskInfo{preemptor}
	jobNeed := utils.SumInitResreq(pending)
	victims := utils.FlattenBundles(action.selectDomainBundles(ssn, preemptorJob, pending, jobNeed, "d1"))
	assert.Len(t, victims, 0)
}

func TestSelectDomainVictims_AllowVictimJobWhenPreemptableUnset(t *testing.T) {
	preemptorJobID := api.JobID("ns/preemptor")
	victimJobID := api.JobID("ns/victim")
	nodeName := "n1"

	preemptor := testTask(preemptorJobID, "preemptor", nodeName, api.Pending, 100, 1000)
	victim := testTask(victimJobID, "victim", nodeName, api.Running, 10, 1000)

	preemptorJob := api.NewJobInfo(preemptorJobID, preemptor)
	preemptorJob.Queue = "q1"
	preemptorJob.Priority = 100
	victimJob := api.NewJobInfo(victimJobID, victim)
	victimJob.Queue = "q1"
	victimJob.Priority = 10
	// Unset podgroup-level preemptability defaults to allowed for gang actions.
	victimJob.Preemptable = false
	victimJob.PodGroup = &api.PodGroup{
		PodGroup: schedulingapi.PodGroup{},
	}

	ssn := &framework.Session{
		Jobs: map[api.JobID]*api.JobInfo{
			preemptorJobID: preemptorJob,
			victimJobID:    victimJob,
		},
		Queues: map[api.QueueID]*api.QueueInfo{
			"q1": {UID: "q1", Name: "q1"},
		},
		Tiers: []conf.Tier{
			{
				Plugins: []conf.PluginOption{
					{Name: "test-preempt"},
				},
			},
		},
	}
	ssn.AddUnifiedEvictableFn("test-preempt", func(_ *api.EvictionContext, candidates []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return candidates, 1
	})

	node := &api.NodeInfo{Name: nodeName, Tasks: map[api.TaskID]*api.TaskInfo{api.PodKey(victim.Pod): victim}}
	ssn.RealNodesList = map[string][]*api.NodeInfo{
		"d1": {node},
	}
	action := New()
	action.allowWholeBundle = true

	pending := []*api.TaskInfo{preemptor}
	jobNeed := utils.SumInitResreq(pending)
	victims := utils.FlattenBundles(action.selectDomainBundles(ssn, preemptorJob, pending, jobNeed, "d1"))
	assert.Len(t, victims, 1)
	assert.Equal(t, api.TaskID(victim.UID), victims[0].UID)
}

func TestSelectDomainVictims_PDBProtectedMembers(t *testing.T) {
	const protectedLabel = "protected"

	tests := []struct {
		name             string
		members          []string // "a-prot-*" members carry the PDB label; bundles are built in name order, so they are the first (safe) candidates
		minAvailable     int32
		disruptions      int32
		withPDB          bool
		allowWholeBundle bool
		want             []string
	}{
		{
			name:             "safe bundle of PDB-protected members is dropped, the free members are evicted as a whole bundle",
			members:          []string{"a-prot-1", "a-prot-2", "b-free-1", "b-free-2"},
			minAvailable:     2,
			withPDB:          true,
			allowWholeBundle: true,
			want:             []string{"b-free-1", "b-free-2"},
		},
		{
			name:             "safe bundle keeps only the members the PDB allows to evict",
			members:          []string{"a-prot-1", "b-free-1", "b-free-2"},
			minAvailable:     1,
			withPDB:          true,
			allowWholeBundle: false,
			want:             []string{"b-free-1"},
		},
		{
			name:             "whole bundle is dropped when one member is PDB-protected",
			members:          []string{"a-prot-1", "b-free-1"},
			minAvailable:     2,
			withPDB:          true,
			allowWholeBundle: true,
			want:             []string{},
		},
		{
			name:             "PDB budget is spent across members of the podgroup",
			members:          []string{"a-prot-1", "a-prot-2", "a-prot-3"},
			minAvailable:     1,
			disruptions:      1,
			withPDB:          true,
			allowWholeBundle: true,
			want:             []string{"a-prot-1"},
		},
		{
			name:             "without a PDB every member stays evictable",
			members:          []string{"a-prot-1", "b-free-1", "b-free-2"},
			minAvailable:     1,
			withPDB:          false,
			allowWholeBundle: true,
			want:             []string{"a-prot-1", "b-free-1", "b-free-2"},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			preemptorJobID := api.JobID("ns/preemptor")
			victimJobID := api.JobID("ns/victim")
			nodeName := "n1"

			preemptor := testTask(preemptorJobID, "preemptor", nodeName, api.Pending, 100, 1000)
			nodeTasks := map[api.TaskID]*api.TaskInfo{}
			victims := make([]*api.TaskInfo, 0, len(tc.members))
			for _, name := range tc.members {
				task := testTask(victimJobID, name, nodeName, api.Running, 10, 1000)
				// The pdb plugin skips pods without labels, so every member is labelled.
				task.Pod.Labels = map[string]string{"app": "unprotected"}
				if strings.HasPrefix(name, "a-prot-") {
					task.Pod.Labels = map[string]string{"app": protectedLabel}
				}
				victims = append(victims, task)
				nodeTasks[api.PodKey(task.Pod)] = task
			}

			preemptorJob := api.NewJobInfo(preemptorJobID, preemptor)
			preemptorJob.Queue = "q1"
			preemptorJob.Priority = 100
			victimJob := api.NewJobInfo(victimJobID, victims...)
			victimJob.Queue = "q1"
			victimJob.Priority = 10
			victimJob.Preemptable = true
			victimJob.MinAvailable = tc.minAvailable

			client := fake.NewSimpleClientset()
			informerFactory := informers.NewSharedInformerFactory(client, 0)
			if tc.withPDB {
				err := informerFactory.Policy().V1().PodDisruptionBudgets().Informer().GetStore().Add(&pdbPolicy.PodDisruptionBudget{
					ObjectMeta: metav1.ObjectMeta{Name: "protected-pdb", Namespace: "ns"},
					Spec: pdbPolicy.PodDisruptionBudgetSpec{
						Selector: &metav1.LabelSelector{MatchLabels: map[string]string{"app": protectedLabel}},
					},
					Status: pdbPolicy.PodDisruptionBudgetStatus{DisruptionsAllowed: tc.disruptions},
				})
				assert.NoError(t, err)
			}
			schedulerCache := cache.NewDefaultMockSchedulerCache("volcano")
			schedulerCache.SetSharedInformerFactory(informerFactory)

			ssn := framework.OpenSession(schedulerCache, []conf.Tier{
				{Plugins: []conf.PluginOption{{Name: pdb.PluginName}}},
			}, nil)
			origJobs, origQueues := ssn.Jobs, ssn.Queues
			defer func() {
				ssn.Jobs, ssn.Queues = origJobs, origQueues
				framework.CloseSession(ssn)
			}()
			pdb.New(framework.Arguments{}).OnSessionOpen(ssn)

			ssn.Jobs = map[api.JobID]*api.JobInfo{preemptorJobID: preemptorJob, victimJobID: victimJob}
			ssn.Queues = map[api.QueueID]*api.QueueInfo{"q1": {UID: "q1", Name: "q1"}}
			ssn.RealNodesList = map[string][]*api.NodeInfo{"d1": {{Name: nodeName, Tasks: nodeTasks}}}

			action := New()
			action.allowWholeBundle = tc.allowWholeBundle
			pending := []*api.TaskInfo{preemptor}
			bundles := action.selectDomainBundles(ssn, preemptorJob, pending, utils.SumInitResreq(pending), "d1")

			got := []string{}
			for _, task := range utils.FlattenBundles(bundles) {
				got = append(got, task.Name)
			}
			sort.Strings(got)
			assert.Equal(t, tc.want, got)
		})
	}
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
