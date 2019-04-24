/*
Copyright 2019 The Kubernetes Authors.

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

package kube_batch

import (
	"fmt"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	kubeletapi "k8s.io/kubernetes/pkg/kubelet/apis"
)

var _ = Describe("Predicates E2E Test", func() {

	// Description: This test is to validate if the scheduler respects the max-pods limit of a node.
	// steps:
	// 1) Fetch a node from the list of available node.
	// 2) Get the nodes max-pods limit.
	// 3) Check nodes stability and get the pods running on that node.
	// 4) From the max-pods limit and the total pods runnnin from (2) and (3) we can calculate how many pods we can create.
	// 5) Create the remaining pods as a podgroup and deploy.
	// 6) Now create a single podgroup called pending and create one task with one replica [pod], this will remain in
	//    pending state since we have exhausted all the max pods limit.
	//
	It("validates MaxPods limit [Slow]", func() {
		context := initTestContext()
		defer func() {

			// need to make sure if the namespace is delete properly.
			// this takes time for this test case since we create lot may pods in podgroup
			cleanupTestContext(context)
		}()

		var totalPodCapacity int64
		var podsNeededForSaturation int32
		schedulableNodes := getAllWorkerNodes(context)
		nodeName := schedulableNodes[0].Name

		// get the max-pods capicity
		podCapacity, found := schedulableNodes[0].Status.Capacity[v1.ResourcePods]
		Expect(found).To(Equal(true))
		totalPodCapacity = podCapacity.Value()
		currentlyScheduledPods := getScheduledPodsOfNode(context, nodeName)
		podsNeededForSaturation = int32(totalPodCapacity) - int32(currentlyScheduledPods)
		if podsNeededForSaturation > 0 {
			// create a podgroup with nodeselector to the node and the number of replicas
			// in the task as podsNeededForSaturation

			affinityNode := &v1.Affinity{
				NodeAffinity: &v1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
						NodeSelectorTerms: []v1.NodeSelectorTerm{
							{
								MatchExpressions: []v1.NodeSelectorRequirement{
									{
										Key:      kubeletapi.LabelHostname,
										Operator: v1.NodeSelectorOpIn,
										Values:   []string{nodeName},
									},
								},
							},
						},
					},
				},
			}

			maxpodJobOne := &jobSpec{
				name: "max-pods",
				tasks: []taskSpec{
					{
						img:      "nginx",
						min:      podsNeededForSaturation,
						rep:      podsNeededForSaturation,
						affinity: affinityNode,
					},
				},
			}

			//Schedule Job in the selected Node
			By(fmt.Sprintf("Creating the PodGroup with %v task replicas on node %v", podsNeededForSaturation, nodeName))
			_, maxpodspg := createJob(context, maxpodJobOne)
			By("Waiting for the PodGroup to have running status")
			err := waitTimeoutPodGroupReady(context, maxpodspg, tenMinute)
			checkError(context, err)

		}
		// create the new podgroup with on task replica on the same node as created before.
		// this podgroup should remain pending
		affinityNode := &v1.Affinity{
			NodeAffinity: &v1.NodeAffinity{
				RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{
					NodeSelectorTerms: []v1.NodeSelectorTerm{
						{
							MatchExpressions: []v1.NodeSelectorRequirement{
								{
									Key:      kubeletapi.LabelHostname,
									Operator: v1.NodeSelectorOpIn,
									Values:   []string{nodeName},
								},
							},
						},
					},
				},
			},
		}
		maxpodJobTwo := &jobSpec{
			name: "unscheduled-pod",
			tasks: []taskSpec{
				{
					img:      "nginx",
					min:      1,
					rep:      1,
					affinity: affinityNode,
				},
			},
		}

		//Schedule Job in the selected Node
		By("Creating the PodGroup with one task replicas")
		_, pendingpg := createJob(context, maxpodJobTwo)
		By("Waiting for the PodGroup status to remain pending")
		err := waitTimeoutPodGroupReady(context, pendingpg, oneMinute)
		if err != wait.ErrWaitTimeout {
			checkError(context, err)
		}

	})
})
