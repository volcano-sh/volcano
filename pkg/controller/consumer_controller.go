package controller

import (
	"github.com/golang/glog"

	corev1 "k8s.io/api/core/v1"
	apiextensionsclient "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"

	arbv1 "github.com/kubernetes-incubator/kube-arbitrator/pkg/apis/v1"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client"
	"github.com/kubernetes-incubator/kube-arbitrator/pkg/client/clientset"
)

type ConsumerController struct {
	config     *rest.Config
	arbclient  *clientset.Clientset
	nsInformer v1.NamespaceInformer
}

func NewConsumerController(config *rest.Config) *ConsumerController {
	cc := &ConsumerController{
		config:    config,
		arbclient: clientset.NewForConfigOrDie(config),
	}

	kubecli := kubernetes.NewForConfigOrDie(config)
	informerFactory := informers.NewSharedInformerFactory(kubecli, 0)

	// create informer for node information
	cc.nsInformer = informerFactory.Core().V1().Namespaces()
	cc.nsInformer.Informer().AddEventHandlerWithResyncPeriod(
		cache.ResourceEventHandlerFuncs{
			AddFunc: cc.addNamespace,
			//UpdateFunc: cc.UpdateNamespace,
			DeleteFunc: cc.deleteNamespace,
		},
		0,
	)

	return cc
}

func (cc *ConsumerController) Run(stopCh chan struct{}) {
	// initialized
	cc.createConsumerCRD()

	go cc.nsInformer.Informer().Run(stopCh)
	cache.WaitForCacheSync(stopCh, cc.nsInformer.Informer().HasSynced)
}

func (cc *ConsumerController) createConsumerCRD() error {
	extensionscs, err := apiextensionsclient.NewForConfig(cc.config)
	if err != nil {
		return err
	}
	_, err = client.CreateConsumerCRD(extensionscs)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}

func (cc *ConsumerController) addNamespace(obj interface{}) {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return
	}

	c := &arbv1.Consumer{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ns.Name,
			Namespace: ns.Name,
		},
	}

	if _, err := cc.arbclient.ArbV1().Consumers(ns.Name).Create(c); err != nil {
		glog.V(3).Infof("Create Consumer <%v/%v> successfully.", c.Name, c.Name)
	}
}

func (cc *ConsumerController) deleteNamespace(obj interface{}) {
	ns, ok := obj.(*corev1.Namespace)
	if !ok {
		return
	}

	err := cc.arbclient.ArbV1().Consumers(ns.Name).Delete(ns.Name, &metav1.DeleteOptions{})
	if err != nil {
		glog.V(3).Infof("Failed to delete Consumer <%s>", ns.Name)
	}
}
