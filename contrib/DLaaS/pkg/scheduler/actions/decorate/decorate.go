/*
Copyright 2018 The Kubernetes Authors.

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

package decorate

import (
	"k8s.io/apimachinery/pkg/labels"

	"github.com/golang/glog"
	arbapi "github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/api"
	"github.com/kubernetes-sigs/kube-batch/contrib/DLaaS/pkg/scheduler/framework"
)

type decorateAction struct {
	ssn *framework.Session
}

func New() *decorateAction {
	return &decorateAction{}
}

func (alloc *decorateAction) Name() string {
	return "decorate"
}

func (alloc *decorateAction) Initialize() {}

func (alloc *decorateAction) Execute(ssn *framework.Session) {
	glog.V(3).Infof("Enter Decorate ...")
	defer glog.V(3).Infof("Leaving Decorate ...")

	// fetch the nodes that match PodSet NodeSelector and NodeAffinity
	// and store it for following DRF assignment
	jobs := ssn.Jobs
	nodes := ssn.Nodes

	for _, job := range jobs {
		job.Candidates = fetchMatchNodeForPodSet(job, nodes)
		glog.V(3).Infof("Got %d candidate nodes for Job %v/%v",
			len(job.Candidates), job.Namespace, job.Name)
	}
}

func (alloc *decorateAction) UnInitialize() {}

func fetchMatchNodeForPodSet(job *arbapi.JobInfo, nodes []*arbapi.NodeInfo) []*arbapi.NodeInfo {
	if len(job.NodeSelector) == 0 {
		// nil slice means select everything.
		return nil
	}

	// Empty slice means no object selected.
	matchNodes := []*arbapi.NodeInfo{}
	selector := labels.SelectorFromSet(labels.Set(job.NodeSelector))

	for _, node := range nodes {
		if selector.Matches(labels.Set(node.Node.Labels)) {
			matchNodes = append(matchNodes, node)
		}
	}

	return matchNodes
}
