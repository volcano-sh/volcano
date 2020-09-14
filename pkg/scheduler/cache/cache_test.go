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

package cache

import (
	"fmt"
	"reflect"
	"testing"

	v1 "k8s.io/api/core/v1"
	"k8s.io/api/scheduling/v1beta1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/yaml"

	schedulingscheme "volcano.sh/volcano/pkg/apis/scheduling/scheme"
	schedulingv1beta1 "volcano.sh/volcano/pkg/apis/scheduling/v1beta1"
	"volcano.sh/volcano/pkg/scheduler/api"
	schedulingapi "volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/util"
)

func buildNode(name string, alloc v1.ResourceList) *v1.Node {
	return &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			UID:  types.UID(name),
			Name: name,
		},
		Status: v1.NodeStatus{
			Capacity:    alloc,
			Allocatable: alloc,
		},
	}
}

func buildPod(ns, n, nn string,
	p v1.PodPhase, req v1.ResourceList,
	owner []metav1.OwnerReference, labels map[string]string) *v1.Pod {

	return &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			UID:             types.UID(fmt.Sprintf("%v-%v", ns, n)),
			Name:            n,
			Namespace:       ns,
			OwnerReferences: owner,
			Labels:          labels,
		},
		Status: v1.PodStatus{
			Phase: p,
		},
		Spec: v1.PodSpec{
			NodeName: nn,
			Containers: []v1.Container{
				{
					Resources: v1.ResourceRequirements{
						Requests: req,
					},
				},
			},
		},
	}
}

func buildResourceList(cpu string, memory string) v1.ResourceList {
	return v1.ResourceList{
		v1.ResourceCPU:    resource.MustParse(cpu),
		v1.ResourceMemory: resource.MustParse(memory),
	}
}

func buildOwnerReference(owner string) metav1.OwnerReference {
	controller := true
	return metav1.OwnerReference{
		Controller: &controller,
		UID:        types.UID(owner),
	}
}

func TestGetOrCreateJob(t *testing.T) {
	owner1 := buildOwnerReference("j1")
	owner2 := buildOwnerReference("j2")

	pod1 := buildPod("c1", "p1", "n1", v1.PodRunning, buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner1}, make(map[string]string))
	pi1 := api.NewTaskInfo(pod1)
	pi1.Job = "j1" // The job name is set by cache.

	pod2 := buildPod("c1", "p2", "n1", v1.PodRunning, buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner2}, make(map[string]string))
	pod2.Spec.SchedulerName = "volcano"
	pi2 := api.NewTaskInfo(pod2)

	pod3 := buildPod("c3", "p3", "n1", v1.PodRunning, buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner2}, make(map[string]string))
	pi3 := api.NewTaskInfo(pod3)

	cache := &SchedulerCache{
		Nodes:         make(map[string]*api.NodeInfo),
		Jobs:          make(map[api.JobID]*api.JobInfo),
		schedulerName: "volcano",
	}

	tests := []struct {
		task   *api.TaskInfo
		gotJob bool // whether getOrCreateJob will return job for corresponding task
	}{
		{
			task:   pi1,
			gotJob: true,
		},
		{
			task:   pi2,
			gotJob: false,
		},
		{
			task:   pi3,
			gotJob: false,
		},
	}
	for i, test := range tests {
		result := cache.getOrCreateJob(test.task) != nil
		if result != test.gotJob {
			t.Errorf("case %d: \n expected %t, \n got %t \n",
				i, test.gotJob, result)
		}
	}
}

func TestSchedulerCache_Bind_NodeWithSufficientResources(t *testing.T) {
	owner := buildOwnerReference("j1")

	cache := &SchedulerCache{
		Jobs:  make(map[api.JobID]*api.JobInfo),
		Nodes: make(map[string]*api.NodeInfo),
		Binder: &util.FakeBinder{
			Binds:   map[string]string{},
			Channel: make(chan string),
		},
	}

	pod := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("1000m", "1G"),
		[]metav1.OwnerReference{owner}, make(map[string]string))
	cache.AddPod(pod)

	node := buildNode("n1", buildResourceList("2000m", "10G"))
	cache.AddNode(node)

	task := api.NewTaskInfo(pod)
	task.Job = "j1"
	if err := cache.addTask(task); err != nil {
		t.Errorf("failed to add task %v", err)
	}

	err := cache.Bind(task, "n1")
	if err != nil {
		t.Errorf("failed to bind pod to node: %v", err)
	}
}

func TestSchedulerCache_Bind_NodeWithInsufficientResources(t *testing.T) {
	owner := buildOwnerReference("j1")

	cache := &SchedulerCache{
		Jobs:  make(map[api.JobID]*api.JobInfo),
		Nodes: make(map[string]*api.NodeInfo),
		Binder: &util.FakeBinder{
			Binds:   map[string]string{},
			Channel: make(chan string),
		},
	}

	pod := buildPod("c1", "p1", "", v1.PodPending, buildResourceList("5000m", "50G"),
		[]metav1.OwnerReference{owner}, make(map[string]string))
	cache.AddPod(pod)

	node := buildNode("n1", buildResourceList("2000m", "10G"))
	cache.AddNode(node)

	task := api.NewTaskInfo(pod)
	task.Job = "j1"

	if err := cache.addTask(task); err != nil {
		t.Errorf("failed to add task %v", err)
	}

	taskBeforeBind := task.Clone()
	nodeBeforeBind := cache.Nodes["n1"].Clone()

	err := cache.Bind(task, "n1")
	if err == nil {
		t.Errorf("expected bind to fail for node with insufficient resources")
	}

	_, taskAfterBind, err := cache.findJobAndTask(task)
	if err != nil {
		t.Errorf("expected to find task after failed bind")
	}
	if !reflect.DeepEqual(taskBeforeBind, taskAfterBind) {
		t.Errorf("expected task to remain the same after failed bind: \n %#v\n %#v", taskBeforeBind, taskAfterBind)
	}

	nodeAfterBind := cache.Nodes["n1"]
	if !reflect.DeepEqual(nodeBeforeBind, nodeAfterBind) {
		t.Errorf("expected node to remain the same after failed bind")
	}
}

func BenchmarkSnapShot(b *testing.B) {
	nodedata := []byte(`
apiVersion: v1
kind: Node
metadata:
  annotations:
    kubeadm.alpha.kubernetes.io/cri-socket: /run/containerd/containerd.sock
    node.alpha.kubernetes.io/ttl: "0"
    volumes.kubernetes.io/controller-managed-attach-detach: "true"
  creationTimestamp: "2020-09-10T11:55:48Z"
  labels:
    beta.kubernetes.io/arch: amd64
    beta.kubernetes.io/os: linux
    kubernetes.io/arch: amd64
    kubernetes.io/hostname: integration-worker
    kubernetes.io/os: linux
  managedFields:
  - apiVersion: v1
    fieldsType: FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          f:kubeadm.alpha.kubernetes.io/cri-socket: {}
    manager: kubeadm
    operation: Update
    time: "2020-09-10T11:55:49Z"
  - apiVersion: v1
    fieldsType: FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          f:node.alpha.kubernetes.io/ttl: {}
      f:spec:
        f:podCIDR: {}
        f:podCIDRs:
          .: {}
          v:"10.244.1.0/24": {}
    manager: kube-controller-manager
    operation: Update
    time: "2020-09-10T11:56:49Z"
  - apiVersion: v1
    fieldsType: FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          .: {}
          f:volumes.kubernetes.io/controller-managed-attach-detach: {}
        f:labels:
          .: {}
          f:beta.kubernetes.io/arch: {}
          f:beta.kubernetes.io/os: {}
          f:kubernetes.io/arch: {}
          f:kubernetes.io/hostname: {}
          f:kubernetes.io/os: {}
      f:status:
        f:addresses:
          .: {}
          k:{"type":"Hostname"}:
            .: {}
            f:address: {}
            f:type: {}
          k:{"type":"InternalIP"}:
            .: {}
            f:address: {}
            f:type: {}
        f:allocatable:
          .: {}
          f:cpu: {}
          f:ephemeral-storage: {}
          f:hugepages-1Gi: {}
          f:hugepages-2Mi: {}
          f:memory: {}
          f:pods: {}
        f:capacity:
          .: {}
          f:cpu: {}
          f:ephemeral-storage: {}
          f:hugepages-1Gi: {}
          f:hugepages-2Mi: {}
          f:memory: {}
          f:pods: {}
        f:conditions:
          .: {}
          k:{"type":"DiskPressure"}:
            .: {}
            f:lastHeartbeatTime: {}
            f:lastTransitionTime: {}
            f:message: {}
            f:reason: {}
            f:status: {}
            f:type: {}
          k:{"type":"MemoryPressure"}:
            .: {}
            f:lastHeartbeatTime: {}
            f:lastTransitionTime: {}
            f:message: {}
            f:reason: {}
            f:status: {}
            f:type: {}
          k:{"type":"PIDPressure"}:
            .: {}
            f:lastHeartbeatTime: {}
            f:lastTransitionTime: {}
            f:message: {}
            f:reason: {}
            f:status: {}
            f:type: {}
          k:{"type":"Ready"}:
            .: {}
            f:lastHeartbeatTime: {}
            f:lastTransitionTime: {}
            f:message: {}
            f:reason: {}
            f:status: {}
            f:type: {}
        f:daemonEndpoints:
          f:kubeletEndpoint:
            f:Port: {}
        f:images: {}
        f:nodeInfo:
          f:architecture: {}
          f:bootID: {}
          f:containerRuntimeVersion: {}
          f:kernelVersion: {}
          f:kubeProxyVersion: {}
          f:kubeletVersion: {}
          f:machineID: {}
          f:operatingSystem: {}
          f:osImage: {}
          f:systemUUID: {}
    manager: kubelet
    operation: Update
    time: "2020-09-14T03:14:27Z"
  name: integration-worker
  resourceVersion: "983917"
  selfLink: /api/v1/nodes/integration-worker
  uid: d16e7d2f-fb83-4dfd-b413-1a70ef7ae639
spec:
  podCIDR: 10.244.1.0/24
  podCIDRs:
  - 10.244.1.0/24
status:
  addresses:
  - address: 172.20.0.6
    type: InternalIP
  - address: integration-worker
    type: Hostname
  allocatable:
    cpu: "128"
    ephemeral-storage: 123722704Ki
    hugepages-1Gi: "0"
    hugepages-2Mi: "0"
    memory: 817433200Ki
    pods: "110"
  capacity:
    cpu: "128"
    ephemeral-storage: 123722704Ki
    hugepages-1Gi: "0"
    hugepages-2Mi: "0"
    memory: 817433200Ki
    pods: "110"
  conditions:
  - lastHeartbeatTime: "2020-09-14T03:14:27Z"
    lastTransitionTime: "2020-09-10T11:55:48Z"
    message: kubelet has sufficient memory available
    reason: KubeletHasSufficientMemory
    status: "False"
    type: MemoryPressure
  - lastHeartbeatTime: "2020-09-14T03:14:27Z"
    lastTransitionTime: "2020-09-10T11:55:48Z"
    message: kubelet has no disk pressure
    reason: KubeletHasNoDiskPressure
    status: "False"
    type: DiskPressure
  - lastHeartbeatTime: "2020-09-14T03:14:27Z"
    lastTransitionTime: "2020-09-10T11:55:48Z"
    message: kubelet has sufficient PID available
    reason: KubeletHasSufficientPID
    status: "False"
    type: PIDPressure
  - lastHeartbeatTime: "2020-09-14T03:14:27Z"
    lastTransitionTime: "2020-09-10T11:56:49Z"
    message: kubelet is posting ready status
    reason: KubeletReady
    status: "True"
    type: Ready
  daemonEndpoints:
    kubeletEndpoint:
      Port: 10250
  images:
  - names:
    - k8s.gcr.io/etcd:3.4.3-0
    sizeBytes: 289997247
  - names:
    - k8s.gcr.io/kube-apiserver:v1.18.2
    sizeBytes: 146648881
  - names:
    - k8s.gcr.io/kube-proxy:v1.18.2
    sizeBytes: 132860030
  - names:
    - k8s.gcr.io/kube-controller-manager:v1.18.2
    sizeBytes: 132826435
  - names:
    - docker.io/kindest/kindnetd:0.5.4
    sizeBytes: 113207016
  - names:
    - k8s.gcr.io/kube-scheduler:v1.18.2
    sizeBytes: 113095985
  - names:
    - docker.io/volcanosh/vc-webhook-manager:69d8b90465ed1fc45c59b66619e3561f908618cb
    sizeBytes: 89717006
  - names:
    - docker.io/volcanosh/vc-scheduler:f2398f1139b83725526a8c6fd6bb170f18ac9847
    sizeBytes: 57335712
  - names:
    - docker.io/volcanosh/vc-scheduler:4e2898689cf09d5c4a55bd29c3630361a25209e0
    sizeBytes: 57125792
  - names:
    - docker.io/volcanosh/vc-scheduler:297a29c7b779dc33d361f79fe65666d0659d2fed
    sizeBytes: 57125792
  - names:
    - docker.io/volcanosh/vc-scheduler:e4774923ee4d10cdbf85860ffaf69e7744af181b
    sizeBytes: 57125792
  - names:
    - docker.io/volcanosh/vc-scheduler:e1605804e5a4fa5e23f6cdc21e5634dc8f3f371e
    sizeBytes: 57121695
  - names:
    - docker.io/volcanosh/vc-scheduler:ace5c87816ed949fb48c3eba3683ea9c5c6fa2f6
    sizeBytes: 57121184
  - names:
    - docker.io/volcanosh/vc-scheduler:7c77a17b89656175c11607b7cd4643729b4f10b3
    sizeBytes: 57121182
  - names:
    - docker.io/volcanosh/vc-scheduler:160b9349966d03f517b03db42ac746ac779e81bb
    sizeBytes: 57117088
  - names:
    - docker.io/volcanosh/vc-scheduler:e475e8ec341a10da3c9db835e3f3ec2d261f210b
    sizeBytes: 57117088
  - names:
    - docker.io/volcanosh/vc-scheduler:d1074ec2c254d6afcd2341b7f68550497b9718b2
    sizeBytes: 57117088
  - names:
    - docker.io/volcanosh/vc-scheduler:69d8b90465ed1fc45c59b66619e3561f908618cb
    sizeBytes: 57116576
  - names:
    - docker.io/volcanosh/vc-scheduler:a0e0e41a03fecdd7fba3f137283cc08b6767683a
    sizeBytes: 57112992
  - names:
    - k8s.gcr.io/debian-base:v2.0.0
    sizeBytes: 53884301
  - names:
    - docker.io/library/nginx@sha256:9a1f8ed9e2273e8b3bbcd2e200024adac624c2e5c9b1d420988809f5c0c41a5e
    - docker.io/library/nginx:latest
    sizeBytes: 53506367
  - names:
    - docker.io/volcanosh/vc-controller-manager:69d8b90465ed1fc45c59b66619e3561f908618cb
    sizeBytes: 49427400
  - names:
    - docker.io/library/nginx@sha256:f7988fb6c02e0ce69257d9bd9cf37ae20a60f1df7563c3a2a6abe24160306b8d
    - docker.io/library/nginx:1.14
    sizeBytes: 44710204
  - names:
    - k8s.gcr.io/coredns:1.6.7
    sizeBytes: 43921887
  - names:
    - docker.io/rancher/local-path-provisioner:v0.0.12
    sizeBytes: 41994847
  - names:
    - k8s.gcr.io/pause:3.2
    sizeBytes: 685724
  - names:
    - docker.io/library/busybox:1.24
    sizeBytes: 677121
  nodeInfo:
    architecture: amd64
    bootID: 513e86e1-60bd-42c0-beab-f89b084fc8af
    containerRuntimeVersion: containerd://1.3.3-14-g449e9269
    kernelVersion: 4.4.0-142-generic
    kubeProxyVersion: v1.18.2
    kubeletVersion: v1.18.2
    machineID: 36f0321a41514267a970d2035510f7c3
    operatingSystem: linux
    osImage: Ubuntu 19.10
    systemUUID: db2953ff-6a9f-4002-94d9-f246b9436b6e
`)
	poddata := []byte(`
apiVersion: v1
kind: Pod
metadata:
  annotations:
    scheduling.k8s.io/group-name: test-job
    volcano.sh/job-name: test-job
    volcano.sh/job-version: "0"
    volcano.sh/task-spec: default-nginx
  creationTimestamp: "2020-09-14T02:44:53Z"
  labels:
    volcano.sh/job-name: test-job
    volcano.sh/job-namespace: default
  managedFields:
  - apiVersion: v1
    fieldsType: FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          .: {}
          f:scheduling.k8s.io/group-name: {}
          f:volcano.sh/job-name: {}
          f:volcano.sh/job-version: {}
          f:volcano.sh/task-spec: {}
        f:labels:
          .: {}
          f:volcano.sh/job-name: {}
          f:volcano.sh/job-namespace: {}
        f:ownerReferences:
          .: {}
          k:{"uid":"24c9fa2a-d17a-45a2-a541-93b8473e4390"}:
            .: {}
            f:apiVersion: {}
            f:blockOwnerDeletion: {}
            f:controller: {}
            f:kind: {}
            f:name: {}
            f:uid: {}
      f:spec:
        f:containers:
          k:{"name":"nginx"}:
            .: {}
            f:env:
              .: {}
              k:{"name":"VC_DEFAULT-NGINX_HOSTS"}:
                .: {}
                f:name: {}
                f:valueFrom:
                  .: {}
                  f:configMapKeyRef:
                    .: {}
                    f:key: {}
                    f:name: {}
              k:{"name":"VC_DEFAULT-NGINX_NUM"}:
                .: {}
                f:name: {}
                f:valueFrom:
                  .: {}
                  f:configMapKeyRef:
                    .: {}
                    f:key: {}
                    f:name: {}
              k:{"name":"VC_TASK_INDEX"}:
                .: {}
                f:name: {}
                f:value: {}
              k:{"name":"VK_TASK_INDEX"}:
                .: {}
                f:name: {}
                f:value: {}
            f:image: {}
            f:imagePullPolicy: {}
            f:name: {}
            f:resources:
              .: {}
              f:requests:
                .: {}
                f:cpu: {}
            f:terminationMessagePath: {}
            f:terminationMessagePolicy: {}
            f:volumeMounts:
              .: {}
              k:{"mountPath":"/etc/volcano"}:
                .: {}
                f:mountPath: {}
                f:name: {}
              k:{"mountPath":"/root/.ssh"}:
                .: {}
                f:mountPath: {}
                f:name: {}
                f:subPath: {}
        f:dnsPolicy: {}
        f:enableServiceLinks: {}
        f:hostname: {}
        f:restartPolicy: {}
        f:schedulerName: {}
        f:securityContext: {}
        f:subdomain: {}
        f:terminationGracePeriodSeconds: {}
        f:volumes:
          .: {}
          k:{"name":"test-job-ssh"}:
            .: {}
            f:name: {}
            f:secret:
              .: {}
              f:defaultMode: {}
              f:items: {}
              f:secretName: {}
          k:{"name":"test-job-svc"}:
            .: {}
            f:configMap:
              .: {}
              f:defaultMode: {}
              f:name: {}
            f:name: {}
    manager: vc-controller-manager
    operation: Update
    time: "2020-09-14T02:44:53Z"
  - apiVersion: v1
    fieldsType: FieldsV1
    fieldsV1:
      f:status:
        f:conditions:
          k:{"type":"ContainersReady"}:
            .: {}
            f:lastProbeTime: {}
            f:lastTransitionTime: {}
            f:status: {}
            f:type: {}
          k:{"type":"Initialized"}:
            .: {}
            f:lastProbeTime: {}
            f:lastTransitionTime: {}
            f:status: {}
            f:type: {}
          k:{"type":"Ready"}:
            .: {}
            f:lastProbeTime: {}
            f:lastTransitionTime: {}
            f:status: {}
            f:type: {}
        f:containerStatuses: {}
        f:hostIP: {}
        f:phase: {}
        f:podIP: {}
        f:podIPs:
          .: {}
          k:{"ip":"10.244.1.51"}:
            .: {}
            f:ip: {}
        f:startTime: {}
    manager: kubelet
    operation: Update
    time: "2020-09-14T02:45:00Z"
  name: test-job-default-nginx-0
  namespace: default
  ownerReferences:
  - apiVersion: batch.volcano.sh/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: Job
    name: test-job
    uid: 24c9fa2a-d17a-45a2-a541-93b8473e4390
  resourceVersion: "978388"
  selfLink: /api/v1/namespaces/default/pods/test-job-default-nginx-0
  uid: b5aa373f-0b71-4d1c-bd2f-e445568386ba
spec:
  containers:
  - env:
    - name: VK_TASK_INDEX
      value: "0"
    - name: VC_TASK_INDEX
      value: "0"
    - name: VC_DEFAULT-NGINX_HOSTS
      valueFrom:
        configMapKeyRef:
          key: VC_DEFAULT-NGINX_HOSTS
          name: test-job-svc
    - name: VC_DEFAULT-NGINX_NUM
      valueFrom:
        configMapKeyRef:
          key: VC_DEFAULT-NGINX_NUM
          name: test-job-svc
    image: nginx
    imagePullPolicy: IfNotPresent
    name: nginx
    resources:
      requests:
        cpu: "1"
    terminationMessagePath: /dev/termination-log
    terminationMessagePolicy: File
    volumeMounts:
    - mountPath: /root/.ssh
      name: test-job-ssh
      subPath: .ssh
    - mountPath: /etc/volcano
      name: test-job-svc
    - mountPath: /var/run/secrets/kubernetes.io/serviceaccount
      name: default-token-2v8sv
      readOnly: true
  dnsPolicy: ClusterFirst
  enableServiceLinks: true
  hostname: test-job-default-nginx-0
  nodeName: integration-worker
  priority: 0
  restartPolicy: OnFailure
  schedulerName: volcano
  securityContext: {}
  serviceAccount: default
  serviceAccountName: default
  subdomain: test-job
  terminationGracePeriodSeconds: 30
  tolerations:
  - effect: NoExecute
    key: node.kubernetes.io/not-ready
    operator: Exists
    tolerationSeconds: 300
  - effect: NoExecute
    key: node.kubernetes.io/unreachable
    operator: Exists
    tolerationSeconds: 300
  volumes:
  - name: test-job-ssh
    secret:
      defaultMode: 384
      items:
      - key: id_rsa
        path: .ssh/id_rsa
      - key: id_rsa.pub
        path: .ssh/id_rsa.pub
      - key: authorized_keys
        path: .ssh/authorized_keys
      - key: config
        path: .ssh/config
      secretName: test-job-ssh
  - configMap:
      defaultMode: 420
      name: test-job-svc
    name: test-job-svc
  - name: default-token-2v8sv
    secret:
      defaultMode: 420
      secretName: default-token-2v8sv
status:
  conditions:
  - lastProbeTime: null
    lastTransitionTime: "2020-09-14T02:44:58Z"
    status: "True"
    type: Initialized
  - lastProbeTime: null
    lastTransitionTime: "2020-09-14T02:45:00Z"
    status: "True"
    type: Ready
  - lastProbeTime: null
    lastTransitionTime: "2020-09-14T02:45:00Z"
    status: "True"
    type: ContainersReady
  - lastProbeTime: null
    lastTransitionTime: "2020-09-14T02:44:58Z"
    status: "True"
    type: PodScheduled
  containerStatuses:
  - containerID: containerd://4a6d855155c0d7a363d6c8c63444d1e597a641e608c37981500f72bb497878d8
    image: docker.io/library/nginx:latest
    imageID: docker.io/library/nginx@sha256:9a1f8ed9e2273e8b3bbcd2e200024adac624c2e5c9b1d420988809f5c0c41a5e
    lastState: {}
    name: nginx
    ready: true
    restartCount: 0
    started: true
    state:
      running:
        startedAt: "2020-09-14T02:44:59Z"
  hostIP: 172.20.0.6
  phase: Running
  podIP: 10.244.1.51
  podIPs:
  - ip: 10.244.1.51
  qosClass: Burstable
  startTime: "2020-09-14T02:44:58Z"
`)

	podgroupdata := []byte(`
apiVersion: scheduling.volcano.sh/v1beta1
kind: PodGroup
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      {"apiVersion":"batch.volcano.sh/v1alpha1","kind":"Job","metadata":{"annotations":{},"name":"test-job","namespace":"default"},"spec":{"maxRetry":5,"minAvailable":3,"plugins":{"env":[],"ssh":[],"svc":[]},"policies":[{"action":"RestartJob","event":"PodEvicted"}],"queue":"default","schedulerName":"volcano","tasks":[{"name":"default-nginx","replicas":6,"template":{"metadata":{"name":"web"},"spec":{"containers":[{"image":"nginx","imagePullPolicy":"IfNotPresent","name":"nginx","resources":{"requests":{"cpu":"1"}}}],"restartPolicy":"OnFailure"}}}]}}
  creationTimestamp: "2020-09-14T02:44:47Z"
  generation: 5
  managedFields:
  - apiVersion: scheduling.volcano.sh/v1beta1
    fieldsType: FieldsV1
    fieldsV1:
      f:metadata:
        f:annotations:
          .: {}
          f:kubectl.kubernetes.io/last-applied-configuration: {}
        f:ownerReferences:
          .: {}
          k:{"uid":"24c9fa2a-d17a-45a2-a541-93b8473e4390"}:
            .: {}
            f:apiVersion: {}
            f:blockOwnerDeletion: {}
            f:controller: {}
            f:kind: {}
            f:name: {}
            f:uid: {}
      f:spec:
        .: {}
        f:minMember: {}
        f:minResources:
          .: {}
          f:cpu: {}
        f:queue: {}
      f:status: {}
    manager: vc-controller-manager
    operation: Update
    time: "2020-09-14T02:44:47Z"
  - apiVersion: scheduling.volcano.sh/v1beta1
    fieldsType: FieldsV1
    fieldsV1:
      f:status:
        f:conditions: {}
        f:phase: {}
        f:running: {}
    manager: vc-scheduler
    operation: Update
    time: "2020-09-14T02:45:03Z"
  name: test-job
  namespace: default
  ownerReferences:
  - apiVersion: batch.volcano.sh/v1alpha1
    blockOwnerDeletion: true
    controller: true
    kind: Job
    name: test-job
    uid: 24c9fa2a-d17a-45a2-a541-93b8473e4390
  resourceVersion: "978401"
  selfLink: /apis/scheduling.volcano.sh/v1beta1/namespaces/default/podgroups/test-job
  uid: 7ee46d28-51c4-4529-b7e8-1c26dee3861f
spec:
  minMember: 1
  minResources:
    cpu: "1"
  queue: default
status:
  conditions:
  - lastTransitionTime: "2020-09-14T02:44:53Z"
    message: '1/0 tasks in gang unschedulable: pod group is not ready, 3 minAvailable.'
    reason: NotEnoughResources
    status: "True"
    transitionID: 0229e228-b74c-4c0a-aa1e-b7ccc00c5b71
    type: Unschedulable
  - lastTransitionTime: "2020-09-14T02:45:03Z"
    reason: tasks in gang are ready to be scheduled
    status: "True"
    transitionID: cb105919-f121-4472-bbb4-811ad4aef245
    type: Scheduled
  phase: Running
  running: 1
`)

	var node v1.Node
	var pod v1.Pod
	var pg schedulingv1beta1.PodGroup

	if err := yaml.Unmarshal(nodedata, &node); err != nil {
		b.Fatalf("unmarshal node failed :%v", err)
	}
	if err := yaml.Unmarshal(poddata, &pod); err != nil {
		b.Fatalf("unmarshal pod failed :%v", err)
	}
	if err := yaml.Unmarshal(podgroupdata, &pg); err != nil {
		b.Fatalf("unmarshal podgroup failed :%v", err)
	}

	sc := &SchedulerCache{
		Jobs:                make(map[schedulingapi.JobID]*schedulingapi.JobInfo),
		Nodes:               make(map[string]*schedulingapi.NodeInfo),
		Queues:              make(map[schedulingapi.QueueID]*schedulingapi.QueueInfo),
		PriorityClasses:     make(map[string]*v1beta1.PriorityClass),
		NamespaceCollection: make(map[string]*schedulingapi.NamespaceCollection),
	}

	podgroup := schedulingapi.PodGroup{}
	if err := schedulingscheme.Scheme.Convert(&pg, &podgroup.PodGroup, nil); err != nil {
		b.Fatalf("Failed to convert podgroup from %T to %T: %v", pg, podgroup, err)
	}

	sc.Queues["default"] = &schedulingapi.QueueInfo{
		UID:  "default",
		Name: "default",

		Weight: 1,
	}

	// construct 1000 node
	for i := 0; i < 1000; i++ {
		node.Name = fmt.Sprintf("test-%d", i)
		sc.Nodes[node.Name] = schedulingapi.NewNodeInfo(&node)
	}

	// construct 10000 pods and podgroups
	for i := 0; i < 10000; i++ {
		pod.Name = fmt.Sprintf("test-%d", i)
		pod.Spec.NodeName = fmt.Sprintf("test-%d", i/10)
		pg.Name = fmt.Sprintf("test-%d", i)

		jobID := schedulingapi.JobID(fmt.Sprintf("%s/%s", pg.Namespace, pg.Name))
		sc.Jobs[jobID] = schedulingapi.NewJobInfo(jobID)

		sc.Jobs[jobID].SetPodGroup(&podgroup)

		task := schedulingapi.NewTaskInfo(&pod)
		err := sc.addTask(task)
		if err != nil {
			b.Fatalf("Add task failed: %v", err)
		}
	}

	for n := 0; n < b.N; n++ {
		sc.Snapshot()
	}
}
