/*
Copyright 2019 The Volcano Authors.

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
	"crypto/tls"
	"regexp"
	"strings"

	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog"

	"volcano.sh/volcano/cmd/webhook-manager/app/options"
	"volcano.sh/volcano/pkg/client/clientset/versioned"
)

func buildConfig(config *options.Config) (*rest.Config, error) {
	var restConfig *rest.Config
	var err error

	master := config.Master
	kubeconfig := config.Kubeconfig
	if master != "" || kubeconfig != "" {
		restConfig, err = clientcmd.BuildConfigFromFlags(master, kubeconfig)
	} else {
		restConfig, err = rest.InClusterConfig()
	}
	if err != nil {
		return nil, err
	}
	restConfig.QPS = config.KubeAPIQPS
	restConfig.Burst = config.KubeAPIBurst

	return restConfig, nil
}

// getKubeClient Get a clientset with restConfig.
func getKubeClient(restConfig *rest.Config) *kubernetes.Clientset {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		klog.Fatal(err)
	}
	return clientset
}

// GetVolcanoClient get a clientset for volcano
func getVolcanoClient(restConfig *rest.Config) *versioned.Clientset {
	clientset, err := versioned.NewForConfig(restConfig)
	if err != nil {
		klog.Fatal(err)
	}
	return clientset
}

// configTLS is a helper function that generate tls certificates from directly defined tls config or kubeconfig
// These are passed in as command line for cluster certification. If tls config is passed in, we use the directly
// defined tls config, else use that defined in kubeconfig
func configTLS(config *options.Config, restConfig *rest.Config) *tls.Config {
	if len(config.CertFile) != 0 && len(config.KeyFile) != 0 {
		sCert, err := tls.LoadX509KeyPair(config.CertFile, config.KeyFile)
		if err != nil {
			klog.Fatal(err)
		}

		return &tls.Config{
			Certificates: []tls.Certificate{sCert},
		}
	}

	if len(restConfig.CertData) != 0 && len(restConfig.KeyData) != 0 {
		sCert, err := tls.X509KeyPair(restConfig.CertData, restConfig.KeyData)
		if err != nil {
			klog.Fatal(err)
		}

		return &tls.Config{
			Certificates: []tls.Certificate{sCert},
		}
	}

	klog.Fatal("tls: failed to find any tls config data")
	return &tls.Config{}
}

func webhookConfigName(name, path string) string {
	if name == "" {
		name = "webhook"
	}

	re := regexp.MustCompile(`-+`)
	raw := strings.Join([]string{name, strings.ReplaceAll(path, "/", "-")}, "-")
	return re.ReplaceAllString(raw, "-")
}
