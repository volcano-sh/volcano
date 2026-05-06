## Volcano

Volcano is a batch system built on Kubernetes. It provides a suite of mechanisms currently missing from
Kubernetes that are commonly required by many classes of batch & elastic workload including:

1. machine learning/deep learning,
2. bioinformatics/genomics, and
3. other "big data" applications.

## Prerequisites

- Kubernetes 1.12+ with CRD support

## Installing volcano via yaml file

All-in-one yaml has been generated for quick deployment. Try command:

```$xslt
kubectl apply -f volcano-v0.0.x.yaml
```

Check the status in namespace `volcano-system`

```$xslt
$kubectl get all -n volcano-system
NAME                                       READY   STATUS      RESTARTS   AGE
pod/volcano-admission-56f5465597-2pbfx     1/1     Running     0          36s
pod/volcano-admission-init-pjgf2           0/1     Completed   0          36s
pod/volcano-controllers-687948d9c8-zdtvw   1/1     Running     0          36s
pod/volcano-scheduler-94998fc64-86hzn      1/1     Running     0          36s


NAME                                TYPE        CLUSTER-IP       EXTERNAL-IP   PORT(S)   AGE
service/volcano-admission-service   ClusterIP   10.103.235.185   <none>        443/TCP   36s


NAME                                  READY   UP-TO-DATE   AVAILABLE   AGE
deployment.apps/volcano-admission     1/1     1            1           36s
deployment.apps/volcano-controllers   1/1     1            1           36s
deployment.apps/volcano-scheduler     1/1     1            1           36s

NAME                                             DESIRED   CURRENT   READY   AGE
replicaset.apps/volcano-admission-56f5465597     1         1         1       36s
replicaset.apps/volcano-controllers-687948d9c8   1         1         1       36s
replicaset.apps/volcano-scheduler-94998fc64      1         1         1       36s
```

## Installing volcano via helm charts

To install the volcano with chart:

```bash
helm install <specified-name> helm/chart/volcano --namespace <namespace> --create-namespace

e.g :
helm install volcano-trial helm/chart/volcano --namespace volcano-trial --create-namespace
```

This command deploys volcano in kubernetes cluster with default configuration.  The [configuration](#configuration) section lists the parameters that can be configured during installation.

To list the volcano chart:

```bash
helm list -n <namespace>

e.g:
 helm list -n volcano-trial
```

## Uninstalling the Chart

```bash
$ helm delete volcano-release --purge
```

## Configuration

The following are the list configurable parameters of Volcano Chart and their default values.

| Parameter | YAML Path | Description | Type | Accepted Values | Example |
|-----------|-----------|-------------|------|-----------------|---------|
| **Image and Core Settings (basic)** | | | | | |
| `basic.image_registry` | `basic.image_registry` | Container image registry for all Volcano images | string | Any registry URL | `docker.io` |
| `basic.image_tag_version` | `basic.image_tag_version` | Default image tag used when a per-component tag is unset | string | Any valid image tag | `latest` |
| `basic.image_pull_policy` | `basic.image_pull_policy` | Image pull policy applied to all component pods | string | `Always`, `IfNotPresent`, `Never` | `Always` |
| `basic.image_pull_secret` | `basic.image_pull_secret` | Name of the imagePullSecret for private registries | string | Any secret name | `my-registry-secret` |
| `basic.controller_image_name` | `basic.controller_image_name` | Controller image name | string | Any image name | `volcanosh/vc-controller-manager` |
| `basic.controller_image_tag_version` | `basic.controller_image_tag_version` | Controller image tag override | string | Any valid image tag | `v1.10.0` |
| `basic.scheduler_image_name` | `basic.scheduler_image_name` | Scheduler image name | string | Any image name | `volcanosh/vc-scheduler` |
| `basic.scheduler_image_tag_version` | `basic.scheduler_image_tag_version` | Scheduler image tag override | string | Any valid image tag | `v1.10.0` |
| `basic.admission_image_name` | `basic.admission_image_name` | Admission controller image name | string | Any image name | `volcanosh/vc-webhook-manager` |
| `basic.admission_image_tag_version` | `basic.admission_image_tag_version` | Admission controller image tag override | string | Any valid image tag | `v1.10.0` |
| `basic.agent_image_name` | `basic.agent_image_name` | Agent image name | string | Any image name | `volcanosh/vc-agent` |
| `basic.agent_image_tag_version` | `basic.agent_image_tag_version` | Agent image tag override | string | Any valid image tag | `v1.10.0` |
| `basic.agent_scheduler_image_name` | `basic.agent_scheduler_image_name` | Agent scheduler image name | string | Any image name | `volcanosh/vc-agent-scheduler` |
| `basic.agent_scheduler_image_tag_version` | `basic.agent_scheduler_image_tag_version` | Agent scheduler image tag override | string | Any valid image tag | `v1.10.0` |
| `basic.admission_secret_name` | `basic.admission_secret_name` | TLS secret name used by the admission webhook | string | Any secret name | `volcano-admission-secret` |
| `basic.admission_config_file` | `basic.admission_config_file` | Path to the admission config file packaged in the chart | string | Valid file path | `config/volcano-admission.conf` |
| `basic.admission_port` | `basic.admission_port` | Webhook server port for the admission controller | int | `1024`-`65535` | `8443` |
| `basic.scheduler_config_file` | `basic.scheduler_config_file` | Path to the scheduler config file packaged in the chart | string | Valid file path | `config/volcano-scheduler.conf` |
| `basic.agent_scheduler_config_file` | `basic.agent_scheduler_config_file` | Path to the agent scheduler config file packaged in the chart | string | Valid file path | `config/agent-scheduler.conf` |
| **Feature Flags (custom)** | | | | | |
| `custom.admission_enable` | `custom.admission_enable` | Deploy the admission controller | bool | `true`, `false` | `true` |
| `custom.controller_enable` | `custom.controller_enable` | Deploy the controller | bool | `true`, `false` | `true` |
| `custom.scheduler_enable` | `custom.scheduler_enable` | Deploy the Volcano scheduler | bool | `true`, `false` | `true` |
| `custom.agent_scheduler_enable` | `custom.agent_scheduler_enable` | Deploy the agent scheduler | bool | `true`, `false` | `false` |
| `custom.colocation_enable` | `custom.colocation_enable` | Deploy the agent DaemonSet for colocation workloads | bool | `true`, `false` | `false` |
| `custom.metrics_enable` | `custom.metrics_enable` | Deploy metrics components such as Prometheus and kube-state-metrics | bool | `true`, `false` | `false` |
| `custom.go_memlimit_enable` | `custom.go_memlimit_enable` | Set `GOMEMLIMIT` from container memory limits for supported components | bool | `true`, `false` | `false` |
| `custom.vap_enable` | `custom.vap_enable` | Enable ValidatingAdmissionPolicy resources | bool | `true`, `false` | `false` |
| `custom.map_enable` | `custom.map_enable` | Enable MutatingAdmissionPolicy resources | bool | `true`, `false` | `false` |
| `custom.leader_elect_enable` | `custom.leader_elect_enable` | Enable leader election for supported components | bool | `true`, `false` | `false` |
| **Replica Counts (custom)** | | | | | |
| `custom.admission_replicas` | `custom.admission_replicas` | Number of admission controller replicas | int | `1`+ | `1` |
| `custom.controller_replicas` | `custom.controller_replicas` | Number of controller replicas | int | `1`+ | `1` |
| `custom.scheduler_replicas` | `custom.scheduler_replicas` | Number of scheduler replicas | int | `1`+ | `1` |
| `custom.agent_scheduler_replicas` | `custom.agent_scheduler_replicas` | Number of agent scheduler replicas | int | `1`+ | `1` |
| **Admission Controller Settings (custom)** | | | | | |
| `custom.admission_feature_gates` | `custom.admission_feature_gates` | Feature gate flags passed to the admission controller | string | Comma-separated `Key=bool` pairs | `FeatureA=true,FeatureB=false` |
| `custom.admission_log_level` | `custom.admission_log_level` | Admission controller verbosity level (`-v`) | int | `0`-`10` | `4` |
| **Scheduler Tuning (custom)** | | | | | |
| `custom.scheduler_name` | `custom.scheduler_name` | Scheduler name registered with Kubernetes | string | Any valid name | `volcano` |
| `custom.scheduler_metrics_enable` | `custom.scheduler_metrics_enable` | Enable the scheduler metrics endpoint | bool | `true`, `false` | `true` |
| `custom.scheduler_pprof_enable` | `custom.scheduler_pprof_enable` | Enable scheduler pprof profiling | bool | `true`, `false` | `false` |
| `custom.scheduler_kube_api_qps` | `custom.scheduler_kube_api_qps` | QPS limit for the scheduler Kubernetes client | int | `1`+ | `2000` |
| `custom.scheduler_kube_api_burst` | `custom.scheduler_kube_api_burst` | Burst limit for the scheduler Kubernetes client | int | `1`+ | `2000` |
| `custom.scheduler_schedule_period` | `custom.scheduler_schedule_period` | How often the scheduler runs a scheduling cycle | string | Go duration string | `1s` |
| `custom.scheduler_node_worker_threads` | `custom.scheduler_node_worker_threads` | Number of goroutines used for node event processing | int | `1`+ | `20` |
| `custom.scheduler_percentage_nodes_to_find` | `custom.scheduler_percentage_nodes_to_find` | Percentage of nodes the scheduler evaluates per cycle; `0` means all nodes | int | `0`-`100` | `10` |
| `custom.scheduler_plugins_dir` | `custom.scheduler_plugins_dir` | Directory used to load custom scheduler plugins | string | Valid directory path | `/custom-plugins` |
| `custom.scheduler_feature_gates` | `custom.scheduler_feature_gates` | Feature gate flags passed to the scheduler | string | Comma-separated `Key=bool` pairs | `FeatureA=true,FeatureB=false` |
| `custom.ignored_provisioners` | `custom.ignored_provisioners` | Storage provisioners ignored by the scheduler | string | Comma-separated provisioner names | `provisioner.example.com` |
| `custom.scheduler_sharding_mode` | `custom.scheduler_sharding_mode` | Scheduler sharding mode | string | Implementation-defined sharding mode string | `default` |
| `custom.scheduler_log_level` | `custom.scheduler_log_level` | Scheduler verbosity level (`-v`) | int | `0`-`10` | `3` |
| **Controller Tuning (custom)** | | | | | |
| `custom.controller_metrics_enable` | `custom.controller_metrics_enable` | Enable the controller metrics endpoint | bool | `true`, `false` | `true` |
| `custom.controller_kube_api_qps` | `custom.controller_kube_api_qps` | QPS limit for the controller Kubernetes client | int | `1`+ | `50` |
| `custom.controller_kube_api_burst` | `custom.controller_kube_api_burst` | Burst limit for the controller Kubernetes client | int | `1`+ | `100` |
| `custom.controller_worker_threads` | `custom.controller_worker_threads` | Number of controller worker goroutines | int | `1`+ | `3` |
| `custom.controller_worker_threads_for_gc` | `custom.controller_worker_threads_for_gc` | Number of controller goroutines used for garbage collection | int | `1`+ | `5` |
| `custom.controller_worker_threads_for_podgroup` | `custom.controller_worker_threads_for_podgroup` | Number of controller goroutines used for PodGroup reconciliation | int | `1`+ | `5` |
| `custom.controller_enabled_controllers` | `custom.controller_enabled_controllers` | Comma-separated list of controllers to enable; prefix with `-` to disable | string | `*`, controller names, optional `-` prefix | `*,-sharding-controller` |
| `custom.controller_log_level` | `custom.controller_log_level` | Controller verbosity level (`-v`) | int | `0`-`10` | `4` |
| **Agent Scheduler Tuning (custom)** | | | | | |
| `custom.agent_scheduler_name` | `custom.agent_scheduler_name` | Agent scheduler instance name | string | Any valid name | `agent-scheduler` |
| `custom.agent_scheduler_worker_count` | `custom.agent_scheduler_worker_count` | Number of scheduling worker goroutines in the agent scheduler | int | `1`+ | `1` |
| `custom.agent_scheduler_sharding_mode` | `custom.agent_scheduler_sharding_mode` | Agent scheduler sharding mode | string | Implementation-defined sharding mode string | `default` |
| `custom.agent_scheduler_sharding_name` | `custom.agent_scheduler_sharding_name` | Agent scheduler sharding name | string | Any valid name | `default` |
| **Agent and Colocation Settings (custom)** | | | | | |
| `custom.agent_supported_features` | `custom.agent_supported_features` | Comma-separated list of agent colocation features to enable | string | `OverSubscription`, `Eviction`, `Resources` | `OverSubscription,Eviction` |
| `custom.agent_extend_resource_cpu_name` | `custom.agent_extend_resource_cpu_name` | Extended resource name used to advertise extra CPU capacity | string | Valid resource name | `example.com/cpu` |
| `custom.agent_extend_resource_memory_name` | `custom.agent_extend_resource_memory_name` | Extended resource name used to advertise extra memory capacity | string | Valid resource name | `example.com/memory` |
| `custom.agent_kube_cgroup_root` | `custom.agent_kube_cgroup_root` | cgroup root path used by kubelet on the node | string | Valid path | `/sys/fs/cgroup` |
| `custom.agent_cni_config_path` | `custom.agent_cni_config_path` | Path to the CNI config file on the host | string | Valid path | `/etc/cni/net.d/cni.conflist` |
| **Config Overrides (custom)** | | | | | |
| `custom.admission_config_override` | `custom.admission_config_override` | Inline YAML that replaces the default admission ConfigMap data | string | Valid admission config YAML | `~` |
| `custom.scheduler_config_override` | `custom.scheduler_config_override` | Inline YAML that replaces the default scheduler ConfigMap data | string | Valid scheduler config YAML | `~` |
| `custom.controller_config_override` | `custom.controller_config_override` | Inline YAML that replaces the default controller ConfigMap data | string | Valid controller config YAML | `~` |
| `custom.enabled_admissions` | `custom.enabled_admissions` | Comma-separated list of admission webhook paths to enable | string | Webhook paths such as `/jobs/mutate` and `/queues/validate` | `/jobs/mutate,/jobs/validate,/podgroups/validate,/queues/mutate,/queues/validate,/hypernodes/validate,/cronjobs/validate` |
| **Sharding ConfigMap (custom)** | | | | | |
| `custom.sharding_configmap_enable` | `custom.sharding_configmap_enable` | Create the sharding ConfigMap watched by the sharding controller | bool | `true`, `false` | `true` |
| `custom.sharding_configmap_data` | `custom.sharding_configmap_data` | Raw YAML content stored in the sharding ConfigMap | string | Valid sharding config YAML | `~` |
| **Affinity (custom)** | | | | | |
| `custom.default_affinity` | `custom.default_affinity` | Default affinity applied to all components unless overridden | object | Kubernetes affinity object | `~` |
| `custom.admission_affinity` | `custom.admission_affinity` | Affinity for admission controller pods | object | Kubernetes affinity object | `~` |
| `custom.controller_affinity` | `custom.controller_affinity` | Affinity for controller pods | object | Kubernetes affinity object | `~` |
| `custom.scheduler_affinity` | `custom.scheduler_affinity` | Affinity for scheduler pods | object | Kubernetes affinity object | `~` |
| `custom.agent_affinity` | `custom.agent_affinity` | Affinity for agent pods | object | Kubernetes affinity object | `~` |
| `custom.agent_scheduler_affinity` | `custom.agent_scheduler_affinity` | Affinity for agent scheduler pods | object | Kubernetes affinity object | `~` |
| **Tolerations (custom)** | | | | | |
| `custom.default_tolerations` | `custom.default_tolerations` | Default tolerations applied to all components unless overridden | list | Kubernetes toleration list | `~` |
| `custom.admission_tolerations` | `custom.admission_tolerations` | Tolerations for admission controller pods | list | Kubernetes toleration list | `~` |
| `custom.controller_tolerations` | `custom.controller_tolerations` | Tolerations for controller pods | list | Kubernetes toleration list | `~` |
| `custom.scheduler_tolerations` | `custom.scheduler_tolerations` | Tolerations for scheduler pods | list | Kubernetes toleration list | `~` |
| `custom.agent_tolerations` | `custom.agent_tolerations` | Tolerations for agent pods | list | Kubernetes toleration list | `[{key: volcano.sh/offline-job-evicting, operator: Exists, effect: NoSchedule}]` |
| `custom.agent_scheduler_tolerations` | `custom.agent_scheduler_tolerations` | Tolerations for agent scheduler pods | list | Kubernetes toleration list | `~` |
| **Pod Security Context (custom)** | | | | | |
| `custom.default_sc` | `custom.default_sc` | Default pod `securityContext` applied to all components unless overridden | object | Kubernetes pod securityContext object | `{seccompProfile: {type: RuntimeDefault}, seLinuxOptions: {level: s0:c123,c456}}` |
| `custom.admission_sc` | `custom.admission_sc` | Pod `securityContext` for admission controller pods | object | Kubernetes pod securityContext object | `~` |
| `custom.controller_sc` | `custom.controller_sc` | Pod `securityContext` for controller pods | object | Kubernetes pod securityContext object | `~` |
| `custom.scheduler_sc` | `custom.scheduler_sc` | Pod `securityContext` for scheduler pods | object | Kubernetes pod securityContext object | `~` |
| `custom.agent_sc` | `custom.agent_sc` | Pod `securityContext` for agent pods | object | Kubernetes pod securityContext object | `~` |
| `custom.agent_scheduler_sc` | `custom.agent_scheduler_sc` | Pod `securityContext` for agent scheduler pods | object | Kubernetes pod securityContext object | `~` |
| **Container Security Context (custom)** | | | | | |
| `custom.default_csc` | `custom.default_csc` | Default container `securityContext` applied to main containers unless overridden | object | Kubernetes container securityContext object | `{runAsNonRoot: true, runAsUser: 1000, capabilities: {add: [DAC_OVERRIDE], drop: [ALL]}, allowPrivilegeEscalation: false}` |
| `custom.admission_main_csc` | `custom.admission_main_csc` | Container `securityContext` for the admission controller main container | object | Kubernetes container securityContext object | `~` |
| `custom.admission_init_csc` | `custom.admission_init_csc` | Container `securityContext` for the admission init container | object | Kubernetes container securityContext object | `~` |
| `custom.controller_main_csc` | `custom.controller_main_csc` | Container `securityContext` for the controller main container | object | Kubernetes container securityContext object | `~` |
| `custom.scheduler_main_csc` | `custom.scheduler_main_csc` | Container `securityContext` for the scheduler main container | object | Kubernetes container securityContext object | `~` |
| `custom.agent_main_csc` | `custom.agent_main_csc` | Container `securityContext` for the agent main container | object | Kubernetes container securityContext object | `{runAsNonRoot: true, runAsUser: 1000, capabilities: {add: [DAC_OVERRIDE, SETUID, SETGID, SETFCAP, BPF], drop: [ALL]}}` |
| `custom.agent_init_csc` | `custom.agent_init_csc` | Container `securityContext` for the agent init container | object | Kubernetes container securityContext object | `{runAsUser: 0, capabilities: {add: [CHOWN, DAC_OVERRIDE, FOWNER], drop: [ALL]}, allowPrivilegeEscalation: false}` |
| `custom.agent_scheduler_main_csc` | `custom.agent_scheduler_main_csc` | Container `securityContext` for the agent scheduler main container | object | Kubernetes container securityContext object | `~` |
| **Node Selector (custom)** | | | | | |
| `custom.default_ns` | `custom.default_ns` | Default nodeSelector applied to all components unless overridden | object | Valid label key-value pairs | `~` |
| `custom.admission_ns` | `custom.admission_ns` | nodeSelector for admission controller pods | object | Valid label key-value pairs | `~` |
| `custom.controller_ns` | `custom.controller_ns` | nodeSelector for controller pods | object | Valid label key-value pairs | `~` |
| `custom.scheduler_ns` | `custom.scheduler_ns` | nodeSelector for scheduler pods | object | Valid label key-value pairs | `~` |
| `custom.agent_ns` | `custom.agent_ns` | nodeSelector for agent pods | object | Valid label key-value pairs | `~` |
| `custom.agent_scheduler_ns` | `custom.agent_scheduler_ns` | nodeSelector for agent scheduler pods | object | Valid label key-value pairs | `~` |
| `custom.kube_state_metrics_ns` | `custom.kube_state_metrics_ns` | nodeSelector for kube-state-metrics pods | object | Valid label key-value pairs | `~` |
| **Labels (custom)** | | | | | |
| `custom.common_labels` | `custom.common_labels` | Labels added to every chart object except CRDs | object | Valid label key-value pairs | `~` |
| `custom.aggregationRule_labels` | `custom.aggregationRule_labels` | Labels used by ClusterRole aggregation rules | object | Valid label key-value pairs | `~` |
| `custom.admission_labels` | `custom.admission_labels` | Labels for the admission Deployment and Job | object | Valid label key-value pairs | `~` |
| `custom.admission_podLabels` | `custom.admission_podLabels` | Labels for admission pods | object | Valid label key-value pairs | `~` |
| `custom.controller_labels` | `custom.controller_labels` | Labels for the controller Deployment | object | Valid label key-value pairs | `~` |
| `custom.controller_podLabels` | `custom.controller_podLabels` | Labels for controller pods | object | Valid label key-value pairs | `~` |
| `custom.scheduler_labels` | `custom.scheduler_labels` | Labels for the scheduler Deployment | object | Valid label key-value pairs | `~` |
| `custom.scheduler_podLabels` | `custom.scheduler_podLabels` | Labels for scheduler pods | object | Valid label key-value pairs | `~` |
| **Resource Requests and Limits (custom)** | | | | | |
| `custom.admission_resources` | `custom.admission_resources` | CPU and memory requests and limits for admission pods | object | Kubernetes resource object | `~` |
| `custom.controller_resources` | `custom.controller_resources` | CPU and memory requests and limits for controller pods | object | Kubernetes resource object | `~` |
| `custom.scheduler_resources` | `custom.scheduler_resources` | CPU and memory requests and limits for scheduler and agent scheduler pods | object | Kubernetes resource object | `~` |
| `custom.agent_resources` | `custom.agent_resources` | CPU and memory requests and limits for agent pods | object | Kubernetes resource object | `~` |
| **Log Levels (custom)** | | | | | |
| `custom.controller_log_level` | `custom.controller_log_level` | Controller verbosity level (`-v`) | int | `0`-`10` | `4` |
| `custom.scheduler_log_level` | `custom.scheduler_log_level` | Scheduler and agent scheduler verbosity level (`-v`) | int | `0`-`10` | `3` |
| **Admission Webhooks (custom)** | | | | | |
| `custom.webhooks_namespace_selector_expressions` | `custom.webhooks_namespace_selector_expressions` | Additional namespace selector expressions used by Volcano admission webhooks | list | Kubernetes label selector expressions | `[{key: workload-type, operator: In, values: [batch]}]` |
| **Service Settings (service)** | | | | | |
| `service.ipFamilyPolicy` | `service.ipFamilyPolicy` | IP family policy for Volcano services | string | `SingleStack`, `PreferDualStack`, `RequireDualStack` | `PreferDualStack` |
| `service.ipFamilies` | `service.ipFamilies` | Ordered list of IP families assigned to Volcano services | list | `IPv4`, `IPv6` | `[IPv4, IPv6]` |

Specify each parameter using the `--set key=value[,key=value]` argument to `helm install`. For example,

```bash
$ helm install --name volcano-release --set basic.image_pull_policy=Always volcano/volcano
```

The above command set image pull policy to `Always`, so docker image will be pulled each time.

Alternatively, a YAML file that specifies the values for the parameters can be provided while installing the chart. For example,

```bash
$ helm install --name volcano-release -f values.yaml volcano/volcano
```

> **Tip**: You can use the default [values.yaml](helm/chart/volcano/values.yaml)
