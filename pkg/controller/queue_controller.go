package controller

import (
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/client-go/rest"

	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
)

type QueueController struct {
	config *rest.Config
}

func NewQueueController(config *rest.Config) *QueueController {
	return &QueueController{
		config: config,
	}
}

func (qc *QueueController) Run(stopCh chan struct{}) {
	// initialized
	qc.createQueueCRD()
	qc.createQueueJobCRD()
}

func (qc *QueueController) createQueueCRD() error {
	extensionscs, err := apiextensionsclient.NewForConfig(qc.config)
	if err != nil {
		return err
	}
	_, err = client.CreateQueueCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (qc *QueueController) createQueueJobCRD() error {
	extensionscs, err := apiextensionsclient.NewForConfig(qc.config)
	if err != nil {
		return err
	}
	_, err = client.CreateQueueJobCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
