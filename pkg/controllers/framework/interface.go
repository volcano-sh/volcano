/*
Copyright 2020 The Volcano Authors.

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

package framework

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"

	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
)

// ControllerOption is the main context object for the controllers.
type ControllerOption struct {
	KubeClient              kubernetes.Interface
	VolcanoClient           vcclientset.Interface
	SharedInformerFactory   informers.SharedInformerFactory
	VCSharedInformerFactory vcinformer.SharedInformerFactory
	SchedulerNames          []string
	WorkerNum               uint32
	MaxRequeueNum           int

	InheritOwnerAnnotations bool
	WorkerThreadsForPG      uint32
	WorkerThreadsForQueue   uint32
	WorkerThreadsForGC      uint32

	// Config holds the common attributes that can be passed to a Kubernetes client
	// and controllers registered by the users can use it.
	Config *rest.Config
}

// Controller is the interface of all controllers.
type Controller interface {
	Name() string
	Initialize(opt *ControllerOption) error
	// Run run the controller
	Run(stopCh <-chan struct{})
}
