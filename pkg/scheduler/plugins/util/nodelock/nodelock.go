/*
Copyright 2022 The Volcano Authors.

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

package nodelock

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
)

const MaxLockRetry = 5

var kubeClient kubernetes.Interface

func GetClient() kubernetes.Interface {
	return kubeClient
}

// NewClient connects to an API server
func NewClient() (kubernetes.Interface, error) {
	kubeConfig := os.Getenv("KUBECONFIG")
	if kubeConfig == "" {
		kubeConfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
	}
	config, err := rest.InClusterConfig()
	if err != nil {
		config, err = clientcmd.BuildConfigFromFlags("", kubeConfig)
		if err != nil {
			return nil, err
		}
	}
	client, err := kubernetes.NewForConfig(config)
	kubeClient = client
	return client, err
}

// UseClient uses an existing client
func UseClient(client kubernetes.Interface) error {
	kubeClient = client
	return nil
}

func updateNodeAnnotations(ctx context.Context, node *v1.Node, updateFunc func(annotations map[string]string)) error {
	newNode := node.DeepCopy()
	updateFunc(newNode.ObjectMeta.Annotations)
	nodeName := newNode.Name
	_, err := kubeClient.CoreV1().Nodes().Update(ctx, newNode, metav1.UpdateOptions{})
	for i := 0; i < MaxLockRetry && err != nil; i++ {
		klog.ErrorS(err, "Failed to update node", "node", nodeName, "retry", i)
		time.Sleep(100 * time.Millisecond)
		node, err = kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
		if err != nil {
			klog.ErrorS(err, "Failed to get node when retry to update", "node", nodeName)
			continue
		}
		newNode = node.DeepCopy()
		updateFunc(newNode.ObjectMeta.Annotations)
		_, err = kubeClient.CoreV1().Nodes().Update(ctx, newNode, metav1.UpdateOptions{})
	}
	if err != nil {
		klog.ErrorS(err, "Failed to update node", "node", nodeName)
		return fmt.Errorf("failed to update node %s, exceeded retry count %d", nodeName, MaxLockRetry)
	}
	return nil
}

func setNodeLock(nodeName string, lockName string) error {
	ctx := context.Background()
	node, err := kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if _, ok := node.ObjectMeta.Annotations[lockName]; ok {
		klog.V(3).Infof("node %s is locked", nodeName)
		return fmt.Errorf("node %s is locked", nodeName)
	}
	updateFunc := func(annotations map[string]string) {
		annotations[lockName] = time.Now().Format(time.RFC3339)
	}
	err = updateNodeAnnotations(ctx, node, updateFunc)
	if err != nil {
		return fmt.Errorf("setNodeLock exceeds retry count %d", MaxLockRetry)
	}
	klog.InfoS("Node lock set", "node", nodeName)
	return nil
}

func ReleaseNodeLock(nodeName string, lockName string) error {
	ctx := context.Background()
	node, err := kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if _, ok := node.ObjectMeta.Annotations[lockName]; !ok {
		klog.V(3).InfoS("Node lock not set", "node", nodeName)
		return nil
	}
	updateFunc := func(annotations map[string]string) {
		delete(annotations, lockName)
	}
	err = updateNodeAnnotations(ctx, node, updateFunc)
	if err != nil {
		return fmt.Errorf("releaseNodeLock exceeds retry count %d", MaxLockRetry)
	}
	klog.InfoS("Node lock released", "node", nodeName)
	return nil
}

// LockNode try lock device 'lockName' on node 'nodeName'
func LockNode(nodeName string, lockName string) error {
	ctx := context.Background()
	node, err := kubeClient.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil {
		return err
	}
	if _, ok := node.ObjectMeta.Annotations[lockName]; !ok {
		return setNodeLock(nodeName, lockName)
	}
	lockTime, err := time.Parse(time.RFC3339, node.ObjectMeta.Annotations[lockName])
	if err != nil {
		return err
	}
	if time.Since(lockTime) > time.Minute*5 {
		klog.V(3).InfoS("Node lock expired", "node", nodeName, "lockTime", lockTime)
		err = ReleaseNodeLock(nodeName, lockName)
		if err != nil {
			klog.ErrorS(err, "Failed to release node lock", "node", nodeName)
			return err
		}
		return setNodeLock(nodeName, lockName)
	}
	return fmt.Errorf("node %s has been locked within 5 minutes", nodeName)
}
