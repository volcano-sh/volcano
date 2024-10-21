/*
Copyright 2024 The Volcano Authors.

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
package api

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/util/sets"
)

const (
	affinityRulesNotMatch        = "node(s) didn't match pod affinity rules"
	existingAntiAffinityNotMatch = "node(s) didn't satisfy existing pods anti-affinity rules"
	nodeAffinity                 = "node(s) didn't match Pod's node affinity/selector"
)

func TestFitError(t *testing.T) {
	tests := []struct {
		task   *TaskInfo
		node   *NodeInfo
		status []*Status
		// the wanted reason from fitError
		reason []string
		// the wanted fitError
		wantErr *FitError
		// string of fitError
		errStr string
	}{
		{
			task:   &TaskInfo{Name: "pod1", Namespace: "ns1"},
			node:   &NodeInfo{Name: "node1"},
			reason: []string{affinityRulesNotMatch, nodeAffinity},
			wantErr: &FitError{
				NodeName: "node1", taskNamespace: "ns1", taskName: "pod1",
				Status: []*Status{{Reason: affinityRulesNotMatch, Code: Error}, {Reason: nodeAffinity, Code: Error}},
			},
			errStr: "task ns1/pod1 on node node1 fit failed: " + affinityRulesNotMatch + ", " + nodeAffinity,
		},
		{
			task:   &TaskInfo{Name: "pod2", Namespace: "ns2"},
			node:   &NodeInfo{Name: "node2"},
			status: []*Status{{Reason: nodeAffinity, Code: UnschedulableAndUnresolvable}, {Reason: existingAntiAffinityNotMatch, Code: Error}},
			reason: []string{nodeAffinity, existingAntiAffinityNotMatch},
			wantErr: &FitError{
				NodeName: "node2", taskNamespace: "ns2", taskName: "pod2",
				Status: []*Status{{Reason: nodeAffinity, Code: UnschedulableAndUnresolvable}, {Reason: existingAntiAffinityNotMatch, Code: Error}},
			},
			errStr: "task ns2/pod2 on node node2 fit failed: " + nodeAffinity + ", " + existingAntiAffinityNotMatch,
		},
	}

	var got *FitError
	for _, test := range tests {
		if len(test.status) != 0 {
			got = NewFitErrWithStatus(test.task, test.node, test.status...)
		} else if len(test.reason) != 0 {
			got = NewFitError(test.task, test.node, test.reason...)
		}

		assert.Equal(t, test.wantErr, got)
		assert.Equal(t, test.reason, got.Reasons())
		assert.Equal(t, test.errStr, got.Error())
	}
}

func TestFitErrors(t *testing.T) {
	tests := []struct {
		node   string
		fitStr string
		err    error
		fiterr *FitError
		want   string // expected error string
		// nodes that are not helpful for preempting, which has a code of UnschedulableAndUnresolvable
		filterNodes map[string]sets.Empty
	}{
		{
			want:        "0/0 nodes are unavailable", // base fit err string is empty, set as the default
			filterNodes: map[string]sets.Empty{},
		},
		{
			node:   "node1",
			fitStr: "fit failed",
			err:    errors.New(NodePodNumberExceeded),
			want:   "fit failed: 1 node(s) pod number exceeded.",
			// no node has UnschedulableAndUnresolvable
			filterNodes: map[string]sets.Empty{},
		},
		{
			node:   "node1",
			fitStr: "NodeResourceFitFailed",
			err:    errors.New(NodePodNumberExceeded),
			fiterr: &FitError{
				taskNamespace: "ns1", taskName: "task1", NodeName: "node2",
				Status: []*Status{{Reason: nodeAffinity, Code: UnschedulableAndUnresolvable}},
			},
			want: "NodeResourceFitFailed: 1 node(s) didn't match Pod's node affinity/selector, 1 node(s) pod number exceeded.",
			// only node2 has UnschedulableAndUnresolvable
			filterNodes: map[string]sets.Empty{"node2": {}},
		},
	}
	for _, test := range tests {
		fitErrs := NewFitErrors()
		fitErrs.SetError(test.fitStr)
		if test.err != nil {
			fitErrs.SetNodeError(test.node, test.err)
		}
		if test.fiterr != nil {
			fitErrs.SetNodeError(test.fiterr.NodeName, test.fiterr)
		}
		got := fitErrs.Error()
		assert.Equal(t, test.want, got)
		assert.Equal(t, test.filterNodes, fitErrs.GetUnschedulableAndUnresolvableNodes())
	}
}
