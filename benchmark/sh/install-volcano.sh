#helm install volcano --set basic.image_pull_policy="IfNotPresent" --set basic.image_tag_version="v1.10.0" \
#     --set custom.controller_kube_api_qps=3000 --set custom.controller_kube_api_burst=3000 --set custom.scheduler_kube_api_qps=10000 --set custom.scheduler_kube_api_burst=10000 \
#     --set custom.controller_metrics_enable=false --set custom.scheduler_node_worker_threads=200 --set custom.scheduler_schedule_period=200ms \
#     --set custom.controller_worker_threads=100 --set custom.controller_worker_threads_for_gc=100 --set custom.controller_worker_threads_for_podgroup=50 \
#     --set-file custom.scheduler_config_override=./custom_scheduler_config.yaml ../../installer/helm/chart/volcano -n volcano-system --create-namespace

helm install volcano --set basic.image_pull_policy="IfNotPresent" --set basic.image_tag_version="v1.10.0" --set custom.controller_metrics_enable=false \
     --set custom.controller_kube_api_qps=3000 --set custom.controller_kube_api_burst=3000 --set custom.scheduler_kube_api_qps=10000 --set custom.scheduler_kube_api_burst=10000 \
     --set-file custom.scheduler_config_override=./custom_scheduler_config.yaml ../../installer/helm/chart/volcano -n volcano-system --create-namespace