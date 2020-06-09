# 使用argo workflow集成volcano job

argo 资源模板允许用户创建、删除或更新任何kubernetes资源类型（包括CRD），我们可以使用资源模板来将volcano job集成到argo workflow中，利用argo来为volcano增加job依赖管理和DAG流程控制能力。

```markdown
[argo使用kubernetes resource](https://github.com/argoproj/argo/blob/master/examples/README.md#kubernetes-resources)
```

## argo serviceAccount操作volcano apiGroup权限

workflow运行时需要为workflow指定serviceAccount，若无指定则使用当前命名空间下的default serviceAccount。

```yaml
argo submit --serviceaccount <name>
```

安装argo会默认创建serviceaccount **argo**，rolebinding **argo-binding**和role **argo-role**。若有需要可以手动创建clusterrole和clusterrolebinding。

为了成功管理kubernetes资源，需要为serviceAccount所绑定的role/clusterrole增加被操作资源的管理权限。

在这里我们增加对volcano apiGroup中所有资源的管理权限(可根据实际情况细分resources和verbs)：

```yaml
- apiGroups:
  - batch.volcano.sh
  resources:
  - "*"
  verbs:
  - "*"
```

## 使用argo workflow创建volcano job 

为了保证argo workflow能够管理所创建的kubernetes，需要为新资源增加ownerReferences：

```yaml
          ownerReferences:
          - apiVersion: argoproj.io/v1alpha1
            blockOwnerDeletion: true
            kind: Workflow
            name: "{{workflow.name}}"
            uid: "{{workflow.uid}}"
```

使用argo workflow创建volcano job：

```yaml
# in a workflow. The resource template type accepts any k8s manifest
# (including CRDs) and can perform any kubectl action against it (e.g. create,
# apply, delete, patch).
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: volcano-job
spec:
  entrypoint: nginx-tmpl
  serviceAccountName: argo              # specify the service account
  templates:
  - name: nginx-tmpl
    resource:				 			# indicates that this is a resource template
      action: create					# can be any kubectl action (e.g. create, delete, apply, patch)
      # The successCondition and failureCondition are optional expressions.
      # If failureCondition is true, the step is considered failed.
      # If successCondition is true, the step is considered successful.
      # They use kubernetes label selection syntax and can be applied against any field
      # of the resource (not just labels). Multiple AND conditions can be represented by comma
      # delimited expressions.
      # For more details: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
      successCondition: status.succeeded > 0
      failureCondition: status.failed > 3
      manifest: |						#put your kubernetes spec here
        apiVersion: batch.volcano.sh/v1alpha1
        kind: Job
        metadata:
          generateName: test-job
          ownerReferences:
          - apiVersion: argoproj.io/v1alpha1
            blockOwnerDeletion: true
            kind: Workflow
            name: "{{workflow.name}}"
            uid: "{{workflow.uid}}"
        spec:
          minAvailable: 1
          schedulerName: volcano
          policies:
          - event: PodEvicted
            action: RestartJob
          plugins:
            ssh: []
            env: []
            svc: []
          maxRetry: 5
          queue: default
          tasks:
          - replicas: 2
            name: "default-nginx"
            template:
              metadata:
                name: web
              spec:
                containers:
                - image: nginx:latest
                  imagePullPolicy: IfNotPresent
                  name: nginx
                  resources:
                    requests:
                      cpu: "100m"
                restartPolicy: OnFailure
```

