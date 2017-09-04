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

package app

import (
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kubernetes-incubator/kube-arbitrator/cmd/app/options"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/controller"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/schedulercache"
)

func buildConfig(master, kubeconfig string) (*rest.Config, error) {
	if kubeconfig != "" || master != "" {
		return clientcmd.BuildConfigFromFlags(master, kubeconfig)
	}
	return rest.InClusterConfig()
}

func Run(opt *options.ServerOption) error {
	config, err := buildConfig(opt.Master, "")
	if err != nil {
		return err
	}

	kubecli := kubernetes.NewForConfigOrDie(config)
	informerFactory := informers.NewSharedInformerFactory(kubecli, 0)

	nodeInformer := informerFactory.Core().V1().Nodes()
	podInformer := informerFactory.Core().V1().Pods()

	cache := schedulercache.New(nodeInformer, podInformer, config)

	rqaController := controller.New(cache)

	go cache.Run()

	rqaController.Run()

	return nil
}
