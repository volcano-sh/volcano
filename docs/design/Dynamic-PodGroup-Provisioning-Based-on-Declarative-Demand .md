### Optimized Design: Dynamic PodGroup Provisioning Based on Declarative Demand  

#### **Background**  
In current Volcano implementations, the `pg_controller` automatically instantiates **PodGroup** CRDs for all Pods derived from native Kubernetes workloads (Deployments, StatefulSets, Jobs, DaemonSets) to enable unified gang scheduling.  
**Core Issue**: Not all workloads require gang scheduling semantics. Mandatory PodGroup creation imposes redundant load on the API Server and degrades cluster-wide scheduling performance.  

---

#### **Design Goals**  
Introduce **declarative opt-in gang scheduling** via Kubernetes annotations, adhering to cloud-native principles of:  
1. **On-demand resource provisioning**  
2. **Control-plane efficiency**  
3. **User-driven intent expression**  

---

### Design Specification  

#### **1. Non-Gang Pods (Default Behavior)**  
- **Trigger Condition**: Pods **lacking both** annotations:  
  - `scheduling.k8s.io/group-name`  
  - `scheduling.volcano.sh/group-min-member`  
- **System Action**:  
  - Volcano Scheduler dynamically generates an **ephemeral ShadowPodGroup** exclusively within its cache  
  - Zero interaction with API Server/etcd  
- **Cloud-Native Benefits**:  
  - Eliminates CRD operation overhead  
  - Reduces etcd write amplification  
  - Enables scheduler-local resource optimization  

#### **2. Gang-Enabled Pods**  
- **User Declaration**: Add Annotation `scheduling.volcano.sh/group-min-member: <min-pods>` to `.spec.template.metadata.annotations` of the parent workload (Deployment/Job/etc.).
- **Controller Workflow**:  
  1. `pg_controller` detects the Annotation and creates a formal **PodGroup** CRD.
  2. Populates `scheduling.k8s.io/group-name` on child Pods.
- **Cloud-Native Benefits**:  
  - Explicit intent via Kubernetes-native metadata  
  - Controller-driven lifecycle management  
  - Compliance with operator pattern principles  

---

### Technical Implementation  

#### **ShadowPodGroup Mechanism**  
- **Nature**: Ephemeral in-memory object  
- **Scope**: Confined to Volcano Scheduler cache  



```go
func createShadowPodGroup(pod *v1.Pod) *schedulingapi.PodGroup {
	pgName := helpers.GeneratePodgroupName(pod)
	res := util.GetPodQuotaUsage(pod)
	podgroup := scheduling.PodGroup{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:   pod.Namespace,
			Name:        pgName,
			Annotations: map[string]string{},
			Labels:      map[string]string{},
		},
		Spec: scheduling.PodGroupSpec{
			MinMember:         1,
			PriorityClassName: pod.Spec.PriorityClassName,
			MinResources:      &res,
		},
		Status: scheduling.PodGroupStatus{
			Phase: scheduling.PodGroupPending,
		},
	}

	for k, v := range pod.Annotations {
		podgroup.Annotations[k] = v
	}
	for k, v := range pod.Labels {
		podgroup.Labels[k] = v
	}

	// Individual annotations on pods would overwrite annotations inherited from upper resources.
	if queueName, ok := pod.Annotations[schedulingv1beta1.QueueNameAnnotationKey]; ok {
		podgroup.Spec.Queue = queueName
	}

	pg := &schedulingapi.PodGroup{PodGroup: podgroup, Version: schedulingapi.PodGroupVersionV1Beta1}
	return pg
}
```


