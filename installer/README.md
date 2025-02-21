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

| Parameter|Description|Default Value|
|----------------|-----------------|----------------------|
|`basic.image_tag_version`| Docker image version Tag | `latest`|
|`basic.controller_image_name`|Controller Docker Image Name|`volcanosh/vc-controller-manager`|
|`basic.scheduler_image_name`|Scheduler Docker Image Name|`volcanosh/vc-scheduler`|
|`basic.admission_image_name`|Admission Controller Image Name|`volcanosh/vc-webhook-manager`|
|`basic.admission_secret_name`|Volcano Admission Secret Name|`volcano-admission-secret`|
|`basic.scheduler_config_file`|Configuration File name for Scheduler|`config/volcano-scheduler.conf`|
|`basic.image_pull_secret`|Image Pull Secret|`""`|
|`basic.image_pull_policy`|Image Pull Policy|`Always`|
|`basic.admission_app_name`|Admission Controller App Name|`volcano-admission`|
|`basic.controller_app_name`|Controller App Name|`volcano-controller`|
|`basic.scheduler_app_name`|Scheduler App Name|`volcano-scheduler`|
|`custom.metrics_enable`|Whether to Enable Metrics|`false`|
|`custom.admission_enable`|Whether to Enable Admission|`true`|
|`custom.admission_replicas`|The number of Admission pods to run|`1`|
|`custom.controller_enable`|Whether to Enable Controller|`true`|
|`custom.controller_replicas`|The number of Controller pods to run|`1`|
|`custom.scheduler_enable`|Whether to Enable Scheduler|`true`|
|`custom.scheduler_replicas`|The number of Scheduler pods to run|`1`|
|`custom.leader_elect_enable`|Whether to Enable leader elect|`false`|
|`custom.admission_config_override`|Override admission configmap|`~`|
|`custom.scheduler_config_override`|Override scheduler configmap|`~`|
|`custom.default_affinity`|Default affinity for Admission/Controller/Scheduler pods|`~`|
|`custom.admission_affinity`|Affinity for Admission pods|`~`|
|`custom.controller_affinity`|Affinity for Controller pods|`~`|
|`custom.scheduler_affinity`|Affinity for Scheduler pods|`~`|
|`custom.default_tolerations`|Default tolerations for Admission/Controller/Scheduler pods|`~`|
|`custom.admission_tolerations`|Tolerations for Admission pods|`~`|
|`custom.controller_tolerations`|Tolerations for Controller pods|`~`|
|`custom.scheduler_tolerations`|Tolerations for Scheduler pods|`~`|
|`custom.default_sc`|Default securityContext for Admission/Controller/Scheduler pods|`~`|
|`custom.admission_sc`|securityContext for Admission pods|`~`|
|`custom.controller_sc`|securityContext for Controller pods|`~`|
|`custom.scheduler_sc`|securityContext for Scheduler pods|`~`|
|`custom.default_ns`|Default nodeSelector for Admission/Controller/Scheduler pods|`~`|
|`custom.admission_ns`|nodeSelector for Admission pods|`~`|
|`custom.controller_ns`|nodeSelector for Controller pods|`~`|
|`custom.scheduler_ns`|nodeSelector for Scheduler pods|`~`|
|`custom.kube_state_metrics_ns`|nodeSelector for Kube State Metrics pods|`~`|
|`custom.admission_podLabels`|Pod labels for Admission pods|`~`|
|`custom.controller_podLabels`|Pod labels for Controller pods|`~`|
|`custom.scheduler_podLabels`|Pod labels for Scheduler pods|`~`|
|`custom.admission_labels`|Labels for Admission deployment and job|`~`|
|`custom.controller_labels`|Labels for Controller deployment|`~`|
|`custom.scheduler_labels`|Labels for Scheduler deployment|`~`|
|`custom.common_labels`|Labels for all chart objects except for CRDs |`~`|
|`custom.admission_resources`|Resources for Admission pods|`~`|
|`custom.admission_log_level`|Settings log print level for Admission|`4`|
|`custom.controller_resources`|Resources for Controller pods|`~`|
|`custom.controller_log_level`|Settings log print level for Controller|`4`|
|`custom.scheduler_resources`|Resources for Scheduler pods|`~`|
|`custom.scheduler_log_level`|Settings log print level for Scheduler|`3`|
|`custom.scheduler_plugins_dir`| Settings dir for the Scheduler to load custom plugins|``|
|`custom.webhooks_namespace_selector_expressions`|Additional namespace selector expressions for Volcano admission webhooks|`~`|
|`service.ipFamilyPolicy`|Settings service the family policy|``|
|`service.ipFamilies`|Settings service the address families|`[]`|

Specify each parameter using the `--set key=value[,key=value]` argument to `helm install`. For example,

```bash
$ helm install --name volcano-release --set basic.image_pull_policy=Always volcano/volcano
```

The above command set image pull policy to `Always`, so docker image will be pulled each time.

Alternatively, a YAML file that specifies the values for the parameters can be provided while installing the chart. For example,

```bash
$ helm install --name volcano-release -f values.yaml volcano/volcano
```

> **Tip**: You can use the default [values.yaml](chart/volcano/values.yaml)
