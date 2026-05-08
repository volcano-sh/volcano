# kube-apiserver-audit-exporter for Volcano

Vendored copy of `kube-apiserver-audit-exporter`. Used by the Volcano
benchmark framework to derive scheduling latency metrics from kube-apiserver
audit logs.

## Why this is here

`pod.creationTimestamp` and `cond.LastTransitionTime` on the Pod object
serialize as `metav1.Time` (second precision), which is too coarse for
benchmarking. Audit events use `metav1.MicroTime` (microsecond precision)
and contain the same information.

`pods/binding` create events are the universal binding entrypoint in
Kubernetes — every scheduler (kube-scheduler, Volcano, Kueue, YuniKorn,
custom) goes through it. So a single set of audit-derived metrics applies
to all of them, which is what makes Volcano's benchmark suite reusable
for cross-scheduler comparison.

The exporter also reads the audit log file directly, not via the apiserver,
so it doesn't add API load that would skew the workload it's measuring.

## Source

- Repository: `https://github.com/wzshiming/kube-apiserver-audit-exporter`
- Imported commit: `57ebc3c2fcfc1aeba53c7cb82e2143fbe41fbe2a` (master, 2025-03-24)
- Copied files:
    - `audit-policy.yaml`
    - `LICENSE`
    - `exporter/exporter.go`
    - `exporter/metrics.go`
    - `exporter/model.go`
    - `exporter/utils.go`
    - `cmd/kube-apiserver-audit-exporter/main.go`

## Modifications

Only one change vs upstream: the Go module import path in `main.go`.

- `github.com/wzshiming/kube-apiserver-audit-exporter/exporter`
  → `volcano.sh/volcano/third_party/kube-apiserver-audit-exporter/exporter`

This lets the package build inside the Volcano go.mod without a separate
module. CLI flags, audit policy, and Prometheus metric definitions are
unchanged from upstream, so the binary still works as a standalone tool
against any Kubernetes cluster.

Volcano-specific extensions (extra labels such as `scheduler`, workload
aggregation, snapshot HTTP endpoint, etc.) intentionally live outside
this directory so the diff against upstream stays minimal.

## Usage

### Build

```bash
go build -o bin/kube-apiserver-audit-exporter \
    ./third_party/kube-apiserver-audit-exporter/cmd/kube-apiserver-audit-exporter
```

### Audit policy

Apply `audit-policy.yaml` to the apiserver via
`--audit-policy-file=/etc/kubernetes/policies/audit-policy.yaml`, and have
it write audit events to `--audit-log-path=/var/log/kubernetes/audit.log`.
The policy records `RequestResponse` for pods, batch jobs, and volcano
jobs only; `omitManagedFields` and `omitStages` keep the log size down.

### Run

```bash
./bin/kube-apiserver-audit-exporter \
    --audit-log-path=/var/log/kubernetes/audit.log \
    --address=:8080 \
    --cluster-label=volcano-benchmark
```

Metrics are exposed at `http://<exporter>:8080/metrics`. See `main.go` for
all flags.

### Prometheus metrics

| Metric | Type | Labels | Description |
|--------|------|--------|-------------|
| `pod_scheduling_latency_seconds` | Histogram | `cluster, namespace, user` | Time from pod create to `pods/binding` create |
| `batchjob_completion_latency_seconds` | Histogram | `cluster, namespace, user` | Time from job (`batch.Job` or `batch.volcano.sh/Job`) creation to completion |
| `api_requests_total` | Counter | `cluster, namespace, user, verb, resource, code` | API requests captured by audit |
| `pod_deleted_total` | Counter | `cluster, namespace, user, phase` | Pod deletions by terminal phase |
| `pod_completed_total` | Counter | `cluster, namespace, user, phase` | Pods that reached `Succeeded` / `Failed` |

The `user` label is parsed from the audit event's `userAgent`. For
`pods/binding` events that's the scheduler (`kube-scheduler`, `volcano`,
`kueue-scheduler`, ...), which is what makes the metrics comparable across
schedulers.

## Upgrade Process

The code is maintained as a manual copy — there is no automated upgrade.
Pick one of the two methods depending on how big the upstream change is.

### Method 1: Apply Diffs

For minor upgrades where the diff is small and easy to review.

1. Clone upstream:
   ```bash
   git clone https://github.com/wzshiming/kube-apiserver-audit-exporter.git
   cd kube-apiserver-audit-exporter
   ```

2. Generate a diff between the imported commit and the target. Replace
   `<OLD>` with the SHA recorded in this README and `<NEW>` with the target:
   ```bash
   git diff <OLD>..<NEW> -- exporter cmd audit-policy.yaml
   ```

3. Apply the changes to the files in this directory by hand. Remember
   to keep the import path modification described above.

### Method 2: Delete and Replace

For larger upgrades, where applying diffs by hand would be error-prone.

1. Delete the existing `exporter/`, `cmd/`, and `audit-policy.yaml` here.
2. Copy them over from a fresh upstream checkout at the new commit.
3. Re-apply the import path change in `main.go`. The package will not
   build inside the Volcano module without it.

After upgrading, update the imported commit SHA in this README and
mention the new SHA in the upgrade commit message.

## License

MIT, inherited from upstream. See `LICENSE`.
