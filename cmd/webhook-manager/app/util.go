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
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"time"

	v1 "k8s.io/api/admissionregistration/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"

	"volcano.sh/apis/pkg/client/clientset/versioned"
	"volcano.sh/volcano/cmd/webhook-manager/app/options"
)

var (
	validatingWebhooksName = []string{
		"volcano-admission-service-jobs-validate",
		"volcano-admission-service-pods-validate",
		"volcano-admission-service-queues-validate",
	}
	mutatingWebhooksName = []string{
		"volcano-admission-service-pods-mutate",
		"volcano-admission-service-queues-mutate",
		"volcano-admission-service-podgroups-mutate",
		"volcano-admission-service-jobs-mutate",
	}
)

func addCaCertForWebhook(kubeClient *kubernetes.Clientset, caBundle []byte) error {
	for _, mutatingWebhookName := range mutatingWebhooksName {
		var mutatingWebhook *v1.MutatingWebhookConfiguration
		webhookChanged := false
		if err := wait.Poll(time.Second, 5*time.Minute, func() (done bool, err error) {
			mutatingWebhook, err = kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Get(context.TODO(), mutatingWebhookName, metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					klog.Errorln(err)
					return false, nil
				}
				return false, fmt.Errorf("failed to get mutating webhook %v", err)
			}
			return true, nil
		}); err != nil {
			return fmt.Errorf("failed to get mutating webhook %v", err)
		}

		for index := 0; index < len(mutatingWebhook.Webhooks); index++ {
			if mutatingWebhook.Webhooks[index].ClientConfig.CABundle == nil ||
				!bytes.Equal(mutatingWebhook.Webhooks[index].ClientConfig.CABundle, caBundle) {
				mutatingWebhook.Webhooks[index].ClientConfig.CABundle = caBundle
				webhookChanged = true
			}
		}
		if webhookChanged {
			if _, err := kubeClient.AdmissionregistrationV1().MutatingWebhookConfigurations().Update(context.TODO(), mutatingWebhook, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("failed to update mutating admission webhooks %v %v", mutatingWebhookName, err)
			}
		}
	}

	for _, validatingWebhookName := range validatingWebhooksName {
		var validatingWebhook *v1.ValidatingWebhookConfiguration
		webhookChanged := false
		if err := wait.Poll(time.Second, 5*time.Minute, func() (done bool, err error) {
			validatingWebhook, err = kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Get(context.TODO(), validatingWebhookName, metav1.GetOptions{})
			if err != nil {
				if apierrors.IsNotFound(err) {
					klog.Errorln(err)
					return false, nil
				}
				return false, fmt.Errorf("failed to get validating webhook %v", err)
			}
			return true, nil
		}); err != nil {
			return fmt.Errorf("failed to get validating webhook %v", err)
		}

		for index := 0; index < len(validatingWebhook.Webhooks); index++ {
			if validatingWebhook.Webhooks[index].ClientConfig.CABundle == nil ||
				!bytes.Equal(validatingWebhook.Webhooks[index].ClientConfig.CABundle, caBundle) {
				validatingWebhook.Webhooks[index].ClientConfig.CABundle = caBundle
				webhookChanged = true
			}
		}
		if webhookChanged {
			if _, err := kubeClient.AdmissionregistrationV1().ValidatingWebhookConfigurations().Update(context.TODO(), validatingWebhook, metav1.UpdateOptions{}); err != nil {
				return fmt.Errorf("failed to update validating admission webhooks %v %v", validatingWebhookName, err)
			}
		}
	}

	return nil
}

// getKubeClient Get a clientset with restConfig.
func getKubeClient(restConfig *rest.Config) *kubernetes.Clientset {
	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		klog.Fatal(err)
	}
	return clientset
}

// GetVolcanoClient get a clientset for volcano.
func getVolcanoClient(restConfig *rest.Config) *versioned.Clientset {
	clientset, err := versioned.NewForConfig(restConfig)
	if err != nil {
		klog.Fatal(err)
	}
	return clientset
}

// configTLS is a helper function that generate tls certificates from directly defined tls config or kubeconfig
// These are passed in as command line for cluster certification. If tls config is passed in, we use the directly
// defined tls config, else use that defined in kubeconfig.
func configTLS(config *options.Config, restConfig *rest.Config) *tls.Config {
	if len(config.CertData) != 0 && len(config.KeyData) != 0 {
		certPool := x509.NewCertPool()
		certPool.AppendCertsFromPEM(config.CaCertData)

		sCert, err := tls.X509KeyPair(config.CertData, config.KeyData)
		if err != nil {
			klog.Fatal(err)
		}

		return &tls.Config{
			Certificates: []tls.Certificate{sCert},
			RootCAs:      certPool,
			MinVersion:   tls.VersionTLS12,
			ClientAuth:   tls.VerifyClientCertIfGiven,
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
				tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
			},
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
