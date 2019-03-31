# Generic variables.
INSTANCE_PREFIX="kubernetes"
SERVICE_CLUSTER_IP_RANGE="10.0.0.0/16"
EVENT_PD="false"

# Etcd related variables.
ETCD_IMAGE="3.3.10-0"
ETCD_VERSION=""

# Controller-manager related variables.
CONTROLLER_MANAGER_TEST_ARGS=" --v=4   "
ALLOCATE_NODE_CIDRS="true"
CLUSTER_IP_RANGE="10.64.0.0/14"
TERMINATED_POD_GC_THRESHOLD="100"

# Scheduler related variables.
SCHEDULER_TEST_ARGS=" --v=4  "

# Apiserver related variables.
APISERVER_TEST_ARGS=" --runtime-config=extensions/v1beta1,scheduling.k8s.io/v1alpha1 --v=4  --delete-collection-workers=16"
STORAGE_MEDIA_TYPE=""
STORAGE_BACKEND="etcd3"
ETCD_SERVERS=""
ETCD_SERVERS_OVERRIDES=""
ETCD_COMPACTION_INTERVAL_SEC=""
RUNTIME_CONFIG=""
NUM_NODES="3"
CUSTOM_ADMISSION_PLUGINS="NamespaceLifecycle,LimitRanger,ServiceAccount,PersistentVolumeLabel,DefaultStorageClass,DefaultTolerationSeconds,NodeRestriction,Priority,StorageObjectInUseProtection,MutatingAdmissionWebhook,ValidatingAdmissionWebhook,ResourceQuota"
FEATURE_GATES="ExperimentalCriticalPodAnnotation=true"
KUBE_APISERVER_REQUEST_TIMEOUT="300"
ENABLE_APISERVER_ADVANCED_AUDIT="false"
