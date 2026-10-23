# Job activeDeadlineSeconds

[NightG1owZ]; Oct 2026

Tracking: [#2761](https://github.com/volcano-sh/volcano/issues/2761) |
Related: [#4807](https://github.com/volcano-sh/volcano/issues/4807) (`apis` sources now live in
`staging/src/volcano.sh/apis`). Supersedes [volcano-sh/apis#138](https://github.com/volcano-sh/apis/pull/138).

## Motivation

A vcjob has no job-level bound on how long it may run, so a job that never installs a completed-exit
mechanism holds queue quota, gang resources and nodes indefinitely; today only manual deletion helps.
Kubernetes `Job` solves this with `spec.activeDeadlineSeconds`, enforced by the job controller.

Volcano has no equivalent: `tasks[].template.spec.activeDeadlineSeconds` kills a single pod (and
`restartPolicy: OnFailure` revives it) without ending the job; `spec.policies` covers task
events, not elapsed time; `spec.runningEstimate` is a scheduler hint, not an enforcement contract.

### Scenarios

| # | Scenario | Why nothing else covers it |
|---|---|---|
| S1 | A job never installs a completed-exit mechanism and runs forever | The case in #2761; only manual deletion helps |
| S2 | A training / MPI / debug job hangs while still holding GPUs | The pod-level deadline kills pods one by one and `OnFailure` restarts them |
| S3 | Fair sharing: one tenant monopolises a queue or gang resources | Queue quota bounds capacity, not one job's lifetime |
| S4 | A wall-clock pipeline whose downstream step must start on time | Downstream cannot tell "busy" from "deadlocked" |
| S5 | Cost control on burst capacity | Billing accrues with no upper bound |

Waiting time in the queue also counts against the deadline (see "Deadline Anchor"). That is a
consequence of the design and must be stated in the user guide.

### Relationship to existing fields

| Field | Bounds | Measured from | Ends the job? |
|---|---|---|---|
| `spec.activeDeadlineSeconds` (this proposal) | How long the job may be **active** | `status.startTime` | yes |
| `spec.ttlSecondsAfterFinished` | Retention of a **finished** job | `status.state.lastTransitionTime` | no, deletes it afterwards |
| `spec.maxRetry` | How often the job may be **restarted** | n/a | yes, on exhaustion (`Failed`) |
| `spec.runningEstimate` | Nothing; scheduler hint | n/a | no |
| `tasks[].template.spec.activeDeadlineSeconds` | One **pod** | pod `startTime` | no |

The first two compose: the job is ended at the deadline, then garbage collected after the TTL.

## Scope

**In scope**: `JobSpec.activeDeadlineSeconds`, `JobStatus.startTime`, controller enforcement, validation,
regenerated CRDs/clients, tests, user guide.

**Out of scope**: pod-level deadline semantics; scheduler behaviour based on deadline proximity;
`JobFlow` and `batch.volcano.sh` `CronJob` (follow-up); changing `status.runningDuration`.

## Design

### API

Add to `JobSpec` in [`staging/src/volcano.sh/apis/pkg/apis/batch/v1alpha1/job.go`](../../staging/src/volcano.sh/apis/pkg/apis/batch/v1alpha1/job.go):

```go
// ActiveDeadlineSeconds specifies the duration in seconds, relative to
// status.startTime, that the job may be active before the system terminates it.
// Once exceeded, the job enters Completing and ends in Completed with
// status.state.reason = DeadlineExceeded. Value must be a positive integer.
// +optional
// +kubebuilder:validation:Minimum=1
ActiveDeadlineSeconds *int64 `json:"activeDeadlineSeconds,omitempty" protobuf:"varint,14,opt,name=activeDeadlineSeconds"`
```

Add to `JobStatus` in the same file:

```go
// StartTime is when the job was first acknowledged by the job controller, and the
// anchor for spec.activeDeadlineSeconds. Written once; not updated on restart.
// +optional
StartTime *metav1.Time `json:"startTime,omitempty" protobuf:"bytes,14,opt,name=startTime"`
```

Tags verified against `master`: `JobSpec` uses 1-13 (`13` is `NetworkTopology`), `JobStatus`
uses 1-13 plus 21, so **14** is free in both. `*int64` uses `varint` (as `TTLSecondsAfterFinished`),
`*metav1.Time` uses `bytes` (as upstream k8s).

```yaml
apiVersion: batch.volcano.sh/v1alpha1
kind: Job
metadata:
  name: bounded-job
spec:
  minAvailable: 2
  activeDeadlineSeconds: 3600
  tasks:
    - replicas: 2
      name: worker
      template:
        spec:
          restartPolicy: OnFailure
          containers:
            - name: worker
              image: ubuntu
```

### Deadline Anchor

`status.startTime` is stamped on the first reconciliation of a job that sets
`activeDeadlineSeconds` and has no `startTime` yet. This matches k8s, which sets `startTime` on
first reconcile regardless of whether any pod exists.

**Queue time therefore counts against the deadline.** A job that spends most of its budget waiting to
be scheduled can be ended before it runs. This bounds total residency, which is the point of the
field, but it differs from the intuitive "count from when the pods start"; the alternative anchor is
listed under "Alternatives Considered".

Existing timestamps cannot serve as the anchor: `status.state.lastTransitionTime` is rewritten on
every phase change and `state/running.go` can move a job from `Running` back to `Pending`;
`metadata.creationTimestamp` cannot express "continuously active" nor be reset;
`status.runningDuration` is an output, not an anchor.

A restart (`RestartJobAction`, which increments `status.retryCount`) does **not** reset `startTime`:
the deadline bounds the job, not a single attempt.

### Expiry Semantics

The job ends in **`Completed`** with `status.state.reason = DeadlineExceeded`. This is the decision
recorded in #2761 at the 2024-10-21 community meeting ("After the set time is reached, the vcjob
status changes to complete"), and it diverges from k8s, which ends as `Failed`.

Volcano's `Failed` is documented as "the job is restarted failed reached the maximum number of
retries" and is produced by the retry paths in `state/running.go` and `state/restarting.go`. A
deadline is not retry exhaustion, so reusing `Failed` would overload a phase whose meaning is
already fixed. `Completed` is volcano's "the job is over and its resources are released" terminal.

Because the phase becomes `Completed`, `isJobFinished` in the garbage collector already treats
the job as finished, so `ttlSecondsAfterFinished` starts counting at expiry with no extra work.

Doc debt to pay off: the `Completed` comment in `job.go` and the phase list in
[`docs/design/job-api.md`](job-api.md) both claim "all tasks are completed"; both must mention this path.

### Phase Transitions

Only non-terminal phases are checked. No new phase, no new state file.

| From | Trigger | To | Notes |
|---|---|---|---|
| `Pending` | deadline exceeded | `Completing` | same path as `CompleteJobAction` |
| `Running` | deadline exceeded | `Completing` | pods soft-deleted, plugins cleaned up, PodGroup deleted |
| `Restarting` | deadline exceeded | `Completing` | deadline wins over `maxRetry` |
| `Completing` | pods drained | `Completed` | existing behaviour (`state/completing.go`) |
| `Completed`/`Failed`/`Terminated`/`Aborted`/`Aborting`/`Terminating` | n/a | n/a | terminal or draining; never touched |

Expiry does not flip the phase instantly because pods must drain first, matching upstream, which
since v1.31 also delays the terminal condition while pods terminate.

### Controller Behaviour

```go
func pastActiveDeadline(job *batch.Job) bool {
    if job.Spec.ActiveDeadlineSeconds == nil || job.Status.StartTime == nil {
        return false
    }
    return time.Since(job.Status.StartTime.Time) >=
        time.Duration(*job.Spec.ActiveDeadlineSeconds)*time.Second
}
```

1. Check inside `processNextReq` in
   [`pkg/controllers/job/job_controller.go`](../../pkg/controllers/job/job_controller.go) before
   `st.Execute(action)`, which guarantees no pod is created after expiry and keeps enforcement out of
   the per-phase state files.
2. On expiry dispatch the existing `CompleteJobAction` rather than writing status directly, so
   `killJob` (soft-delete pods, `OnJobDelete` plugin hooks, PodGroup deletion) is reused unchanged.
3. Stamp `startTime` on the same path before the comparison, so a job is never evaluated against an
   unset anchor.

The controller is event-driven only: informer resync is **0**
(`cmd/controller-manager/app/server.go`) and there is no `--resync-period` flag, so a quiet job would
never be re-checked. Arm `queue.AddAfter(key, remaining)` on job add/update, the same mechanism
upstream k8s uses and this repo already uses in the garbage collector (`gc.enqueueAfter`).

### Validation and Mutability

* Unset (`nil`) means no deadline: today's behaviour, no migration.
* `Minimum=1` rejects `0` and negatives at the CRD schema, so no webhook range check is needed. `0`
  is rejected deliberately: k8s accepts it (`ValidateNonnegativeField`) even though its comment says
  "positive integer", and such a job expires on its first reconcile.
* The field is **immutable after creation**. `validateJobUpdate` in
  [`pkg/webhooks/admission/jobs/validate/admit_job.go`](../../pkg/webhooks/admission/jobs/validate/admit_job.go)
  copies old values over the new spec and compares the whole spec, so a change is already rejected;
  the error message enumerates the fields that may change and must stay accurate, and the intended
  immutability is stated here. The ValidatingAdmissionPolicy needs no change.

### Observability

| Signal | Value |
|---|---|
| `status.state.phase` | `Completed` |
| `status.state.reason` | `DeadlineExceeded` |
| `status.state.message` | "job was active longer than the specified deadline" |
| `status.conditions` | a `JobCondition` via the existing `newCondition`/`appendJobCondition` helpers |
| Event | `Warning` event; new reason in the `JobEvent` enum in `job.go` |
| Metrics | reuse `UpdateJobCompleted` in `pkg/controllers/job/state/metrics.go` |

## Feature Interaction

* **Queue / quota**: nothing to do; quota is released as pods are deleted.
* **Gang / PodGroup**: `killJob` deletes the PodGroup on a full kill (`target == nil`), so the scheduler
  drops the job immediately.
* **Plugins** (`svc`, `ssh`, `env`, distributed frameworks): cleaned up by existing `OnJobDelete` hooks.
* **`minAvailable` / `minSuccess` / `maxRetry`**: expiry takes precedence; no `RestartJobAction` is produced.
* **`ttlSecondsAfterFinished`**: starts counting at expiry, because `lastTransitionTime` is set then.
* **`CronJob`**: its `startingDeadlineSeconds` is a scheduling tolerance for missed occurrences, not
  a run-time bound; do not confuse the two.

## Compatibility and Limitations

1. **No migration**: jobs without the field are unaffected.
2. **Queue time counts** (see "Deadline Anchor").
3. **Forced termination**: pods are interrupted and `status.succeeded`/`status.failed` may be non-zero
   while the phase is `Completed`, so automation must key on `status.state.reason`, not the phase alone
   (cf. kubernetes/kubernetes#117303).
4. **Pre-existing inconsistency, untouched**: `status.runningDuration` is documented as "from job running
   to complete" but computed from `metadata.creationTimestamp`; `status.startTime` is the accurate
   anchor and a follow-up can align them.
5. **Name collision**: the CRD already has `activeDeadlineSeconds` under `spec.tasks[].template.spec`
   (from `PodTemplateSpec`). The user guide must say which level to use.

## Alternatives Considered

1. **Pod-level `activeDeadlineSeconds` only**: kills pods, not the job; `OnFailure` revives them.
2. **External reaper**: extra operational surface, races with the controller.
3. **Reuse `runningEstimate`**: a scheduler hint for SLA logic; overloading it changes an existing field.
4. **Anchor on `creationTimestamp` or `status.state.lastTransitionTime`**: see "Deadline Anchor" — the
   former cannot express "continuously active", the latter is rewritten on every phase change.
5. **End as `Failed` (strict k8s parity)**: reviewers who prefer it can flip the terminal phase with a
   one-line change; the transition, condition and event are identical either way.
6. **Anchor on the first transition into `Running`** so queue time does not count: not chosen for v1
   because it diverges from k8s without user demand and reintroduces "a job that never schedules never
   expires". A straightforward follow-up if the community wants a queue-fair variant.

## Implementation Plan

```bash
cd staging/src/volcano.sh/apis && bash hack/update-codegen.sh
make manifests generate-yaml
make verify-generated-yaml
```

Regenerates `zz_generated.deepcopy.go`, `pkg/client/applyconfiguration/batch/v1alpha1/job{spec,status}.go`,
`config/crd/volcano/bases/batch.volcano.sh_jobs.yaml`,
`installer/helm/chart/volcano/crd/bases/batch.volcano.sh_jobs.yaml` and
`installer/volcano-development{,-vap,-vap-map}.yaml`.

Then: controller changes above; confirm the `validateJobUpdate` message; user-guide section, an
`example/` yaml, and the two doc updates in "Expiry Semantics".

## Test Plan

Unit (`pkg/controllers/job`):

* Deadline not reached: job proceeds, no terminal transition.
* Deadline exceeded in `Running`: pods soft-deleted, phase `Completed`, reason `DeadlineExceeded`,
  condition and event emitted.
* Deadline exceeded in `Pending`: job ends without any pod ever created.
* `startTime` stamped once across repeated syncs; a restart does not reset it.
* Job without the field unaffected (regression); terminal phases never re-evaluated.

E2E (`test/e2e`):

* Short `activeDeadlineSeconds`: assert terminal phase, reason, condition, event, and that all pods are gone.
* With `ttlSecondsAfterFinished`: job is collected after a TTL measured from the expiry moment.
* Restart the controller-manager between creation and expiry: the deadline is still enforced.

