/*
Copyright(C)2020-2022. Huawei Technologies Co.,Ltd. All rights reserved.
*/

/*
Package vnpu is using for HuaWei Ascend pin vnpu allocation.
*/
package vnpu

import (
	"context"
	"fmt"
	"strings"

	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"

	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/common/util"
	"volcano.sh/volcano/pkg/scheduler/plugins/ascend-volcano-plugin/plugin"
)

func getNamespaceEvents(ssn *framework.Session, namespace string) (*v1.EventList, error) {
	events, err := ssn.KubeClient().CoreV1().Events(namespace).List(context.TODO(), metav1.ListOptions{})
	if err != nil {
		klog.V(util.LogDebugLev).Infof("getNamespaceEvents get error:%s.", err)
		return nil, err
	}
	if len(events.Items) < 1 {
		klog.V(util.LogDebugLev).Infof("getNamespaceEvents get no error in:%s.", namespace)
		return nil, fmt.Errorf("%s no events", namespace)
	}
	return events, nil
}

// GetSegmentFailureTaskIDs get segmentation failed pod from pod event
func GetSegmentFailureTaskIDs(ssn *framework.Session, namespace string) []api.TaskID {
	if ssn == nil {
		klog.V(util.LogDebugLev).Infof("GetSegmentFailureTaskIDs %s.", util.ArgumentError)
		return nil
	}

	events, err := getNamespaceEvents(ssn, namespace)
	if err != nil {
		klog.V(util.LogDebugLev).Infof("GetSegmentFailureTaskIDs get error :%s.", err)
		return nil
	}
	var faultTIDs []api.TaskID
	for _, event := range events.Items {
		if !isEventSegmentFailurePod(event) {
			continue
		}

		faultPod := getPodFromKubernetes(ssn, event.InvolvedObject.Name, namespace)
		if faultPod == nil || faultPod.UID != event.InvolvedObject.UID {
			continue
		}
		faultTIDs = append(faultTIDs, api.TaskID(faultPod.UID))
	}
	return faultTIDs
}

func isEventSegmentFailurePod(event v1.Event) bool {
	if event.InvolvedObject.Kind != podObjectType {
		return false
	}

	if event.Type != PodEventTypeAllocateFailed || event.Reason != PodEventReasonAllocateFailed ||
		!(strings.Contains(event.Message, PodEventMsgNoResourceFailed) ||
			strings.Contains(event.Message, PodEventMsgDyCutFailed)) {
		return false
	}
	return true
}

func getPodFromKubernetes(ssn *framework.Session, name, namespace string) *v1.Pod {
	faultPod, err := ssn.KubeClient().CoreV1().Pods(namespace).Get(context.TODO(), name, metav1.GetOptions{})
	if err != nil {
		return nil
	}
	klog.V(util.LogInfoLev).Infof("in getPodEvent pod %s segmentation fault event", name)
	return faultPod
}

// GetVNPUTaskDVPP dvpp default is null
func GetVNPUTaskDVPP(asTask util.NPUTask) string {
	value, ok := asTask.Label[plugin.AscendVNPUDVPP]
	if !ok {
		value = plugin.AscendDVPPEnabledNull
	}
	return value
}

// GetVNPUTaskCpuLevel cpu default is null
func GetVNPUTaskCpuLevel(asTask util.NPUTask) string {
	value, ok := asTask.Label[plugin.AscendVNPULevel]
	if !ok {
		value = plugin.AscendVNPULevelLow
	}
	return value
}
