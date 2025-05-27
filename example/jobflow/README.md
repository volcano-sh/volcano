## JobFlow

#### These examples shows how to run JobFlow via Volcano.

[JobFlow](../../docs/design/jobflow) is a workflow engine based on volcano Job. It proposes two concepts to automate running multiple batch jobs, named JobTemplate and JobFlow, so end users can easily declare their jobs and run them using complex control primitives such as sequential or parallel execution, if-then -else statement, switch-case statement, loop execution, etc.

read design at [here](../../docs/design/jobflow).

### Prerequisites

- Kubernetes: >`1.17`

## startup steps

### run jobflow example
```bash
# deploy jobTemplate first
kubectl apply -f JobTemplate.yaml
# deploy jobFlow second
kubectl apply -f JobFlow.yaml

# check them
kubectl get jt
kubectl get jf
kubectl get po
```