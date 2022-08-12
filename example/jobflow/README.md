## JobFlow

#### These examples shows how to run JobFlow via Volcano.

[JobFlow](../../docs/design/jobflow) is a workflow engine based on volcano Job. It proposes two concepts to automate running multiple batch jobs, named JobTemplate and JobFlow, so end users can easily declare their jobs and run them using complex control primitives such as sequential or parallel execution, if-then -else statement, switch-case statement, loop execution, etc.

read design at [here](../../docs/design/jobflow).

### Prerequisites

- docker: `18.06`
- Kubernetes: >`1.17`

## startup steps

build image from local
```bash
# get volcano and jobflow source code from github
git clone http://github.com/volcano-sh/volcano.git
git clone https://github.com/BoCloud/JobFlow.git

# build image beyondcent/jobflow:v0.0.1 from local
cd JobFlow
make
make docker-build
```

##### deploy JobFlow from [here](https://github.com/BoCloud/JobFlow#deploy)
```bash
kubectl apply -f https://raw.githubusercontent.com/BoCloud/JobFlow/main/deploy/jobflow.yaml
```

##### deploy Volcano from [here](https://volcano.sh/en/docs/installation/#install-with-yaml-files)
```bash
kubectl apply -f https://raw.githubusercontent.com/volcano-sh/volcano/master/installer/volcano-development.yaml
```

if cert of `jobflow-webhook-service.kube-system.svc` has expired, generate one to replace it.
```bash
# delete expired cert in secrets
kubectl delete secret jobflow-webhook-server-cert -nkube-system

# use gen-admission-secret.sh register new secret
cd volcano
./installer/dockerfile/webhook-manager/gen-admission-secret.sh --service jobflow-webhook-service --namespace kube-system --secret jobflow-webhook-server-cert

# restart jobflow-controller-manager
kubectl delete pod/jobflow-controller-manager-67847d59dd-j8dmc -nkube-system
```

##### run jobflow example
```bash
# deploy jobTemplate first
cd volcano
kubectl apply -f example/jobflow/JobTemplate.yaml
# deploy jobFlow second
kubectl apply -f example/jobflow/JobFlow.yaml

# check them
kubectl get jt
kubectl get jf
kubectl get po
```