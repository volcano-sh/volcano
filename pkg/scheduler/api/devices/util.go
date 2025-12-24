/*
Copyright 2023 The Volcano Authors.

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

/*
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

package devices

import (
	"context"
	"encoding/json"
	"fmt"

	v1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8stypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog/v2"
)

// These are predefined codes used in a Status.
const (
	// Success means that plugin ran correctly and found pod schedulable.
	// NOTE: A nil status is also considered as "Success".
	Success int = iota
	// Error is used for internal plugin errors, unexpected input, etc.
	Error
	// Unschedulable is used when a plugin finds a pod unschedulable. The scheduler might attempt to
	// preempt other pods to get this pod scheduled. Use UnschedulableAndUnresolvable to make the
	// scheduler skip preemption.
	// The accompanying status message should explain why the pod is unschedulable.
	Unschedulable
	// UnschedulableAndUnresolvable is used when a plugin finds a pod unschedulable and
	// preemption would not change anything. Plugins should return Unschedulable if it is possible
	// that the pod can get scheduled with preemption.
	// The accompanying status message should explain why the pod is unschedulable.
	UnschedulableAndUnresolvable
	// Wait is used when a Permit plugin finds a pod scheduling should wait.
	Wait
	// Skip is used when a Bind plugin chooses to skip binding.
	Skip
)

var kubeClient *kubernetes.Clientset

func GetClient() kubernetes.Interface {
	var err error
	if kubeClient == nil {
		kubeClient, err = NewClient()
		if err != nil {
			klog.ErrorS(err, "deviceshare initClient failed")
		}
	}
	return kubeClient
}

// NewClient connects to an API server
func NewClient() (*kubernetes.Clientset, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		return nil, err
	}
	client, err := kubernetes.NewForConfig(config)
	return client, err
}

func GetNode(nodename string) (*v1.Node, error) {
	if nodename == "" {
		klog.ErrorS(nil, "Node name is empty")
		return nil, fmt.Errorf("nodename is empty")
	}

	klog.V(5).InfoS("Fetching node", "nodeName", nodename)
	n, err := GetClient().CoreV1().Nodes().Get(context.Background(), nodename, metav1.GetOptions{})
	if err != nil {
		switch {
		case apierrors.IsNotFound(err):
			klog.ErrorS(err, "Node not found", "nodeName", nodename)
			return nil, fmt.Errorf("node %s not found", nodename)
		case apierrors.IsUnauthorized(err):
			klog.ErrorS(err, "Unauthorized to access node", "nodeName", nodename)
			return nil, fmt.Errorf("unauthorized to access node %s", nodename)
		default:
			klog.ErrorS(err, "Failed to get node", "nodeName", nodename)
			return nil, fmt.Errorf("failed to get node %s: %v", nodename, err)
		}
	}

	klog.V(5).InfoS("Successfully fetched node", "nodeName", nodename)
	return n, nil
}

func PatchPodAnnotations(kubeClient kubernetes.Interface, pod *v1.Pod, annotations map[string]string) error {
	type patchMetadata struct {
		Annotations map[string]string `json:"annotations,omitempty"`
	}
	type patchPod struct {
		Metadata patchMetadata `json:"metadata"`
		//Spec     patchSpec     `json:"spec,omitempty"`
	}

	p := patchPod{}
	p.Metadata.Annotations = annotations

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = kubeClient.CoreV1().Pods(pod.Namespace).
		Patch(context.Background(), pod.Name, k8stypes.StrategicMergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.Errorf("patch pod %v failed, %v", pod.Name, err)
	}

	return err
}

func PatchNodeAnnotations(node *v1.Node, annotations map[string]string) error {
	type patchMetadata struct {
		Annotations map[string]string `json:"annotations,omitempty"`
	}
	type patchPod struct {
		Metadata patchMetadata `json:"metadata"`
		//Spec     patchSpec     `json:"spec,omitempty"`
	}

	p := patchPod{}
	p.Metadata.Annotations = annotations

	bytes, err := json.Marshal(p)
	if err != nil {
		return err
	}
	_, err = GetClient().CoreV1().Nodes().
		Patch(context.Background(), node.Name, k8stypes.StrategicMergePatchType, bytes, metav1.PatchOptions{})
	if err != nil {
		klog.Infoln("annotations=", annotations)
		klog.Infof("patch node %v failed, %v", node.Name, err)
	}
	return err
}

func ExtractResourceRequest(pod *v1.Pod, resourceType, countName, memoryName, percentageName, coreName string) []ContainerDeviceRequest {
	resourceName := v1.ResourceName(countName)
	resourceMem := v1.ResourceName(memoryName)
	counts := []ContainerDeviceRequest{}

	//Count Nvidia GPU
	for i := 0; i < len(pod.Spec.Containers); i++ {
		singledevice := false
		v, ok := pod.Spec.Containers[i].Resources.Limits[resourceName]
		if !ok {
			v, ok = pod.Spec.Containers[i].Resources.Limits[resourceMem]
			singledevice = true
		}
		if ok {
			n := int64(1)
			if !singledevice {
				n, _ = v.AsInt64()
			}
			memnum := int32(0)
			mem, ok := pod.Spec.Containers[i].Resources.Limits[resourceMem]
			if !ok {
				mem, ok = pod.Spec.Containers[i].Resources.Requests[resourceMem]
			}
			if ok {
				memnums, ok := mem.AsInt64()
				if ok {
					memnum = int32(memnums)
				}
			}
			mempnum := int32(101)
			if percentageName != "" {
				resourceMemPercentage := v1.ResourceName(percentageName)
				mem, ok = pod.Spec.Containers[i].Resources.Limits[resourceMemPercentage]
				if !ok {
					mem, ok = pod.Spec.Containers[i].Resources.Requests[resourceMemPercentage]
				}
				if ok {
					mempnums, ok := mem.AsInt64()
					if ok {
						mempnum = int32(mempnums)
					}
				}
			}
			if mempnum == 101 && memnum == 0 {
				mempnum = 100
			}
			corenum := int32(0)
			if coreName != "" {
				resourceCores := v1.ResourceName(coreName)
				core, ok := pod.Spec.Containers[i].Resources.Limits[resourceCores]
				if !ok {
					core, ok = pod.Spec.Containers[i].Resources.Requests[resourceCores]
				}
				if ok {
					corenums, ok := core.AsInt64()
					if ok {
						corenum = int32(corenums)
					}
				}
			}
			counts = append(counts, ContainerDeviceRequest{
				Nums:             int32(n),
				Type:             resourceType,
				Memreq:           memnum,
				MemPercentagereq: int32(mempnum),
				Coresreq:         corenum,
			})
		}
	}
	klog.V(3).Infoln("counts=", counts)
	return counts
}
