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

package app

import (
	"fmt"
	"net"
	"net/http"
	"strconv"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	corev1 "k8s.io/api/core/v1"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	typedv1 "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/controller-manager/pkg/clientbuilder"
	"k8s.io/klog/v2"

	"volcano.sh/apis/pkg/apis/helpers"
	"volcano.sh/volcano/cmd/agent/app/options"
	"volcano.sh/volcano/pkg/agent/apis"
	"volcano.sh/volcano/pkg/agent/healthcheck"
	"volcano.sh/volcano/pkg/agent/utils"
	"volcano.sh/volcano/pkg/config"
)

func NewConfiguration(opts *options.VolcanoAgentOptions) (*config.Configuration, error) {
	conf := config.NewConfiguration()
	if err := opts.Validate(); err != nil {
		return conf, err
	}
	if err := opts.ApplyTo(conf); err != nil {
		return conf, err
	}

	if conf.GenericConfiguration.ExtendResourceCPUName != "" {
		apis.SetExtendResourceCPU(conf.GenericConfiguration.ExtendResourceCPUName)
	}

	if conf.GenericConfiguration.ExtendResourceMemoryName != "" {
		apis.SetExtendResourceMemory(conf.GenericConfiguration.ExtendResourceMemoryName)
	}

	klog.InfoS("Set extend resource", "cpu", apis.ExtendResourceCPU, "memory", apis.ExtendResourceMemory)

	kubeConfig, err := restclient.InClusterConfig()
	if err != nil {
		return conf, fmt.Errorf("failed to create kubeconfig: %v", err)
	}

	kubeClient, err := clientset.NewForConfig(restclient.AddUserAgent(kubeConfig, utils.Component))
	if err != nil {
		return conf, fmt.Errorf("failed to create kubeclient: %v", err)
	}
	conf.GenericConfiguration.KubeClient = kubeClient

	broadcaster := record.NewBroadcaster()
	broadcaster.StartLogging(klog.Infof)
	broadcaster.StartStructuredLogging(2)
	broadcaster.StartRecordingToSink(&typedv1.EventSinkImpl{Interface: kubeClient.CoreV1().Events("")})
	recorder := broadcaster.NewRecorder(scheme.Scheme, corev1.EventSource{Component: "volcano-agent"})
	conf.GenericConfiguration.Recorder = recorder

	rootClientBuilder := clientbuilder.SimpleControllerClientBuilder{
		ClientConfig: kubeConfig,
	}
	versionedClient := rootClientBuilder.ClientOrDie("shared-informers")
	conf.Complete(versionedClient)
	return conf, nil
}

// RunServer will run both health check and metrics server.
func RunServer(checker healthcheck.HealthChecker, address string, port int) {
	go func() {
		klog.InfoS("Start http health check server")
		mux := http.NewServeMux()
		mux.HandleFunc("/healthz", checker.HealthCheck)
		mux.Handle("/metrics", promhttp.Handler())
		s := &http.Server{
			Addr:              net.JoinHostPort(address, strconv.Itoa(port)),
			Handler:           mux,
			ReadHeaderTimeout: helpers.DefaultReadHeaderTimeout,
			ReadTimeout:       helpers.DefaultReadTimeout,
			WriteTimeout:      helpers.DefaultWriteTimeout,
		}
		if err := s.ListenAndServe(); err != nil {
			klog.Fatalf("failed to start health check server: %v", err)
		}
	}()
}
