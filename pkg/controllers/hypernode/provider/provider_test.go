package provider

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"

	"volcano.sh/apis/pkg/apis/topology/v1alpha1"
	topologyv1alpha1 "volcano.sh/apis/pkg/apis/topology/v1alpha1"
	topologyapplyv1alpha1 "volcano.sh/apis/pkg/client/applyconfiguration/topology/v1alpha1"
	fakeclient "volcano.sh/apis/pkg/client/clientset/versioned/fake"
	"volcano.sh/apis/pkg/client/informers/externalversions"
)

func Test_provider_Provision_Add_And_Delete_Event(t *testing.T) {
	eventCh := make(chan Event)
	client := fakeclient.NewSimpleClientset()
	factory := externalversions.NewSharedInformerFactory(client, 0)
	stopCh := make(chan struct{})
	defer close(stopCh)
	p := &provider{
		eventCh:  eventCh,
		replyCh:  make(chan Reply),
		vcClient: client,
		factory:  factory,
	}
	go p.Provision(stopCh)
	// add event
	add := addEvent()
	delete := deleteEvent()
	eventCh <- add
	err := wait.PollUntilContextTimeout(context.TODO(), 1*time.Second, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := client.TopologyV1alpha1().HyperNodes().Get(ctx, add.HyperNodeName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		return true, nil
	})
	assert.NoError(t, err)

	// update event
	update := updateEvent()
	eventCh <- update
	err = wait.PollUntilContextTimeout(context.TODO(), 1*time.Second, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		hn, err := client.TopologyV1alpha1().HyperNodes().Get(ctx, update.HyperNodeName, metav1.GetOptions{})
		if err != nil {
			return false, nil
		}
		if !reflect.DeepEqual(hn.Spec, expectedHyperNode().Spec) {
			return false, nil
		}
		return true, nil
	})
	assert.NoError(t, err)

	// delete event
	eventCh <- delete
	err = wait.PollUntilContextTimeout(context.TODO(), 1*time.Second, 10*time.Second, true, func(ctx context.Context) (bool, error) {
		_, err := client.TopologyV1alpha1().HyperNodes().Get(ctx, delete.HyperNodeName, metav1.GetOptions{})
		if errors.IsNotFound(err) {

			return true, nil
		}
		return false, nil
	})
	assert.NoError(t, err)
}

func addEvent() Event {
	return Event{
		Type:          "Add",
		HyperNodeName: "hn-1",
		HyperNode: topologyv1alpha1.HyperNode{
			ObjectMeta: metav1.ObjectMeta{
				Name: "hn-1",
			},
			Spec: v1alpha1.HyperNodeSpec{
				Tier: 1,
			},
		},
	}
}

func deleteEvent() Event {
	return Event{
		Type:          "Delete",
		HyperNodeName: "hn-1",
	}
}

func updateEvent() Event {
	patch := *topologyapplyv1alpha1.HyperNode("hn-1").WithSpec(topologyapplyv1alpha1.HyperNodeSpec().WithTier(2))
	return Event{
		Type:          "Update",
		HyperNodeName: "hn-1",
		HyperNode:     expectedHyperNode(),
		Patch:         patch,
	}
}

func expectedHyperNode() v1alpha1.HyperNode {
	return topologyv1alpha1.HyperNode{
		ObjectMeta: metav1.ObjectMeta{
			Name: "hn-1",
		},
		Spec: v1alpha1.HyperNodeSpec{
			Tier: 2,
		},
	}
}
