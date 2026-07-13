module volcano.sh/volcano

go 1.26.0

require (
	github.com/AdaLogics/go-fuzz-headers v0.0.0-20240806141605-e8a1dd7889d6
	github.com/agiledragon/gomonkey/v2 v2.11.0
	github.com/cilium/ebpf v0.17.3
	github.com/containernetworking/cni v1.1.2
	github.com/containernetworking/plugins v1.1.1
	github.com/elastic/go-elasticsearch/v7 v7.17.7
	github.com/fsnotify/fsnotify v1.9.0
	github.com/golang/mock v1.6.0
	github.com/google/go-cmp v0.7.0
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/hashicorp/go-multierror v1.1.1
	github.com/imdario/mergo v0.3.16
	github.com/mitchellh/mapstructure v1.5.0
	github.com/moby/sys/userns v0.1.0
	github.com/onsi/ginkgo/v2 v2.28.1
	github.com/onsi/gomega v1.39.1
	github.com/opencontainers/cgroups v0.0.6
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.23.2
	github.com/prometheus/common v0.67.5
	github.com/prometheus/prometheus v0.311.3
	github.com/robfig/cron/v3 v3.0.1
	github.com/spf13/cobra v1.10.2
	github.com/spf13/pflag v1.0.10
	github.com/stretchr/testify v1.11.1
	github.com/vishvananda/netlink v1.3.1
	go.uber.org/automaxprocs v1.6.0
	golang.org/x/crypto v0.53.0
	golang.org/x/sys v0.46.0
	golang.org/x/time v0.15.0
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.36.1
	k8s.io/apimachinery v0.36.1
	k8s.io/apiserver v0.36.1
	k8s.io/client-go v0.36.1
	k8s.io/code-generator v0.36.1
	k8s.io/component-base v0.36.1
	k8s.io/component-helpers v0.36.1
	k8s.io/dynamic-resource-allocation v0.36.1
	k8s.io/klog/v2 v2.140.0
	k8s.io/kubectl v0.0.0
	k8s.io/kubernetes v1.36.1
	k8s.io/metrics v0.36.1
	k8s.io/pod-security-admission v0.0.0
	k8s.io/utils v0.0.0-20260210185600-b8788abfbbc2
	sigs.k8s.io/controller-runtime v0.24.1
	sigs.k8s.io/e2e-framework v0.6.0
	sigs.k8s.io/yaml v1.6.0
	stathat.com/c/consistent v1.0.0
	volcano.sh/apis v0.0.0
)

require (
	cel.dev/expr v0.25.1 // indirect
	cyphar.com/go-pathrs v0.2.2 // indirect
	github.com/Azure/go-ansiterm v0.0.0-20250102033503-faa5f7b0171c // indirect
	github.com/JeffAshton/win_pdh v0.0.0-20161109143554-76bb4ee9f0ab // indirect
	github.com/MakeNowJust/heredoc v1.0.0 // indirect
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/Microsoft/go-winio v0.6.2 // indirect
	github.com/Microsoft/hnslib v0.1.2 // indirect
	github.com/NYTimes/gziphandler v1.1.1 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.0 // indirect
	github.com/cenkalti/backoff/v5 v5.0.3 // indirect
	github.com/chai2010/gettext-go v1.0.2 // indirect
	github.com/container-storage-interface/spec v1.9.0 // indirect
	github.com/containerd/containerd/api v1.10.0 // indirect
	github.com/containerd/errdefs v1.0.0 // indirect
	github.com/containerd/errdefs/pkg v0.3.0 // indirect
	github.com/containerd/log v0.1.0 // indirect
	github.com/containerd/ttrpc v1.2.7 // indirect
	github.com/containerd/typeurl/v2 v2.2.3 // indirect
	github.com/coreos/go-semver v0.3.1 // indirect
	github.com/cyphar/filepath-securejoin v0.6.1 // indirect
	github.com/docker/go-units v0.5.0 // indirect
	github.com/euank/go-kmsg-parser v2.0.0+incompatible // indirect
	github.com/exponent-io/jsonpath v0.0.0-20210407135951-1de76d718b3f // indirect
	github.com/fatih/camelcase v1.0.0 // indirect
	github.com/go-errors/errors v1.4.2 // indirect
	github.com/go-openapi/swag/cmdutils v0.25.5 // indirect
	github.com/go-openapi/swag/conv v0.25.5 // indirect
	github.com/go-openapi/swag/fileutils v0.25.5 // indirect
	github.com/go-openapi/swag/jsonname v0.25.5 // indirect
	github.com/go-openapi/swag/jsonutils v0.25.5 // indirect
	github.com/go-openapi/swag/loading v0.25.5 // indirect
	github.com/go-openapi/swag/mangling v0.25.5 // indirect
	github.com/go-openapi/swag/netutils v0.25.5 // indirect
	github.com/go-openapi/swag/stringutils v0.25.5 // indirect
	github.com/go-openapi/swag/typeutils v0.25.5 // indirect
	github.com/go-openapi/swag/yamlutils v0.25.5 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/godbus/dbus/v5 v5.2.2 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/grafana/regexp v0.0.0-20250905093917-f7b3be9d1853 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/providers/prometheus v1.1.0 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.3.3 // indirect
	github.com/kylelemons/godebug v1.1.0 // indirect
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de // indirect
	github.com/mistifyio/go-zfs v2.1.2-0.20190413222219-f784269be439+incompatible // indirect
	github.com/mitchellh/go-wordwrap v1.0.1 // indirect
	github.com/moby/spdystream v0.5.1 // indirect
	github.com/moby/term v0.5.2 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/opencontainers/runtime-spec v1.3.0 // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/sirupsen/logrus v1.9.4 // indirect
	github.com/vishvananda/netns v0.0.5 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	go.etcd.io/etcd/api/v3 v3.6.8 // indirect
	go.etcd.io/etcd/client/v3 v3.6.8 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.opentelemetry.io/contrib/instrumentation/github.com/emicklei/go-restful/otelrestful v0.65.0 // indirect
	go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc v0.65.0 // indirect
	go.yaml.in/yaml/v2 v2.4.4 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	gopkg.in/natefinch/lumberjack.v2 v2.2.1 // indirect
	k8s.io/cli-runtime v0.36.1 // indirect
	k8s.io/cri-api v0.36.1 // indirect
	k8s.io/cri-client v0.0.0 // indirect
	k8s.io/cri-streaming v0.0.0 // indirect
	k8s.io/csi-translation-lib v0.36.1 // indirect
	k8s.io/gengo/v2 v2.0.0-20250922181213-ec3ebc5fd46b // indirect
	k8s.io/kms v0.36.1 // indirect
	k8s.io/streaming v0.36.1 // indirect
	sigs.k8s.io/kustomize/api v0.21.1 // indirect
	sigs.k8s.io/kustomize/kyaml v0.21.1 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.2 // indirect
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/coreos/go-systemd/v22 v22.7.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/emicklei/go-restful/v3 v3.13.0 // indirect
	github.com/evanphx/json-patch/v5 v5.9.11 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.22.5 // indirect
	github.com/go-openapi/jsonreference v0.21.4 // indirect
	github.com/go-openapi/swag v0.25.5 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/cadvisor v0.56.2 // indirect
	github.com/google/cel-go v0.26.0 // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20260302011040-a15ffb7f9dcc // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.28.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/moby/sys/mountinfo v0.7.2 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/selinux v1.13.1 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/prometheus/client_model v0.6.2
	github.com/prometheus/procfs v0.19.2 // indirect
	github.com/stoewer/go-strcase v1.3.0 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.6.8 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.67.0 // indirect
	go.opentelemetry.io/otel v1.43.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.42.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.42.0 // indirect
	go.opentelemetry.io/otel/metric v1.43.0 // indirect
	go.opentelemetry.io/otel/sdk v1.43.0 // indirect
	go.opentelemetry.io/otel/trace v1.43.0 // indirect
	go.opentelemetry.io/proto/otlp v1.9.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	golang.org/x/exp v0.0.0-20260218203240-3dfff04db8fa // indirect
	golang.org/x/mod v0.36.0 // indirect
	golang.org/x/net v0.55.0 // indirect
	golang.org/x/oauth2 v0.36.0 // indirect
	golang.org/x/sync v0.21.0 // indirect
	golang.org/x/term v0.44.0 // indirect
	golang.org/x/text v0.38.0 // indirect
	golang.org/x/tools v0.45.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20260319201613-d00831a3d3e7 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260311181403-84a4fc48630c // indirect
	google.golang.org/grpc v1.79.3 // indirect
	google.golang.org/protobuf v1.36.12-0.20260120151049-f2248ac996af // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiextensions-apiserver v0.36.1 // indirect
	k8s.io/cloud-provider v0.0.0 // indirect
	k8s.io/controller-manager v0.36.1
	k8s.io/kube-openapi v0.0.0-20260317180543-43fb72c5454a // indirect
	k8s.io/kube-scheduler v0.0.0
	k8s.io/kubelet v0.36.1 // indirect
	k8s.io/mount-utils v0.0.0 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.34.0 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
)

replace (
	cloud.google.com/go => cloud.google.com/go v0.100.2
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc => go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.42.0
	google.golang.org/grpc => google.golang.org/grpc v1.79.3
	k8s.io/api => k8s.io/api v0.36.1
	k8s.io/apiextensions-apiserver => k8s.io/apiextensions-apiserver v0.36.1
	k8s.io/apimachinery => k8s.io/apimachinery v0.36.1
	k8s.io/apiserver => k8s.io/apiserver v0.36.1
	k8s.io/cli-runtime => k8s.io/cli-runtime v0.36.1
	k8s.io/client-go => k8s.io/client-go v0.36.1
	k8s.io/cloud-provider => k8s.io/cloud-provider v0.36.1
	k8s.io/cluster-bootstrap => k8s.io/cluster-bootstrap v0.36.1
	k8s.io/code-generator => k8s.io/code-generator v0.36.1
	k8s.io/component-base => k8s.io/component-base v0.36.1
	k8s.io/component-helpers => k8s.io/component-helpers v0.36.1
	k8s.io/controller-manager => k8s.io/controller-manager v0.36.1
	k8s.io/cri-api => k8s.io/cri-api v0.36.1
	k8s.io/cri-client => k8s.io/cri-client v0.36.1
	k8s.io/cri-streaming => k8s.io/cri-streaming v0.36.1
	k8s.io/csi-translation-lib => k8s.io/csi-translation-lib v0.36.1
	k8s.io/dynamic-resource-allocation => k8s.io/dynamic-resource-allocation v0.36.1
	k8s.io/endpointslice => k8s.io/endpointslice v0.36.1
	k8s.io/externaljwt => k8s.io/externaljwt v0.36.1
	k8s.io/kube-aggregator => k8s.io/kube-aggregator v0.36.1
	k8s.io/kube-controller-manager => k8s.io/kube-controller-manager v0.36.1
	k8s.io/kube-proxy => k8s.io/kube-proxy v0.36.1
	k8s.io/kube-scheduler => k8s.io/kube-scheduler v0.36.1
	k8s.io/kubectl => k8s.io/kubectl v0.36.1
	k8s.io/kubelet => k8s.io/kubelet v0.36.1
	k8s.io/legacy-cloud-providers => k8s.io/legacy-cloud-providers v0.36.1
	k8s.io/metrics => k8s.io/metrics v0.36.1
	k8s.io/mount-utils => k8s.io/mount-utils v0.36.1
	k8s.io/node-api => k8s.io/node-api v0.36.1
	k8s.io/pod-security-admission => k8s.io/pod-security-admission v0.36.1
	k8s.io/sample-apiserver => k8s.io/sample-apiserver v0.36.1
	k8s.io/sample-cli-plugin => k8s.io/sample-cli-plugin v0.36.1
	k8s.io/sample-controller => k8s.io/sample-controller v0.36.1
	// Use local staging directory for APIs development
	// This allows API changes to be made and reviewed in the same PR as implementation changes
	volcano.sh/apis => ./staging/src/volcano.sh/apis
)
