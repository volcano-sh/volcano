/*
Copyright 2017 The Kubernetes Authors.

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

package cache

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"

	"github.com/golang/glog"
	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
)

type ConsumerInfo struct {
	Consumer *arbv1.Consumer

	Name      string
	Namespace string

	// All jobs belong to this Consumer
	PodSets map[types.UID]*PodSet

	// The pod that without `Owners`
	Pods map[string]*PodInfo
}

func NewConsumerInfo(consumer *arbv1.Consumer) *ConsumerInfo {
	if consumer == nil {
		return &ConsumerInfo{
			Name:      "",
			Namespace: "",
			Consumer:  nil,

			PodSets: make(map[types.UID]*PodSet),
			Pods:    make(map[string]*PodInfo),
		}
	}

	return &ConsumerInfo{
		Name:      consumer.Name,
		Namespace: consumer.Namespace,
		Consumer:  consumer,

		PodSets: make(map[types.UID]*PodSet),
		Pods:    make(map[string]*PodInfo),
	}
}

func (ci *ConsumerInfo) SetConsumer(consumer *arbv1.Consumer) {
	if consumer == nil {
		ci.Name = ""
		ci.Namespace = ""
		ci.Consumer = consumer
		ci.PodSets = make(map[types.UID]*PodSet)
		ci.Pods = make(map[string]*PodInfo)
		return
	}

	ci.Name = consumer.Name
	ci.Namespace = consumer.Namespace
	ci.Consumer = consumer
}

func (ci *ConsumerInfo) AddPod(pi *PodInfo) {
	if len(pi.Owner) == 0 {
		ci.Pods[pi.Name] = pi
	} else {
		if _, found := ci.PodSets[pi.Owner]; !found {
			ci.PodSets[pi.Owner] = NewPodSet(pi.Owner)
		}
		ci.PodSets[pi.Owner].AddPodInfo(pi)
	}
}

func (ci *ConsumerInfo) RemovePod(pi *PodInfo) {
	if len(pi.Owner) == 0 {
		delete(ci.Pods, pi.Name)
	} else {
		if _, found := ci.PodSets[pi.Owner]; found {
			ci.PodSets[pi.Owner].DeletePodInfo(pi)
		}
	}
}

func (ci *ConsumerInfo) AddPdb(pi *PdbInfo) {
	for _, ps := range ci.PodSets {
		if len(ps.PdbName) != 0 {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(pi.Pdb.Spec.Selector)
		if err != nil {
			glog.V(4).Infof("LabelSelectorAsSelector fail for pdb %s", pi.Name)
			continue
		}
		// One PDB is fully for one PodSet
		// TODO(jinzhej): handle PDB cross different PodSet later on demand
		if selector.Matches(labels.Set(ps.Labels)) {
			ps.PdbName = pi.Name
			if pi.Pdb.Spec.MinAvailable.Type == intstr.Int {
				// support integer MinAvailable in PodDisruptionBuget
				// TODO(jinzhej): percentage MinAvailable, integer/percentage MaxUnavailable will be supported on demand
				ps.MinAvailable = int(pi.Pdb.Spec.MinAvailable.IntVal)
			}
		}
	}
}

func (ci *ConsumerInfo) RemovePdb(pi *PdbInfo) {
	for _, ps := range ci.PodSets {
		if len(ps.PdbName) == 0 {
			continue
		}
		selector, err := metav1.LabelSelectorAsSelector(pi.Pdb.Spec.Selector)
		if err != nil {
			glog.V(4).Infof("LabelSelectorAsSelector fail for pdb %s", pi.Name)
			continue
		}
		if selector.Matches(labels.Set(ps.Labels)) {
			ps.PdbName = ""
			ps.MinAvailable = 0
		}
	}
}

func (ci *ConsumerInfo) Clone() *ConsumerInfo {
	info := &ConsumerInfo{
		Name:      ci.Name,
		Namespace: ci.Namespace,
		Consumer:  ci.Consumer,

		PodSets: make(map[types.UID]*PodSet),
		Pods:    make(map[string]*PodInfo),
	}

	for owner, ps := range ci.PodSets {
		info.PodSets[owner] = ps.Clone()
	}

	for name, p := range ci.Pods {
		info.Pods[name] = p.Clone()
	}

	return info
}
