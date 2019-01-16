/*
Copyright 2017 The Volcano Authors.

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

package state

import (
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	kbv1 "github.com/kubernetes-sigs/kube-batch/pkg/apis/scheduling/v1alpha1"

	vkv1 "hpw.cloud/volcano/pkg/apis/batch/v1alpha1"
)

type Request struct {
	Event  vkv1.Event
	Action vkv1.Action

	Namespace string
	Target    *metav1.OwnerReference

	Job      *vkv1.Job
	Pod      *v1.Pod
	PodGroup *kbv1.PodGroup

	Reason  string
	Message string
}

type ActionFn func(req *Request) error

var actionFns = map[vkv1.Action]ActionFn{}

func RegisterActions(afs map[vkv1.Action]ActionFn) {
	actionFns = afs
}

type State interface {
	Execute() error
}

func NewState(req *Request) State {
	policies := parsePolicies(req)

	switch req.Job.Status.State.Phase {
	case vkv1.Restarting:
		return &restartingState{
			request:  req,
			policies: policies,
		}
	default:
		return &baseState{
			request:  req,
			policies: policies,
		}
	}
}
