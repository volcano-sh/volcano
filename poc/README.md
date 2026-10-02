# PoC: Namespace-scoped Queue for Volcano

LFX Mentorship 2026 Term 2 — CNCF Volcano  
**Feature:** Support Namespace-scoped Queue in Volcano

---

## What this PoC demonstrates

Volcano's `Queue` is cluster-scoped, so only cluster-admins can create one.  
Tenants who own a namespace cannot create queues for their own workloads.

This PoC shows the **shadow Queue approach** — the central architectural idea:

> The `NamespaceQueue` controller watches namespace-scoped `NamespaceQueue` objects
> and synthesises a cluster-scoped shadow `Queue` for each one. The Volcano scheduler
> picks up the shadow Queue through the existing Queue informer — **zero scheduler
> changes required**. The shadow Queue name is deterministic (`shadowQueueName`),
> making reconciliation idempotent.

```
Tenant                  Controller              Scheduler
------                  ----------              ---------
creates                 watches                 sees
NamespaceQueue    --->  NamespaceQueue    --->  shadow Queue
(namespaced)            creates/updates         (cluster-scoped)
                        shadow Queue            processes identically
                                                to any cluster Queue
```

---

## Files changed

| File | What |
|------|------|
| `staging/src/volcano.sh/apis/pkg/apis/scheduling/v1beta1/namespacequeue_types.go` | `NamespaceQueue`, `NamespaceQueueSpec`, `NamespaceQueueStatus`, `NamespaceQueueList` types + pre-codegen DeepCopy |
| `staging/src/volcano.sh/apis/pkg/apis/scheduling/v1beta1/register.go` | registers `NamespaceQueue`/`NamespaceQueueList` with the scheme |
| `staging/src/volcano.sh/apis/pkg/apis/scheduling/v1beta1/labels.go` | adds `NamespaceQueueNameAnnotationKey` constant |
| `pkg/controllers/namespacequeue/shadow_queue.go` | `shadowQueueName()` + `buildShadowQueue()` — the core logic |
| `pkg/controllers/namespacequeue/shadow_queue_test.go` | unit tests for the core logic |
| `pkg/controllers/namespacequeue/namespacequeue_controller.go` | full controller: dynamic informer, reconcile loop, shadow Queue create/update/delete, status mirroring |

---

## Run the unit tests

The unit tests cover the pure functions (`shadowQueueName`, `buildShadowQueue`,
`shadowQueueSpecChanged`) and require no cluster.

```bash
cd /path/to/volcano
go test ./pkg/controllers/namespacequeue/... -v
```

Expected output (all pass):

```
=== RUN   TestShadowQueueName_Format
--- PASS: TestShadowQueueName_Format
=== RUN   TestShadowQueueName_Deterministic
--- PASS: TestShadowQueueName_Deterministic
=== RUN   TestShadowQueueName_NoCollision
--- PASS: TestShadowQueueName_NoCollision
=== RUN   TestShadowQueueName_LongNameTruncated
--- PASS: TestShadowQueueName_LongNameTruncated
=== RUN   TestShadowQueueName_CrossNamespaceUnique
--- PASS: TestShadowQueueName_CrossNamespaceUnique
=== RUN   TestBuildShadowQueue_BasicSpec
--- PASS: TestBuildShadowQueue_BasicSpec
=== RUN   TestBuildShadowQueue_DefaultReclaimable
--- PASS: TestBuildShadowQueue_DefaultReclaimable
=== RUN   TestBuildShadowQueue_DefaultWeight
--- PASS: TestBuildShadowQueue_DefaultWeight
=== RUN   TestBuildShadowQueue_GuaranteeTranslated
--- PASS: TestBuildShadowQueue_GuaranteeTranslated
=== RUN   TestBuildShadowQueue_EmptyResourceListsOmitted
--- PASS: TestBuildShadowQueue_EmptyResourceListsOmitted
=== RUN   TestBuildShadowQueue_SpecIsolated
--- PASS: TestBuildShadowQueue_SpecIsolated
=== RUN   TestShadowQueueSpecChanged_DetectsDrift
--- PASS: TestShadowQueueSpecChanged_DetectsDrift
```

---

## Run against a local cluster (Kind)

**Prerequisites:** `kind`, `kubectl`, `helm`, Go 1.23+

```bash
# 1. Create a Kind cluster and install Volcano
kind create cluster --name volcano
helm install volcano installer/helm/chart/volcano --namespace volcano-system --create-namespace

# 2. (After `make generate-code`) apply the NamespaceQueue CRD
kubectl apply -f config/crd/bases/scheduling.volcano.sh_namespacequeues.yaml

# 3. Activate the controller by blank-importing the package in the controller-manager
#    (see cmd/controller-manager/main.go — add the import line below)
#    _ "volcano.sh/volcano/pkg/controllers/namespacequeue"

# 4. Create a NamespaceQueue as a tenant (no ClusterRoleBinding needed)
kubectl -n team-alpha apply -f - <<EOF
apiVersion: scheduling.volcano.sh/v1beta1
kind: NamespaceQueue
metadata:
  name: ml-training
  namespace: team-alpha
spec:
  weight: 5
  capability:
    cpu: "10"
    memory: "20Gi"
  deserved:
    cpu: "4"
    memory: "8Gi"
  reclaimable: true
EOF

# 5. Verify the shadow cluster Queue was created by the controller
kubectl get queue   # should show "nsq-<hash8>-ml-training"

# 6. Create a PodGroup referencing the NamespaceQueue
#    (admission webhook patches spec.queue to the shadow Queue name)
kubectl -n team-alpha apply -f - <<EOF
apiVersion: scheduling.volcano.sh/v1beta1
kind: PodGroup
metadata:
  name: my-job-pg
  namespace: team-alpha
  annotations:
    scheduling.volcano.sh/namespacequeue-name: ml-training
spec:
  minMember: 1
EOF
```

---

## What's not in this PoC (full implementation scope)

- **CRD manifest** — generated by `make manifests` after adding kubebuilder markers (done)
- **Generated lister/informer** — produced by `make generate-code`; replaces the dynamic informer in the controller
- **Admission webhook** — resolves `scheduling.volcano.sh/namespacequeue-name` annotation on PodGroup and patches `spec.queue` to the shadow Queue name
- **RBAC manifests** — tenant `Role` granting `namespacequeues` create/update/delete in their namespace
- **Status mirroring** — full watch on the shadow Queue status to propagate Allocated back to NamespaceQueueStatus
- **E2E tests** in `test/e2e/namespacequeue/`
