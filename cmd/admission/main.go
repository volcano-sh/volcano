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

	"github.com/golang/glog"

	"k8s.io/client-go/tools/clientcmd"

	"volcano.sh/volcano/cmd/admission/app"
	appConf "volcano.sh/volcano/cmd/admission/app/options"
	admissioncontroller "volcano.sh/volcano/pkg/admission"
	"volcano.sh/volcano/pkg/version"
)

func serveJobs(w http.ResponseWriter, r *http.Request) {
	admissioncontroller.Serve(w, r, admissioncontroller.AdmitJobs)
}

func serveMutateJobs(w http.ResponseWriter, r *http.Request) {
	admissioncontroller.Serve(w, r, admissioncontroller.MutateJobs)
}

func main() {
	config := appConf.NewConfig()
	config.AddFlags()
	flag.Parse()

	if config.PrintVersion {
		version.PrintVersionAndExit()
	}

	http.HandleFunc(admissioncontroller.AdmitJobPath, serveJobs)
	http.HandleFunc(admissioncontroller.MutateJobPath, serveMutateJobs)

	if err := config.CheckPortOrDie(); err != nil {
		glog.Fatalf("Configured port is invalid: %v\n", err)
	}
	addr := ":" + strconv.Itoa(config.Port)

	restConfig, err := clientcmd.BuildConfigFromFlags(config.Master, config.Kubeconfig)
	if err != nil {
		glog.Fatalf("Unable to build k8s config: %v\n", err)
	}

	admissioncontroller.VolcanoClientSet = app.GetVolcanoClient(restConfig)

	servePods(config)

	caBundle, err := ioutil.ReadFile(config.CaCertFile)
	if err != nil {
		glog.Fatalf("Unable to read cacert file: %v\n", err)
	}

	err = appConf.RegisterWebhooks(config, app.GetClient(restConfig), caBundle)
	if err != nil {
		glog.Fatalf("Unable to register webhook configs: %v\n", err)
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
			glog.Fatalf("ListenAndServeTLS for admission webhook failed: %v\n", err)
			close(webhookServeError)
		}
	}()

	select {
	case <-stopChannel:
		if err := server.Close(); err != nil {
			glog.Fatalf("Close admission server failed: %v\n", err)
		}
		return
	case <-webhookServeError:
		return
	}
}

func servePods(config *appConf.Config) {
	admController := &admissioncontroller.Controller{
		VcClients:     admissioncontroller.VolcanoClientSet,
		SchedulerName: config.SchedulerName,
	}
	http.HandleFunc(admissioncontroller.AdmitPodPath, admController.ServerPods)

	return
}
