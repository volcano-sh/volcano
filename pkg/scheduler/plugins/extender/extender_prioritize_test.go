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
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"

	vcapisv1 "volcano.sh/apis/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/actions/allocate"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/conf"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/uthelper"
	"volcano.sh/volcano/pkg/scheduler/util"
)

const prioritizeVerb = "prioritize"

// newPrioritizeExtender starts an HTTP server that answers the prioritize verb
// with the given status code and body, and counts the requests it receives.
func newPrioritizeExtender(t *testing.T, statusCode int, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	calls := &atomic.Int32{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		assert.Equal(t, "/"+prioritizeVerb, r.URL.Path)
		w.WriteHeader(statusCode)
		w.Write([]byte(body))
	}))
	t.Cleanup(server.Close)
	return server, calls
}

func prioritizeTiers(serverURL string, ignorable bool) []conf.Tier {
	trueValue := true
	return []conf.Tier{
		{
			Plugins: []conf.PluginOption{
				{
					Name:             PluginName,
					EnabledNodeOrder: &trueValue,
					Arguments: framework.Arguments{
						ExtenderURLPrefix:      serverURL,
						ExtenderPrioritizeVerb: prioritizeVerb,
						ExtenderIgnorable:      ignorable,
					},
				},
			},
		},
	}
}

func prioritizeTestCase(name string) uthelper.TestCommonStruct {
	return uthelper.TestCommonStruct{
		Name:    name,
		Plugins: map[string]framework.PluginBuilder{PluginName: New},
		Pods: []*corev1.Pod{
			util.BuildPod("ns1", "p1", "", corev1.PodPending, api.BuildResourceList("1", "1Gi"), "pg1", make(map[string]string), make(map[string]string)),
		},
		Nodes: []*corev1.Node{
			util.BuildNode("n1", api.BuildResourceList("4", "8Gi", api.ScalarResource{Name: "pods", Value: "10"}), make(map[string]string)),
			util.BuildNode("n2", api.BuildResourceList("4", "8Gi", api.ScalarResource{Name: "pods", Value: "10"}), make(map[string]string)),
		},
		PodGroups: []*vcapisv1.PodGroup{
			util.BuildPodGroup("pg1", "ns1", "q1", 1, nil, vcapisv1.PodGroupInqueue),
		},
		Queues: []*vcapisv1.Queue{
			util.BuildQueue("q1", 1, nil),
		},
	}
}

// TestBatchNodeOrderFn checks how the extender's prioritize response, an HTTP
// failure and the ignorable option decide the node scores and the error that
// the extender plugin returns to the session.
func TestBatchNodeOrderFn(t *testing.T) {
	tests := []struct {
		name         string
		ignorable    bool
		statusCode   int
		responseBody string
		wantScores   map[string]float64
		wantErr      string
	}{
		{
			name:         "extender scores the nodes",
			statusCode:   http.StatusOK,
			responseBody: `{"nodeScore":{"n1":10,"n2":20}}`,
			wantScores:   map[string]float64{"n1": 10, "n2": 20},
		},
		{
			name:         "extender has no preference and returns empty scores",
			statusCode:   http.StatusOK,
			responseBody: `{"nodeScore":{}}`,
			wantScores:   map[string]float64{},
		},
		{
			name:         "extender has no preference and returns no scores",
			statusCode:   http.StatusOK,
			responseBody: `{}`,
			wantScores:   map[string]float64{},
		},
		{
			name:         "extender reports an error when not ignorable",
			statusCode:   http.StatusOK,
			responseBody: `{"errorMessage":"cache not ready"}`,
			wantErr:      "cache not ready",
		},
		{
			name:         "extender reported error is ignored when ignorable",
			ignorable:    true,
			statusCode:   http.StatusOK,
			responseBody: `{"errorMessage":"cache not ready"}`,
			wantScores:   map[string]float64{},
		},
		{
			name:         "extender failure is returned when not ignorable",
			statusCode:   http.StatusInternalServerError,
			responseBody: `{}`,
			wantErr:      "failed prioritize with extender",
		},
		{
			name:         "extender failure is ignored when ignorable",
			ignorable:    true,
			statusCode:   http.StatusInternalServerError,
			responseBody: `{}`,
			wantScores:   map[string]float64{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, calls := newPrioritizeExtender(t, tt.statusCode, tt.responseBody)
			test := prioritizeTestCase(tt.name)
			ssn := test.RegisterSession(prioritizeTiers(server.URL, tt.ignorable), nil)
			defer test.Close()

			var task *api.TaskInfo
			for _, job := range ssn.Jobs {
				for _, jobTask := range job.Tasks {
					task = jobTask
				}
			}
			require.NotNil(t, task, "task for pod ns1/p1 not found in session")
			nodes := []*api.NodeInfo{ssn.Nodes["n1"], ssn.Nodes["n2"]}

			scores, err := ssn.BatchNodeOrderFn(task, nodes)

			assert.Equal(t, int32(1), calls.Load(), "extender prioritize calls")
			if tt.wantErr != "" {
				assert.ErrorContains(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantScores, scores)
		})
	}
}

// TestAllocateWithExtenderWithoutPreference runs the allocate action on two
// feasible nodes and checks that the task is bound when the extender's
// prioritize response carries no node scores.
func TestAllocateWithExtenderWithoutPreference(t *testing.T) {
	server, calls := newPrioritizeExtender(t, http.StatusOK, `{}`)
	test := prioritizeTestCase("task is bound when the extender has no preference")
	// Either node is a valid choice, so only the number of binds is checked.
	test.ExpectBindsNum = 1
	test.MinimalBindCheck = true

	test.RegisterSession(prioritizeTiers(server.URL, false), nil)
	defer test.Close()
	test.Run([]framework.Action{allocate.New()})

	require.NoError(t, test.CheckAll(0))
	assert.Equal(t, int32(1), calls.Load(), "extender prioritize calls")
}
