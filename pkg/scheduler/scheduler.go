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

package scheduler

import (
	"time"

	"github.com/golang/glog"

	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	schedcache "github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/cache"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/scheduler/framework"
)

type Scheduler struct {
	cache  schedcache.Cache
	config *rest.Config
}

func NewScheduler(config *rest.Config, schedulerName string) (*Scheduler, error) {
	scheduler := &Scheduler{
		config: config,
		cache:  schedcache.New(config, schedulerName),
	}

	return scheduler, nil
}

func (pc *Scheduler) Run(stopCh <-chan struct{}) {
	createSchedulingSpecKind(pc.config)

	// Start cache for policy.
	go pc.cache.Run(stopCh)
	pc.cache.WaitForCacheSync(stopCh)

	go wait.Until(pc.runOnce, 2*time.Second, stopCh)
}

func (pc *Scheduler) runOnce() {
	glog.V(4).Infof("Start scheduling ...")
	defer glog.V(4).Infof("End scheduling ...")

	ssn := framework.OpenSession(pc.cache)
	defer framework.CloseSession(ssn)

	for _, action := range Actions {
		action.Execute(ssn)
	}

}

func createSchedulingSpecKind(config *rest.Config) error {
	extensionscs, err := apiextensionsclient.NewForConfig(config)
	if err != nil {
		return err
	}
	_, err = client.CreateSchedulingSpecKind(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
