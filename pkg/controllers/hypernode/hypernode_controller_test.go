package hypernode

import (
	"testing"
	"time"

	vcclientset "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	vcinformer "volcano.sh/apis/pkg/client/informers/externalversions"
	"volcano.sh/volcano/pkg/controllers/hypernode/provider"
)

func TestHyperNodeController_Run(t *testing.T) {
	stopCh := make(chan struct{})
	defer close(stopCh)

	fakeClient := vcclientset.NewSimpleClientset()
	informerFactory := vcinformer.NewSharedInformerFactory(fakeClient, 0)

	controller := &hyperNodeController{
		vcClient:          fakeClient,
		vcInformerFactory: informerFactory,
		hyperNodeInformer: informerFactory.Topology().V1alpha1().HyperNodes(),
		hyperNodeLister:   informerFactory.Topology().V1alpha1().HyperNodes().Lister(),
		provider:          provider.NewProvider(fakeClient, informerFactory, ""),
	}

	go controller.Run(stopCh)
	time.Sleep(1 * time.Second)
	for informerType, ok := range informerFactory.WaitForCacheSync(stopCh) {
		if !ok {
			t.Errorf("Failed to sync cache for informer type: %s", informerType)
		}
	}
}
