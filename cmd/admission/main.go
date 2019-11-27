/*
Copyright 2018 The Volcano Authors.

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
package main

import (
	"flag"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	"volcano.sh/volcano/cmd/admission/app"
	"volcano.sh/volcano/cmd/admission/app/options"
	"volcano.sh/volcano/pkg/admission"
	"volcano.sh/volcano/pkg/version"
)

func serveJobs(w http.ResponseWriter, r *http.Request) {
	admission.Serve(w, r, admission.AdmitJobs)
}

func serveMutateJobs(w http.ResponseWriter, r *http.Request) {
	admission.Serve(w, r, admission.MutateJobs)
}

func main() {
	config := options.NewConfig()
	config.AddFlags()
	flag.Parse()

	if config.PrintVersion {
		version.PrintVersionAndExit()
	}

	http.HandleFunc(admission.AdmitJobPath, serveJobs)
	http.HandleFunc(admission.MutateJobPath, serveMutateJobs)

	if err := config.CheckPortOrDie(); err != nil {
		klog.Fatalf("Configured port is invalid: %v", err)
	}
	addr := ":" + strconv.Itoa(config.Port)

	restConfig, err := clientcmd.BuildConfigFromFlags(config.Master, config.Kubeconfig)
	if err != nil {
		klog.Fatalf("Unable to build k8s config: %v", err)
	}

	admission.VolcanoClientSet = app.GetVolcanoClient(restConfig)

	servePods(config)

	caBundle, err := ioutil.ReadFile(config.CaCertFile)
	if err != nil {
		klog.Fatalf("Unable to read cacert file: %v", err)
	}

	err = options.RegisterWebhooks(config, app.GetClient(restConfig), caBundle)
	if err != nil {
		klog.Fatalf("Unable to register webhook configs: %v", err)
	}

	stopChannel := make(chan os.Signal)
	signal.Notify(stopChannel, syscall.SIGTERM, syscall.SIGINT)

	server := &http.Server{
		Addr:      addr,
		TLSConfig: app.ConfigTLS(config, restConfig),
	}
	webhookServeError := make(chan struct{})
	go func() {
		err = server.ListenAndServeTLS("", "")
		if err != nil && err != http.ErrServerClosed {
			klog.Fatalf("ListenAndServeTLS for admission webhook failed: %v", err)
			close(webhookServeError)
		}
	}()

	select {
	case <-stopChannel:
		if err := server.Close(); err != nil {
			klog.Fatalf("Close admission server failed: %v", err)
		}
		return
	case <-webhookServeError:
		return
	}
}

func servePods(config *options.Config) {
	admController := &admission.Controller{
		VcClients:     admission.VolcanoClientSet,
		SchedulerName: config.SchedulerName,
	}
	http.HandleFunc(admission.AdmitPodPath, admController.ServerPods)

	return
}
