# Benchmark Scripts

| Script | Purpose | Called By |
|--------|---------|-----------|
| `common.sh` | Shared env vars, logging functions, `require_cmd`, `wait_for_deployment` | Sourced by all scripts |
| `create-cluster.sh` | Create Kind cluster with audit logging and NodePort mappings | `make create-cluster` |
| `create-kwok-nodes.sh` | Install KWOK controller and create simulated nodes | `make create-nodes` |
| `build-images.sh` | Build audit-exporter image and load into Kind cluster | `make build-images` |
| `install-volcano.sh` | Install Volcano via Helm (local source or release chart) | `make install-volcano` |
| `install-monitoring.sh` | Deploy Prometheus, Grafana, kube-state-metrics, audit-exporter | `make install-monitoring` |
| `run-tests.sh` | Apply scheduler config, run Go tests, auto-collect report | `make test-config`, `make test-gang-env` |
| `collect-report.sh` | Query audit-exporter metrics from Prometheus, write JSON report | Called by `run-tests.sh`; `make report` |
| `export-grafana-charts.sh` | Export Grafana dashboard panels as PNG via Render API | `make export-charts` |
| `cleanup.sh` | Tear down VCJobs, monitoring, Volcano, KWOK; optionally delete cluster | `make cleanup`, `make cleanup-all` |
| `cleanup-kwok-nodes.sh` | Delete KWOK nodes only (sub-task used by `cleanup.sh`) | `cleanup.sh` |
