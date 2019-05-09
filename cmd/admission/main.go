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
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strconv"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"volcano.sh/volcano/cmd/admission/app"
	appConf "volcano.sh/volcano/cmd/admission/app/configure"
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
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
	addr := ":" + strconv.Itoa(config.Port)

	restConfig, err := clientcmd.BuildConfigFromFlags(config.Master, config.Kubeconfig)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	clientset := app.GetClient(restConfig)

	admissioncontroller.KubeBatchClientSet = app.GetKubeBatchClient(restConfig)

	if err := servePods(clientset, config); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}

	caCertPem, err := ioutil.ReadFile(config.CaCertFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
	} else {
		// patch caBundle in webhook
		if err = appConf.PatchMutateWebhookConfig(clientset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
			config.MutateWebhookConfigName, config.MutateWebhookName, caCertPem); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
		if err = appConf.PatchValidateWebhookConfig(clientset.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations(),
			config.ValidateWebhookConfigName, config.ValidateWebhookName, caCertPem); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
		if err = appConf.PatchValidateWebhookConfig(clientset.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations(),
			config.ValidateWebhookPodConfigName, config.ValidateWebhookPodName, caCertPem); err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
		}
	}

	server := &http.Server{
		Addr:      addr,
		TLSConfig: app.ConfigTLS(config, restConfig),
	}
	server.ListenAndServeTLS("", "")
}

func servePods(clientset *kubernetes.Clientset, config *appConf.Config) error {
	kbClientset, err := app.GetSchedulerClient(config)
	if err != nil {
		return err
	}

	admController := &admissioncontroller.Controller{
		KubeClients:   clientset,
		KbClients:     kbClientset,
		SchedulerName: config.SchedulerName,
	}
	http.HandleFunc(admissioncontroller.AdmitPodPath, admController.ServerPods)

	return nil
}
