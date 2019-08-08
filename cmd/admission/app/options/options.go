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

package options

import (
	"flag"
	"fmt"

	"github.com/golang/glog"

	"k8s.io/api/admissionregistration/v1beta1"
	"k8s.io/client-go/kubernetes"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	admissionregistrationv1beta1client "k8s.io/client-go/kubernetes/typed/admissionregistration/v1beta1"
)

const (
	defaultSchedulerName = "volcano"
)

// Config admission-controller server config.
type Config struct {
	Master                    string
	Kubeconfig                string
	CertFile                  string
	KeyFile                   string
	CaCertFile                string
	Port                      int
	MutateWebhookConfigName   string
	MutateWebhookName         string
	ValidateWebhookConfigName string
	ValidateWebhookName       string
	PrintVersion              bool
	AdmissionServiceName      string
	AdmissionServiceNamespace string
	SchedulerName             string
}

// NewConfig create new config
func NewConfig() *Config {
	c := Config{}
	return &c
}

// AddFlags add flags
func (c *Config) AddFlags() {
	flag.StringVar(&c.Master, "master", c.Master, "The address of the Kubernetes API server (overrides any value in kubeconfig)")
	flag.StringVar(&c.Kubeconfig, "kubeconfig", c.Kubeconfig, "Path to kubeconfig file with authorization and master location information.")
	flag.StringVar(&c.CertFile, "tls-cert-file", c.CertFile, ""+
		"File containing the default x509 Certificate for HTTPS. (CA cert, if any, concatenated "+
		"after server cert).")
	flag.StringVar(&c.KeyFile, "tls-private-key-file", c.KeyFile, "File containing the default x509 private key matching --tls-cert-file.")
	flag.StringVar(&c.CaCertFile, "ca-cert-file", c.CaCertFile, "File containing the x509 Certificate for HTTPS.")
	flag.IntVar(&c.Port, "port", 443, "the port used by admission-controller-server.")
	flag.StringVar(&c.MutateWebhookConfigName, "mutate-webhook-config-name", "",
		"Name of the mutatingwebhookconfiguration resource in Kubernetes [Deprecated]: it will be generated when not specified.")
	flag.StringVar(&c.MutateWebhookName, "mutate-webhook-name", "",
		"Name of the webhook entry in the webhook config. [Deprecated]: it will be generated when not specified")
	flag.StringVar(&c.ValidateWebhookConfigName, "validate-webhook-config-name", "",
		"Name of the mutatingwebhookconfiguration resource in Kubernetes. [Deprecated]: it will be generated when not specified")
	flag.StringVar(&c.ValidateWebhookName, "validate-webhook-name", "",
		"Name of the webhook entry in the webhook config. [Deprecated]: it will be generated when not specified")
	flag.BoolVar(&c.PrintVersion, "version", false, "Show version and quit")
	flag.StringVar(&c.AdmissionServiceNamespace, "webhook-namespace", "default", "The namespace of this webhook")
	flag.StringVar(&c.AdmissionServiceName, "webhook-service-name", "admission-service", "The name of this admission service")
	flag.StringVar(&c.SchedulerName, "scheduler-name", defaultSchedulerName, "Volcano will handle pods whose .spec.SchedulerName is same as scheduler-name")
}

const (
	// ValidateConfigName ValidatingWebhookConfiguration name format
	ValidateConfigName = "%s-validate-job"
	// MutateConfigName MutatingWebhookConfiguration name format
	MutateConfigName = "%s-mutate-job"
	// ValidateHookName Default name for webhooks in ValidatingWebhookConfiguration
	ValidateHookName = "validatejob.volcano.sh"
	// MutateHookName Default name for webhooks in MutatingWebhookConfiguration
	MutateHookName = "mutatejob.volcano.sh"
	// ValidatePodConfigName ValidatingWebhookPodConfiguration name format
	ValidatePodConfigName = "%s-validate-pod"
	// ValidatePodHookName Default name for webhooks in ValidatingWebhookPodConfiguration
	ValidatePodHookName = "validatepod.volcano.sh"
)

// CheckPortOrDie check valid port range
func (c *Config) CheckPortOrDie() error {
	if c.Port < 1 || c.Port > 65535 {
		return fmt.Errorf("the port should be in the range of 1 and 65535")
	}
	return nil
}

func useGeneratedNameIfRequired(configured, generated string) string {
	if configured != "" {
		return configured
	}
	return generated
}

// RegisterWebhooks register webhooks for admission service
func RegisterWebhooks(c *Config, clienset *kubernetes.Clientset, cabundle []byte) error {
	ignorePolicy := v1beta1.Ignore

	//Prepare validate webhooks
	path := "/jobs"
	JobValidateHooks := v1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: useGeneratedNameIfRequired(c.ValidateWebhookConfigName,
				fmt.Sprintf(ValidateConfigName, c.AdmissionServiceName)),
		},
		Webhooks: []v1beta1.Webhook{{
			Name: useGeneratedNameIfRequired(c.ValidateWebhookName, ValidateHookName),
			Rules: []v1beta1.RuleWithOperations{
				{
					Operations: []v1beta1.OperationType{v1beta1.Create, v1beta1.Update},
					Rule: v1beta1.Rule{
						APIGroups:   []string{"batch.volcano.sh"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"jobs"},
					},
				},
			},
			ClientConfig: v1beta1.WebhookClientConfig{
				Service: &v1beta1.ServiceReference{
					Name:      c.AdmissionServiceName,
					Namespace: c.AdmissionServiceNamespace,
					Path:      &path,
				},
				CABundle: cabundle,
			},
			FailurePolicy: &ignorePolicy,
		}},
	}

	if err := registerValidateWebhook(clienset.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations(),
		[]v1beta1.ValidatingWebhookConfiguration{JobValidateHooks}); err != nil {
		return err
	}

	//Prepare mutate jobs
	path = "/mutating-jobs"
	JobMutateHooks := v1beta1.MutatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: useGeneratedNameIfRequired(c.MutateWebhookConfigName,
				fmt.Sprintf(MutateConfigName, c.AdmissionServiceName)),
		},
		Webhooks: []v1beta1.Webhook{{
			Name: useGeneratedNameIfRequired(c.MutateWebhookName, MutateHookName),
			Rules: []v1beta1.RuleWithOperations{
				{
					Operations: []v1beta1.OperationType{v1beta1.Create},
					Rule: v1beta1.Rule{
						APIGroups:   []string{"batch.volcano.sh"},
						APIVersions: []string{"v1alpha1"},
						Resources:   []string{"jobs"},
					},
				},
			},
			ClientConfig: v1beta1.WebhookClientConfig{
				Service: &v1beta1.ServiceReference{
					Name:      c.AdmissionServiceName,
					Namespace: c.AdmissionServiceNamespace,
					Path:      &path,
				},
				CABundle: cabundle,
			},
			FailurePolicy: &ignorePolicy,
		}},
	}

	if err := registerMutateWebhook(clienset.AdmissionregistrationV1beta1().MutatingWebhookConfigurations(),
		[]v1beta1.MutatingWebhookConfiguration{JobMutateHooks}); err != nil {
		return err
	}

	// Prepare validate pods
	path = "/pods"
	PodValidateHooks := v1beta1.ValidatingWebhookConfiguration{
		ObjectMeta: metav1.ObjectMeta{
			Name: useGeneratedNameIfRequired("",
				fmt.Sprintf(ValidatePodConfigName, c.AdmissionServiceName)),
		},
		Webhooks: []v1beta1.Webhook{{
			Name: useGeneratedNameIfRequired("", ValidatePodHookName),
			Rules: []v1beta1.RuleWithOperations{
				{
					Operations: []v1beta1.OperationType{v1beta1.Create},
					Rule: v1beta1.Rule{
						APIGroups:   []string{""},
						APIVersions: []string{"v1"},
						Resources:   []string{"pods"},
					},
				},
			},
			ClientConfig: v1beta1.WebhookClientConfig{
				Service: &v1beta1.ServiceReference{
					Name:      c.AdmissionServiceName,
					Namespace: c.AdmissionServiceNamespace,
					Path:      &path,
				},
				CABundle: cabundle,
			},
			FailurePolicy: &ignorePolicy,
		}},
	}

	if err := registerValidateWebhook(clienset.AdmissionregistrationV1beta1().ValidatingWebhookConfigurations(),
		[]v1beta1.ValidatingWebhookConfiguration{PodValidateHooks}); err != nil {
		return err
	}

	return nil

}

func registerMutateWebhook(client admissionregistrationv1beta1client.MutatingWebhookConfigurationInterface,
	webhooks []v1beta1.MutatingWebhookConfiguration) error {
	for _, hook := range webhooks {
		existing, err := client.Get(hook.Name, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if err == nil && existing != nil {
			glog.Infof("Updating MutatingWebhookConfiguration %v", hook)
			existing.Webhooks = hook.Webhooks
			if _, err := client.Update(existing); err != nil {
				return err
			}
		} else {
			glog.Infof("Creating MutatingWebhookConfiguration %v", hook)
			if _, err := client.Create(&hook); err != nil {
				return err
			}
		}
	}
	return nil
}

func registerValidateWebhook(client admissionregistrationv1beta1client.ValidatingWebhookConfigurationInterface,
	webhooks []v1beta1.ValidatingWebhookConfiguration) error {
	for _, hook := range webhooks {
		existing, err := client.Get(hook.Name, metav1.GetOptions{})
		if err != nil && !apierrors.IsNotFound(err) {
			return err
		}
		if err == nil && existing != nil {
			existing.Webhooks = hook.Webhooks
			glog.Infof("Updating ValidatingWebhookConfiguration %v", hook)
			if _, err := client.Update(existing); err != nil {
				return err
			}
		} else {
			glog.Infof("Creating ValidatingWebhookConfiguration %v", hook)
			if _, err := client.Create(&hook); err != nil {
				return err
			}
		}
	}
	return nil
}
