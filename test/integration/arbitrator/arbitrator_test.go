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

package arbitrator

import (
	"fmt"
	"testing"
	"time"

	apiv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/controller"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/policy/proportion"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
	"github.com/kubernetes-incubator/kube-arbitrator/test/integration/framework"

	"k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clientset "k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
)

// prepareNamespace prepare two namespaces "ns01" and "ns02"
func prepareNamespace(cs *clientset.Clientset) error {
	ns01 := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ns01",
		},
	}
	ns02 := &v1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ns02",
		},
	}

	_, err := cs.CoreV1().Namespaces().Create(ns01)
	if err != nil {
		return fmt.Errorf("fail to create namespace ns01, %#v", err)
	}
	_, err = cs.CoreV1().Namespaces().Create(ns02)
	if err != nil {
		return fmt.Errorf("fail to create namespace ns02, %#v", err)
	}

	return nil
}

// prepareNode prepare one node "node01" which contain 15 cpus and 15Gi memory
func prepareNode(cs *clientset.Clientset) error {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:         "node01",
			GenerateName: "node01",
		},
		Spec: v1.NodeSpec{
			ExternalID: "foo",
		},
		Status: v1.NodeStatus{
			Capacity: v1.ResourceList{
				v1.ResourceCPU:    resource.MustParse("15"),
				v1.ResourceMemory: resource.MustParse("15Gi"),
			},
			Phase: v1.NodeRunning,
			Conditions: []v1.NodeCondition{
				{Type: v1.NodeReady, Status: v1.ConditionTrue},
			},
		},
	}

	_, err := cs.CoreV1().Nodes().Create(node)
	if err != nil {
		return fmt.Errorf("fail to create node node01, %#v", err)
	}

	return nil
}

// prepareResourceQuota prepare two resource quotas "rq01" and "rq02"
// "rq01" is under namespace "ns01", "rq02" is under namespace "ns02"
// "rq01" and "rq02" will not limit cpu and memory usage at first
func prepareResourceQuota(cs *clientset.Clientset) error {
	rq01 := &v1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rq01",
			Namespace: "ns01",
		},
		Spec: v1.ResourceQuotaSpec{
			Hard: v1.ResourceList{
				v1.ResourcePods: resource.MustParse("1000"),
			},
		},
	}
	rq02 := &v1.ResourceQuota{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rq02",
			Namespace: "ns02",
		},
		Spec: v1.ResourceQuotaSpec{
			Hard: v1.ResourceList{
				v1.ResourcePods: resource.MustParse("1000"),
			},
		},
	}

	_, err := cs.CoreV1().ResourceQuotas(rq01.Namespace).Create(rq01)
	if err != nil {
		return fmt.Errorf("fail to create quota rq01, %#v", err)
	}
	_, err = cs.CoreV1().ResourceQuotas(rq02.Namespace).Create(rq02)
	if err != nil {
		return fmt.Errorf("fail to create quota rq02, %#v", err)
	}

	return nil
}

// prepareCRD prepare customer resource definition "ResourceQuotaAllocator"
// create two ResourceQuotaAllocator, "queue01" and "queue02"
// "queue01" is under namespace "ns01" and has attribute "weight=1"
// "queue02" is under namespace "ns02" and has attribute "weight=2"
func prepareCRD(config *restclient.Config) error {
	extensionscs, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("fail to create crd config, %#v", err)
	}

	_, err = client.CreateQueueCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("fail to create crd, %#v", err)
	}

	crdClient, _, err := client.NewClient(config)
	if err != nil {
		return fmt.Errorf("fail to create crd client, %#v", err)
	}

	crd01 := &apiv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "queue01",
			Namespace: "ns01",
		},
		Spec: apiv1.QueueSpec{
			Weight: 1,
		},
	}
	crd02 := &apiv1.Queue{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "queue02",
			Namespace: "ns02",
		},
		Spec: apiv1.QueueSpec{
			Weight: 2,
		},
	}

	var result apiv1.Queue
	err = crdClient.Post().
		Resource(apiv1.QueuePlural).
		Namespace(crd01.Namespace).
		Body(crd01).
		Do().Into(&result)
	if err != nil {
		return fmt.Errorf("fail to create crd crd01, %#v", err)
	}
	err = crdClient.Post().
		Resource(apiv1.QueuePlural).
		Namespace(crd02.Namespace).
		Body(crd02).
		Do().Into(&result)
	if err != nil {
		return fmt.Errorf("fail to create crd crd02, %#v", err)
	}

	return nil
}

func TestArbitrator(t *testing.T) {
	config, tearDown := framework.StartTestServerOrDie(t)
	defer tearDown()

	cs := clientset.NewForConfigOrDie(config)
	defer cs.CoreV1().Nodes().DeleteCollection(nil, metav1.ListOptions{})

	err := prepareNamespace(cs)
	if err != nil {
		t.Fatalf("fail to prepare namespaces, %#v", err)
	}

	err = prepareNode(cs)
	if err != nil {
		t.Fatalf("fail to prepare node, %#v", err)
	}

	err = prepareResourceQuota(cs)
	if err != nil {
		t.Fatalf("fail to prepare resource quota, %#v", err)
	}

	err = prepareCRD(config)
	if err != nil {
		t.Fatalf("fail to prepare CRD, %#v", err)
	}

	neverStop := make(chan struct{})
	defer close(neverStop)
	cache := schedulercache.New(config)
	go cache.Run(neverStop)
	c := controller.NewQueueController(config, cache, proportion.New())
	go c.Run()

	// sleep to wait scheduler finish
	time.Sleep(10 * time.Second)

	// verify scheduler result
	rq01, _ := cs.CoreV1().ResourceQuotas("ns01").Get("rq01", metav1.GetOptions{})
	cpu01 := rq01.Spec.Hard["limits.cpu"]
	if v, _ := (&cpu01).AsInt64(); v != int64(5) {
		t.Fatalf("after scheduler, cpu is not 5 for rq01, %#v", rq01)
	}
	rq02, _ := cs.CoreV1().ResourceQuotas("ns02").Get("rq02", metav1.GetOptions{})
	cpu02 := rq02.Spec.Hard["limits.cpu"]
	if v, _ := (&cpu02).AsInt64(); v != int64(10) {
		t.Fatalf("after scheduler, cpu is not 10 for rq02, %#v", rq02)
	}
}
