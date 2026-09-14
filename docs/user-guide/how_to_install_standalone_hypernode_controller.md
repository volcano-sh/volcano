# Install the Standalone HyperNode Controller

The Volcano Helm chart can install the HyperNode controller without the Volcano controller manager, scheduler, or admission webhook. This deployment mode is useful when a cluster needs HyperNode topology discovery and reconciliation but uses another scheduler or does not need the other Volcano control-plane components.

The standalone controller uses the same HyperNode API and discovery configuration as the controller embedded in `vc-controller-manager`. The Helm chart also installs the required HyperNode CRDs, RBAC, controller ConfigMap, and ServiceAccount.

## Prerequisites

- A Kubernetes cluster
- Helm 3
- Permission to create CRDs and cluster-scoped RBAC resources
- A Volcano Helm chart version that supports `custom.hypernode_controller_mode`

## Install with Helm

Add the Volcano Helm repository:

```bash
helm repo add volcano-sh https://volcano-sh.github.io/helm-charts
helm repo update
```

Install only the standalone HyperNode controller:

```bash
helm install volcano volcano-sh/volcano \
  --namespace volcano-system \
  --create-namespace \
  --set custom.hypernode_controller_mode=standalone \
  --set custom.controller_enable=false \
  --set custom.scheduler_enable=false \
  --set custom.admission_enable=false
```

When installing from a Volcano source checkout, replace `volcano-sh/volcano` with `./installer/helm/chart/volcano`.

The following values define this topology-only installation:

| Helm value | Purpose |
| --- | --- |
| `custom.hypernode_controller_mode=standalone` | Runs HyperNode reconciliation in the standalone controller. |
| `custom.controller_enable=false` | Does not install `vc-controller-manager`. |
| `custom.scheduler_enable=false` | Does not install `vc-scheduler`. |
| `custom.admission_enable=false` | Does not install `vc-webhook-manager`. |

The same configuration can be stored in a values file:

```yaml
custom:
  hypernode_controller_mode: standalone
  controller_enable: false
  scheduler_enable: false
  admission_enable: false
```

Then install it with:

```bash
helm install volcano volcano-sh/volcano \
  --namespace volcano-system \
  --create-namespace \
  --values hypernode-standalone-values.yaml
```

## Configure Topology Discovery

Configure discovery through `custom.controller_config_override` or the controller ConfigMap, as described in [How to Use HyperNode Auto Discovery](./how_to_use_hypernode_auto_discovery.md). The standalone controller and `vc-controller-manager` use the same discovery configuration.

By default, the standalone controller can read discovery credential Secrets in its installation namespace. If a discovery configuration references a Secret in another namespace, grant its ServiceAccount access to that Secret with a Role and RoleBinding in the Secret's namespace.

## Verify the Installation

Wait for the standalone controller to become ready:

```bash
kubectl rollout status deployment/volcano-hypernode-controller \
  --namespace volcano-system
```

List the installed Volcano workloads:

```bash
kubectl get deployments --namespace volcano-system
```

With the default release name and values above, `volcano-hypernode-controller` is the only Volcano control-plane Deployment. Resource names use the Helm release name as their prefix if a different release name is selected.

Check the controller logs and discovered HyperNode resources:

```bash
kubectl logs --namespace volcano-system \
  deployment/volcano-hypernode-controller \
  --container hypernode-controller-manager
kubectl get hypernodes
```

## Migrate an Existing Installation

When changing HyperNode ownership from `vc-controller-manager` to the standalone controller, first disable HyperNode reconciliation and wait for the existing controller manager to roll out:

```bash
helm upgrade volcano volcano-sh/volcano \
  --namespace volcano-system \
  --reuse-values \
  --set custom.hypernode_controller_mode=disabled

kubectl rollout status deployment/volcano-controllers \
  --namespace volcano-system
```

Then enable the standalone controller. To retain the other Volcano components, change only `custom.hypernode_controller_mode`. To convert the installation to a topology-only deployment, also disable the controller manager, scheduler, and admission webhook:

```bash
helm upgrade volcano volcano-sh/volcano \
  --namespace volcano-system \
  --reuse-values \
  --set custom.hypernode_controller_mode=standalone \
  --set custom.controller_enable=false \
  --set custom.scheduler_enable=false \
  --set custom.admission_enable=false
```

The intermediate `disabled` mode prevents the embedded and standalone controllers from reconciling HyperNode resources at the same time.
