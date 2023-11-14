/*
Copyright 2023 The Volcano Authors.

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

package pdb

import (
	pdbPolicy "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/informers"
	policylisters "k8s.io/client-go/listers/policy/v1"
	"k8s.io/klog/v2"

	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
	"volcano.sh/volcano/pkg/scheduler/plugins/util"
)

// PluginName indicates name of volcano scheduler plugin
const PluginName = "pdb"

type pdbPlugin struct {
	// Arguments given for pdb plugin
	pluginArguments framework.Arguments
	// Lister for PodDisruptionBudget
	lister policylisters.PodDisruptionBudgetLister
}

// New function returns pdb plugin object
func New(arguments framework.Arguments) framework.Plugin {
	return &pdbPlugin{
		pluginArguments: arguments,
		lister:          nil,
	}
}

// Name function returns pdb plugin name
func (pp *pdbPlugin) Name() string {
	return PluginName
}

func (pp *pdbPlugin) OnSessionOpen(ssn *framework.Session) {
	klog.V(4).Infof("Enter pdb plugin ...")
	defer klog.V(4).Infof("Leaving pdb plugin.")

	// 0. Init the PDB lister
	if pp.lister == nil {
		pp.lister = getPDBLister(ssn.InformerFactory())
	}

	// 1. define the func to filter out tasks that violate PDB constraints
	pdbFilterFn := func(tasks []*api.TaskInfo) []*api.TaskInfo {
		var victims []*api.TaskInfo

		// (a. get all PDBs
		pdbs, err := getPodDisruptionBudgets(pp.lister)
		if err != nil {
			klog.Errorf("Failed to list pdbs condition: %v", err)
			return victims
		}

		// (b. init the pdbsAllowed array
		pdbsAllowed := make([]int32, len(pdbs))
		for i, pdb := range pdbs {
			pdbsAllowed[i] = pdb.Status.DisruptionsAllowed
		}

		// (c. range every task to check if it violates the PDB constraints.
		// If task does not violate the PDB constraints, then add it to victims.
		for _, task := range tasks {
			pod := task.Pod
			pdbForPodIsViolated := false

			// A pod with no labels will not match any PDB. So, no need to check.
			if len(pod.Labels) == 0 {
				continue
			}

			for i, pdb := range pdbs {
				if pdb.Namespace != pod.Namespace {
					continue
				}
				selector, err := metav1.LabelSelectorAsSelector(pdb.Spec.Selector)
				if err != nil {
					continue
				}
				// A PDB with a nil or empty selector matches nothing.
				if selector.Empty() || !selector.Matches(labels.Set(pod.Labels)) {
					continue
				}

				// Existing in DisruptedPods means it has been processed in API server,
				// we don't treat it as a violating case.
				if _, exist := pdb.Status.DisruptedPods[pod.Name]; exist {
					continue
				}
				// Only decrement the matched pdb when it's not in its <DisruptedPods>;
				// otherwise we may over-decrement the budget number.
				pdbsAllowed[i]--

				if pdbsAllowed[i] < 0 {
					pdbForPodIsViolated = true
				}
			}

			if !pdbForPodIsViolated {
				victims = append(victims, task)
			} else {
				klog.V(4).Infof("The pod <%s> of task <%s> violates the pdb constraint, so filter it from the victim list", task.Name, task.Pod.Name)
			}
		}
		return victims
	}

	// 2. wrap pdbFilterFn to meet reclaimable and preemptable interface requirements
	wrappedPdbFilterFn := func(preemptor *api.TaskInfo, preemptees []*api.TaskInfo) ([]*api.TaskInfo, int) {
		return pdbFilterFn(preemptees), util.Permit
	}

	// 3. register VictimTasksFns, ReclaimableFn and PreemptableFn
	victimsFns := []api.VictimTasksFn{pdbFilterFn}
	ssn.AddVictimTasksFns(pp.Name(), victimsFns)
	ssn.AddReclaimableFn(pp.Name(), wrappedPdbFilterFn)
	ssn.AddPreemptableFn(pp.Name(), wrappedPdbFilterFn)
}

func (pp *pdbPlugin) OnSessionClose(ssn *framework.Session) {}

// getPDBLister returns the lister of PodDisruptionBudget
func getPDBLister(informerFactory informers.SharedInformerFactory) policylisters.PodDisruptionBudgetLister {
	return informerFactory.Policy().V1().PodDisruptionBudgets().Lister()
}

// getPodDisruptionBudgets returns all pdbs
func getPodDisruptionBudgets(pdbLister policylisters.PodDisruptionBudgetLister) ([]*pdbPolicy.PodDisruptionBudget, error) {
	if pdbLister != nil {
		return pdbLister.List(labels.Everything())
	}
	return nil, nil
}
