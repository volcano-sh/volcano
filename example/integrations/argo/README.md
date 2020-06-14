# Use Argo Workflow to integrate Volcano Job

The Argo resource template allows users to create, delete, or update any type of Kubernetes resource (including CRDs). We can use the resource template to integrate Volcano Jobs into Argo Workflow, and use Argo to add job dependency management and DAG process control capabilities to volcano.

```markdown
[argo examples:kubernetes resource](https://github.com/argoproj/argo/blob/master/examples/README.md#kubernetes-resources)
```

## Argo Workflow RBAC

When the Argo Workflow is running, you need to specify serviceAccount for the Argo Workflow. If not specified, the default serviceAccount in the current namespace is used.

```yaml
argo submit --serviceaccount <name>
```

Installing argo will create serviceaccount **argo**, rolebinding **argo-binding** and role **argo-role** by default. If necessary, you can manually create clusterrole and clusterrolebinding.

In order to successfully manage kubernetes resources, it is necessary to add the management authority of the operated resources for the role/clusterrole bound to serviceAccount.

We add the management authority for all resources in volcano api groups(resources and verbs can be subdivided according to the actual situation,verbs **create** and **get** is must):

```yaml
- apiGroups:
  - batch.volcano.sh
  resources:
  - "*"
  verbs:
  - "*"
```

## Create Volcano Job by Argo Workflow

In order to ensure that Argo Workflow can manage the kubernetes resources created by itself, owner references need to be added for new resources:

```yaml
          ownerReferences:
          - apiVersion: argoproj.io/v1alpha1
            blockOwnerDeletion: true
            kind: Workflow
            name: "{{workflow.name}}"
            uid: "{{workflow.uid}}"
```

Example of using Argo Workflow to create Volcano Job:

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
  serviceAccountName: argo        # specify the service account
  templates:
  - name: nginx-tmpl
    activeDeadlineSeconds: 120        # to limit the elapsed time for a workflow, you need set the variable activeDeadlineSeconds
    resource:        # indicates that this is a resource template
      action: create        # can be any kubectl action (e.g. create, delete, apply, patch)
      # The successCondition and failureCondition are optional expressions.
      # If failureCondition is true, the step is considered failed.
      # If successCondition is true, the step is considered successful.
      # They use kubernetes label selection syntax and can be applied against any field
      # of the resource (not just labels). Multiple AND conditions can be represented by comma
      # delimited expressions.
      # For more details: https://kubernetes.io/docs/concepts/overview/working-with-objects/labels/
      # argoexec will get the resource information by "kubectl get -o json -w resouce/name" and check if the conditions are match
      # Completed is the phase that all tasks of Job are completed
      # Failed is the phase that the job is restarted failed reached the maximum number of retries.
      # change the successCondition or failureCondition according to the actual situation
      successCondition: status.state.phase = Completed
      failureCondition: status.state.phase = Failed
      manifest: |						#put your kubernetes spec here
        apiVersion: batch.volcano.sh/v1alpha1
        kind: Job
        metadata:
          generateName: test-job-
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

Argo Workflow will create one Volcano Job.If the job is a long running job(like nginx process), you must set activeDeadlineSeconds for template otherwise Workflow will not enter the next step.

