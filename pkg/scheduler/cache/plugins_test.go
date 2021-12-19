/*
Copyright 2021 The Volcano Authors.
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

package cache

import (
	"encoding/json"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/runtime/serializer/streaming"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	restclientwatch "k8s.io/client-go/rest/watch"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog"
)

var eventChan = make(chan *metav1.WatchEvent, 10)
var nodeCache = make(map[string]*v1.Node)
var lock sync.Mutex

func TestEventHandler(t *testing.T) {
	RegisterEventHandler("fake", func(cache *SchedulerCache, config io.Reader, restConfig *rest.Config) (Interface, error) {
		return newFakeEventHandler(cache), nil
	})

	if len(eventHandlers) != 2 {
		t.Errorf("Unexpected eventHandler number %d, expected %d.", len(eventHandlers), 1)
		return
	}

	file, err := os.Create("test-config-file")
	if err != nil {
		t.Errorf("Create file failed for %v.", err)
		return
	}

	defer func() {
		if err := file.Close(); err != nil {
			t.Errorf("Close file failed for %v.", err)
		}
		if err := os.Remove("test-config-file"); err != nil {
			t.Errorf("Remove test file failed for %v.", err)
		}
	}()

	eh, err := GetEventHandler("fake", "test-config-file", nil, nil)
	if err != nil {
		t.Errorf("Unexpected error: %v.", err)
		return
	}

	if eh == nil {
		t.Errorf("Unexpected nil event handler.")
		return
	}

	if _, ok := eh.(*fakeEventHandler); !ok {
		t.Errorf("Unexpected event handler response.")
		return
	}

	nullEH, err := GetEventHandler("not-exist", "", nil, nil)
	if err != nil {
		t.Errorf("Unexpected error: %v.", err)
		return
	}

	if nullEH != nil {
		t.Errorf("Unexpected non-nil event handler.")
		return
	}
}

func TestCustomInformer(t *testing.T) {
	eventChan = make(chan *metav1.WatchEvent, 10)
	nodeCache = make(map[string]*v1.Node)

	testInformer := newTestInformer()
	testInformer.AddEventHandler(cache.ResourceEventHandlerFuncs{
		AddFunc:    addTestNode,
		UpdateFunc: updateTestNode,
		DeleteFunc: deleteTestNode,
	})

	sc := &SchedulerCache{}

	RegisterEventHandler("fake-custom", func(cache *SchedulerCache, config io.Reader, restConfig *rest.Config) (Interface, error) {
		cache.CustomInformers = append(cache.CustomInformers, testInformer)
		return newFakeEventHandler(cache), nil
	})

	_, err := GetEventHandler("fake-custom", "", sc, nil)
	if err != nil {
		t.Errorf("Get fake custom event handler failed for %v.", err)
		return
	}

	stopCh := wait.NeverStop
	for _, ci := range sc.CustomInformers {
		go ci.Run(stopCh)
	}
	sc.WaitForCustomCacheSync(stopCh)

	nodeLength := getLength()
	if nodeLength != 3 {
		t.Errorf("Unexpected custom node cache number %d, expected %d.", nodeLength, 3)
		return
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		response := &metav1.WatchEvent{
			Type: "ADDED",
			Object: runtime.RawExtension{
				Object: &v1.Node{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Node",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-node-4",
						Namespace:       "testing",
						UID:             "test-uid-4",
						ResourceVersion: "0",
					},
				},
			},
		}
		eventChan <- response
		response = &metav1.WatchEvent{
			Type: "MODIFIED",
			Object: runtime.RawExtension{
				Object: &v1.Node{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Node",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-node-1",
						Namespace:       "testing",
						UID:             "test-uid-1",
						ResourceVersion: "1",
					},
					Spec: v1.NodeSpec{
						PodCIDR: "test-cidr",
					},
				},
			},
		}
		eventChan <- response
		response = &metav1.WatchEvent{
			Type: "DELETED",
			Object: runtime.RawExtension{
				Object: &v1.Node{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Node",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-node-2",
						Namespace:       "testing",
						UID:             "test-uid-2",
						ResourceVersion: "1",
					},
				},
			},
		}
		eventChan <- response
		response = &metav1.WatchEvent{
			Type: "DELETED",
			Object: runtime.RawExtension{
				Object: &v1.Node{
					TypeMeta: metav1.TypeMeta{
						Kind:       "Node",
						APIVersion: "v1",
					},
					ObjectMeta: metav1.ObjectMeta{
						Name:            "test-node-3",
						Namespace:       "testing",
						UID:             "test-uid-3",
						ResourceVersion: "1",
					},
				},
			},
		}
		eventChan <- response
	}()
	wg.Done()

	time.Sleep(1 * time.Second)

	nodeLength = getLength()
	if nodeLength != 2 {
		t.Errorf("Unexpected custom node cache number %d, expected %d.", nodeLength, 2)
		return
	}

	if _, ok := getNode("test-node-2"); ok {
		t.Errorf("Node was not deleted from the cache.")
		return
	}

	if _, ok := getNode("test-node-3"); ok {
		t.Errorf("Node was not deleted from the cache.")
		return
	}

	if _, ok := getNode("test-node-4"); !ok {
		t.Errorf("Node was not added to the cache.")
		return
	}

	if node, ok := getNode("test-node-1"); !ok || node.Spec.PodCIDR != "test-cidr" {
		t.Errorf("Node cache was not refreshed.")
		return
	}
}

func newTestInformer() cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return listNodes(), nil
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return watchNodes(), nil
			},
		},
		&v1.Node{},
		1*time.Minute,
		cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
}

func listNodes() runtime.Object {
	return &v1.NodeList{
		Items: []v1.Node{
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Node",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-node-1",
					Namespace:       "testing",
					UID:             "test-uid-1",
					ResourceVersion: "0",
				},
			},
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Node",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-node-2",
					Namespace:       "testing",
					UID:             "test-uid-2",
					ResourceVersion: "0",
				},
			},
			{
				TypeMeta: metav1.TypeMeta{
					Kind:       "Node",
					APIVersion: "v1",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:            "test-node-3",
					Namespace:       "testing",
					UID:             "test-uid-3",
					ResourceVersion: "0",
				},
			},
		},
	}
}

func watchNodes() watch.Interface {
	contentType := "application/json"
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil {
		klog.Errorf("ParseMediaType failed for %v.", err)
		return nil
	}

	objectDecoder, streamingSerializer, framer, err := runtime.NewClientNegotiator(scheme.Codecs.WithoutConversion(),
		schema.GroupVersion{
			Group:   "",
			Version: "v1",
		}).StreamDecoder(mediaType, params)
	if err != nil {
		klog.Errorf("Decode failed for %v.", err)
		return nil
	}

	if len(eventChan) == 0 {
		time.Sleep(1 * time.Second)
	}

	framerReader := framer.NewFrameReader(ioutil.NopCloser(&testReader{}))
	watchEventDecoder := streaming.NewDecoder(framerReader, streamingSerializer)
	return watch.NewStreamWatcher(
		restclientwatch.NewDecoder(watchEventDecoder, objectDecoder),
		// use 500 to indicate that the cause of the error is unknown - other error codes
		// are more specific to HTTP interactions, and set a reason
		apierrors.NewClientErrorReporter(http.StatusInternalServerError, "GET", "ClientWatchDecoding"),
	)
}

type testReader struct {
}

func (r *testReader) Read(b []byte) (n int, err error) {
	if len(eventChan) > 0 {
		response := <-eventChan
		body, err := json.Marshal(response)
		if err != nil {
			klog.Errorf("Marshal failed for %v.", err)
			return 0, err
		}

		n = copy(b, body[0:])
		return n, nil
	}

	return 0, io.EOF
}

func addTestNode(obj interface{}) {
	node, ok := obj.(*v1.Node)
	if !ok {
		klog.Errorf("Cannot convert to *v1.Node: %v.", obj)
		return
	}

	if _, ok := getNode(node.Name); ok {
		klog.Errorf("Node %q already in the cache.", node.Name)
		return
	}

	lock.Lock()
	defer lock.Unlock()
	nodeCache[node.Name] = node
}

func updateTestNode(oldObj, newObj interface{}) {
	oldNode, ok := oldObj.(*v1.Node)
	if !ok {
		klog.Errorf("Cannot convert oldObj to *v1.Node: %v.", oldObj)
		return
	}
	newNode, ok := newObj.(*v1.Node)
	if !ok {
		klog.Errorf("Cannot convert newObj to *v1.Node: %v.", newObj)
		return
	}

	lock.Lock()
	defer lock.Unlock()
	delete(nodeCache, oldNode.Name)
	nodeCache[newNode.Name] = newNode
}

func deleteTestNode(obj interface{}) {
	var node *v1.Node
	switch t := obj.(type) {
	case *v1.Node:
		node = t
	case cache.DeletedFinalStateUnknown:
		var ok bool
		node, ok = t.Obj.(*v1.Node)
		if !ok {
			klog.Errorf("Cannot convert to *v1.Node: %v", t.Obj)
			return
		}
	default:
		klog.Errorf("Cannot convert to *v1.Node: %v", t)
		return
	}

	lock.Lock()
	defer lock.Unlock()
	delete(nodeCache, node.Name)
}

func getLength() int {
	lock.Lock()
	defer lock.Unlock()

	return len(nodeCache)
}

func getNode(name string) (*v1.Node, bool) {
	lock.Lock()
	defer lock.Unlock()

	node, ok := nodeCache[name]
	return node, ok
}
