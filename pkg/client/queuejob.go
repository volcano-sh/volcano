/*
Copyright 2017 The Kubernetes Authors.

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

package client

import (
	"fmt"
	"reflect"
	"time"

	qjobv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	apiextv1beta1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1beta1"
	apiextclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/rest"
)

const (
	QueueJobPlural   string = "queuejobs"
	QueueJobGroup    string = qjobv1.GroupName
	QueueJobVersion  string = "v1"
	FullQueueJobName string = QueueJobPlural + "." + QueueJobGroup
)

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(qjobv1.SchemeGroupVersion,
		&qjobv1.QueueJob{},
		&qjobv1.QueueJobList{},
	)
	metav1.AddToGroupVersion(scheme, qjobv1.SchemeGroupVersion)
	return nil
}

func NewQueueJobClient(cfg *rest.Config) (*rest.RESTClient, *runtime.Scheme, error) {
	scheme := runtime.NewScheme()
	if err := qjobv1.AddToScheme(scheme); err != nil {
		return nil, nil, err
	}

	config := *cfg
	config.GroupVersion = &qjobv1.SchemeGroupVersion
	config.APIPath = "/apis"
	config.ContentType = runtime.ContentTypeJSON
	config.NegotiatedSerializer = serializer.DirectCodecFactory{
		CodecFactory: serializer.NewCodecFactory(scheme)}

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, nil, err
	}
	return client, scheme, nil
}

func CreateQueueJob(clientset apiextclient.Interface) (*apiextv1beta1.CustomResourceDefinition, error) {
	qjob := &apiextv1beta1.CustomResourceDefinition{
		ObjectMeta: metav1.ObjectMeta{
			Name: FullQueueJobName,
		},
		Spec: apiextv1beta1.CustomResourceDefinitionSpec{
			Group:   QueueJobGroup,
			Version: QueueJobVersion,
			Scope:   apiextv1beta1.NamespaceScoped,
			Names: apiextv1beta1.CustomResourceDefinitionNames{
				Plural: QueueJobPlural,
				Kind:   reflect.TypeOf(qjobv1.QueueJob{}).Name(),
			},
		},
	}

	_, err := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Create(qjob)
	if err != nil && apierrors.IsAlreadyExists(err) {
		return nil, err
	}
	return nil, err

	// wait for QueueJob being established
	err = wait.Poll(500*time.Millisecond, 60*time.Second, func() (bool, error) {
		qjob, err = clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Get(FullQueueJobName, metav1.GetOptions{})
		if err != nil {
			return false, err
		}
		for _, cond := range qjob.Status.Conditions {
			switch cond.Type {
			case apiextv1beta1.Established:
				if cond.Status == apiextv1beta1.ConditionTrue {
					return true, err
				}
			case apiextv1beta1.NamesAccepted:
				if cond.Status == apiextv1beta1.ConditionFalse {
					fmt.Printf("Name conflict: %v\n", cond.Reason)
				}
			}
		}
		return false, err
	})
	if err != nil {
		deleteErr := clientset.ApiextensionsV1beta1().CustomResourceDefinitions().Delete(FullQueueJobName, nil)
		if deleteErr != nil {
			return nil, errors.NewAggregate([]error{err, deleteErr})
		}
		return nil, err
	}
	return qjob, nil
}
