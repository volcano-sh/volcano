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

package app

import (
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	clientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/internalclientset"
	apiextensionsinformers "k8s.io/apiextensions-apiserver/pkg/client/informers/internalversion"
	"k8s.io/klog"

	"volcano.sh/volcano/cmd/webhook-manager/app/options"
	"volcano.sh/volcano/pkg/version"
	admrouter "volcano.sh/volcano/pkg/webhooks/admission/router"
	convrouter "volcano.sh/volcano/pkg/webhooks/conversion/router"
)

// Run start the service of admission controller.
func Run(config *options.Config) error {
	if config.PrintVersion {
		version.PrintVersionAndExit()
		return nil
	}

	if config.WebhookURL == "" && config.WebhookNamespace == "" && config.WebhookName == "" {
		return fmt.Errorf("failed to start webhooks as both 'url' and 'namespace/name' of webhook are empty")
	}

	restConfig, err := buildConfig(config)
	if err != nil {
		return fmt.Errorf("unable to build k8s config: %v", err)
	}

	caBundle, err := ioutil.ReadFile(config.CaCertFile)
	if err != nil {
		return fmt.Errorf("unable to read cacert file (%s): %v", config.CaCertFile, err)
	}

	vClient := getVolcanoClient(restConfig)
	kubeClient := getKubeClient(restConfig)
	admrouter.ForEachAdmission(func(service *admrouter.AdmissionService) {
		if service.Config != nil {
			service.Config.VolcanoClient = vClient
			service.Config.SchedulerName = config.SchedulerName
		}

		klog.V(3).Infof("Registered '%s' as webhook.", service.Path)
		http.HandleFunc(service.Path, service.Handler)

		klog.V(3).Infof("Registered configuration for webhook <%s>", service.Path)
		registerAdmissionConfig(kubeClient, config, service, caBundle)
	})

	apiExtClient, _ := clientset.NewForConfig(restConfig)
	informerFactory := apiextensionsinformers.NewSharedInformerFactory(apiExtClient, 0)
	crdInformer := informerFactory.Apiextensions().InternalVersion().CustomResourceDefinitions()
	convctrl := convrouter.NewController(crdInformer, apiExtClient)

	convrouter.ForEachConversion(func(service *convrouter.ConversionService) {
		if service.Config != nil {
			service.Config.VolcanoClient = vClient
			service.Config.SchedulerName = config.SchedulerName
		}

		klog.V(3).Infof("Registered '%s' as webhook.", service.Path)
		http.HandleFunc(service.Path, service.Handler)

		klog.V(3).Infof("Registered configuration for webhook <%s>", service.Path)
		clientConfig := buildConversionWebhookConfig(config, service, caBundle)
		for _, name := range service.Names {
			convctrl.RegisterWebhookConfig(name, clientConfig)
		}
	})

	go informerFactory.Start(nil)
	go convctrl.Run(nil)

	webhookServeError := make(chan struct{})
	stopChannel := make(chan os.Signal)
	signal.Notify(stopChannel, syscall.SIGTERM, syscall.SIGINT)

	server := &http.Server{
		Addr:      ":" + strconv.Itoa(config.Port),
		TLSConfig: configTLS(config, restConfig),
	}
	go func() {
		err = server.ListenAndServeTLS("", "")
		if err != nil && err != http.ErrServerClosed {
			klog.Fatalf("ListenAndServeTLS for admission webhook failed: %v", err)
			close(webhookServeError)
		}

		klog.Info("Volcano Webhook manager started.")
	}()

	select {
	case <-stopChannel:
		if err := server.Close(); err != nil {
			return fmt.Errorf("close admission server failed: %v", err)
		}
		return nil
	case <-webhookServeError:
		return fmt.Errorf("unknown webhook server error")
	}
}
