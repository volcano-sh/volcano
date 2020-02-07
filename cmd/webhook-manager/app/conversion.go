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
	"k8s.io/apiextensions-apiserver/pkg/apis/apiextensions"
	"k8s.io/klog"

	"volcano.sh/volcano/cmd/webhook-manager/app/options"
	"volcano.sh/volcano/pkg/webhooks/conversion/router"
)

func buildConversionWebhookConfig(config *options.Config, service *router.ConversionService, caBundle []byte) *apiextensions.WebhookClientConfig {
	clientConfig := &apiextensions.WebhookClientConfig{
		CABundle: caBundle,
	}
	if config.WebhookURL != "" {
		url := config.WebhookURL + service.Path
		clientConfig.URL = &url
		klog.Infof("The URL of webhook manager is <%s>.", url)
	}
	if config.WebhookName != "" && config.WebhookNamespace != "" {
		clientConfig.Service = &apiextensions.ServiceReference{
			Name:      config.WebhookName,
			Namespace: config.WebhookNamespace,
			Path:      &service.Path,
		}
		klog.Infof("The service of webhook manager is <%s/%s/%s>.",
			config.WebhookName, config.WebhookNamespace, service.Path)
	}

	return clientConfig
}
