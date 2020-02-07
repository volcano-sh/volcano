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

package app

import (
	"k8s.io/api/admissionregistration/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/klog"

	"volcano.sh/volcano/cmd/webhook-manager/app/options"
	"volcano.sh/volcano/pkg/webhooks/admission/router"
)

func registerAdmissionConfig(kubeClient *kubernetes.Clientset, config *options.Config, service *router.AdmissionService, caBundle []byte) {
	clientConfig := v1beta1.WebhookClientConfig{
		CABundle: caBundle,
	}
	if config.WebhookURL != "" {
		url := config.WebhookURL + service.Path
		clientConfig.URL = &url
		klog.Infof("The URL of webhook manager is <%s>.", url)
	}
	if config.WebhookName != "" && config.WebhookNamespace != "" {
		clientConfig.Service = &v1beta1.ServiceReference{
			Name:      config.WebhookName,
			Namespace: config.WebhookNamespace,
			Path:      &service.Path,
		}
		klog.Infof("The service of webhook manager is <%s/%s/%s>.",
			config.WebhookName, config.WebhookNamespace, service.Path)
	}
	if service.MutatingConfig != nil {
		for i := range service.MutatingConfig.Webhooks {
			service.MutatingConfig.Webhooks[i].ClientConfig = clientConfig
		}

		service.MutatingConfig.ObjectMeta.Name = webhookConfigName(config.WebhookName, service.Path)

		if err := registerMutateWebhook(kubeClient, service.MutatingConfig); err != nil {
			klog.Errorf("Failed to register mutating admission webhook (%s): %v",
				service.Path, err)
		} else {
			klog.V(3).Infof("Registered mutating webhook for path <%s>.", service.Path)
		}
	}
	if service.ValidatingConfig != nil {
		for i := range service.ValidatingConfig.Webhooks {
			service.ValidatingConfig.Webhooks[i].ClientConfig = clientConfig
		}

		service.ValidatingConfig.ObjectMeta.Name = webhookConfigName(config.WebhookName, service.Path)

		if err := registerValidateWebhook(kubeClient, service.ValidatingConfig); err != nil {
			klog.Errorf("Failed to register validating admission webhook (%s): %v",
				service.Path, err)
		} else {
			klog.V(3).Infof("Registered validating webhook for path <%s>.", service.Path)
		}
	}
}

func registerMutateWebhook(clientset *kubernetes.Clientset, hook *v1beta1.MutatingWebhookConfiguration) error {
	client := clientset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations()
	existing, err := client.Get(hook.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err == nil && existing != nil {
		klog.V(4).Infof("Updating MutatingWebhookConfiguration %v", hook)
		existing.Webhooks = hook.Webhooks
		if _, err := client.Update(existing); err != nil {
			return err
		}
	} else {
		klog.V(4).Infof("Creating MutatingWebhookConfiguration %v", hook)
		if _, err := client.Create(hook); err != nil {
			return err
		}
	}

	return nil
}

func registerValidateWebhook(clientset *kubernetes.Clientset, hook *v1beta1.ValidatingWebhookConfiguration) error {
	client := clientset.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations()

	existing, err := client.Get(hook.Name, metav1.GetOptions{})
	if err != nil && !apierrors.IsNotFound(err) {
		return err
	}
	if err == nil && existing != nil {
		existing.Webhooks = hook.Webhooks
		klog.V(4).Infof("Updating ValidatingWebhookConfiguration %v", hook)
		if _, err := client.Update(existing); err != nil {
			return err
		}
	} else {
		klog.V(4).Infof("Creating ValidatingWebhookConfiguration %v", hook)
		if _, err := client.Create(hook); err != nil {
			return err
		}
	}

	return nil
}
