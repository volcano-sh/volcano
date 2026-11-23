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

package extender

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	vcapisv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

// TestOnSessionOpen checks that a failed onSessionOpen call does not stop the
// plugin from registering its predicate, so an unavailable extender is handled
// according to extender.ignorable instead of being skipped for the session.
func TestOnSessionOpen(t *testing.T) {
	const (
		sessionOpenVerb = "onSessionOpen"
		predicateVerb   = "predicate"
		rejectReason    = "node is reserved by the extender"
	)

	// The extender fails onSessionOpen and rejects every node in predicate.
	failingSessionOpen := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/"+sessionOpenVerb {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(`{"status":"` + rejectReason + `"}`))
	}))
	defer failingSessionOpen.Close()

	// A server that has been shut down refuses every connection, like an
	// extender whose pods are gone.
	unavailable := httptest.NewServer(http.NotFoundHandler())
	unavailableURL := unavailable.URL
	unavailable.Close()

	tests := []struct {
		name            string
		urlPrefix       string
		sessionOpenVerb string
		ignorable       bool
		wantErr         string
	}{
		{
			name:            "unavailable extender rejects the node when not ignorable",
			urlPrefix:       unavailableURL,
			sessionOpenVerb: sessionOpenVerb,
			wantErr:         "connection refused",
		},
		{
			name:      "unavailable extender rejects the node when not ignorable and onSessionOpen is not configured",
			urlPrefix: unavailableURL,
			wantErr:   "connection refused",
		},
		{
			name:            "unavailable extender is ignored when ignorable",
			urlPrefix:       unavailableURL,
			sessionOpenVerb: sessionOpenVerb,
			ignorable:       true,
		},
		{
			name:            "predicate is still asked when onSessionOpen fails",
			urlPrefix:       failingSessionOpen.URL,
			sessionOpenVerb: sessionOpenVerb,
			wantErr:         rejectReason,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			arguments := framework.Arguments{
				ExtenderURLPrefix:     tt.urlPrefix,
				ExtenderPredicateVerb: predicateVerb,
				ExtenderIgnorable:     tt.ignorable,
			}
			if tt.sessionOpenVerb != "" {
				arguments[ExtenderOnSessionOpenVerb] = tt.sessionOpenVerb
			}

			trueValue := true
			test := uthelper.TestCommonStruct{
				Name:    tt.name,
				Plugins: map[string]framework.PluginBuilder{PluginName: New},
				Pods: []*corev1.Pod{
					util.BuildPod("ns1", "p1", "", corev1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
				},
				Nodes: []*corev1.Node{
					util.BuildNode("n1", api.BuildResourceList("4", "8Gi"), make(map[string]string)),
				},
				PodGroups: []*vcapisv1.PodGroup{
					util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, vcapisv1.PodGroupInqueue),
				},
				Queues: []*vcapisv1.Queue{
					util.BuildQueue("q1", 1, nil),
				},
			}
			tiers := []conf.Tier{
				{
					Plugins: []conf.PluginOption{
						{Name: PluginName, EnabledPredicate: &trueValue, Arguments: arguments},
					},
				},
			}
			ssn := test.RegisterSession(tiers, nil)
			defer test.Close()

			var task *api.TaskInfo
			for _, job := range ssn.Jobs {
				for _, jobTask := range job.Tasks {
					task = jobTask
				}
			}
			require.NotNil(t, task, "task for pod ns1/p1 not found in session")

			err := ssn.PredicateFn(task, ssn.Nodes["n1"])

			if tt.wantErr == "" {
				assert.NoError(t, err)
				return
			}
			assert.ErrorContains(t, err, tt.wantErr)
		})
	}
}
