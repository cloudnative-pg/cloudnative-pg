module github.com/cloudnative-pg/cloudnative-pg

go 1.23

toolchain go1.23.3

require (
	github.com/DATA-DOG/go-sqlmock v1.5.2
	github.com/Masterminds/semver/v3 v3.3.0
	github.com/avast/retry-go/v4 v4.6.0
	github.com/blang/semver v3.5.1+incompatible
	github.com/cheynewallace/tabby v1.1.1
	github.com/cloudnative-pg/barman-cloud v0.0.0-20241105055149-ae6c2408bd14
	github.com/cloudnative-pg/cnpg-i v0.0.0-20241105133936-c704f46c20e0
	github.com/cloudnative-pg/machinery v0.0.0-20241122084004-33b997fc6c61
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc
	github.com/evanphx/json-patch/v5 v5.9.0
	github.com/go-logr/logr v1.4.2
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/grpc-ecosystem/go-grpc-middleware/v2 v2.1.0
	github.com/jackc/pgx/v5 v5.7.1
	github.com/jackc/puddle/v2 v2.2.2
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51
	github.com/kubernetes-csi/external-snapshotter/client/v8 v8.0.0
	github.com/lib/pq v1.10.9
	github.com/logrusorgru/aurora/v4 v4.0.0
	github.com/mitchellh/go-ps v1.0.0
	github.com/onsi/ginkgo/v2 v2.21.0
	github.com/onsi/gomega v1.35.1
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.78.1
	github.com/prometheus/client_golang v1.20.5
	github.com/robfig/cron v1.2.0
	github.com/sethvargo/go-password v0.3.1
	github.com/spf13/cobra v1.8.1
	github.com/stern/stern v1.31.0
	github.com/thoas/go-funk v0.9.3
	go.uber.org/atomic v1.11.0
	go.uber.org/multierr v1.11.0
	go.uber.org/zap v1.27.0
	golang.org/x/term v0.26.0
	google.golang.org/grpc v1.68.0
	gopkg.in/yaml.v3 v3.0.1
	k8s.io/api v0.31.2
	k8s.io/apiextensions-apiserver v0.31.2
	k8s.io/apimachinery v0.31.2
	k8s.io/cli-runtime v0.31.2
	k8s.io/client-go v0.31.2
	k8s.io/utils v0.0.0-20241104163129-6fe5fd82f078
	sigs.k8s.io/controller-runtime v0.19.1
	sigs.k8s.io/yaml v1.4.0
)

require (
	github.com/Azure/go-ansiterm v0.0.0-20230124172434-306776ec8161 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/emicklei/go-restful/v3 v3.12.1 // indirect
	github.com/fatih/color v1.17.0 // indirect
	github.com/fsnotify/fsnotify v1.7.0 // indirect
	github.com/fxamacker/cbor/v2 v2.7.0 // indirect
	github.com/go-errors/errors v1.5.1 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.21.0 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/btree v1.1.3 // indirect
	github.com/google/gnostic-models v0.6.8 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20241029153458-d1b30febd7db // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.0 // indirect
	github.com/gregjones/httpcache v0.0.0-20190611155906-901d90724c79 // indirect
	github.com/imdario/mergo v0.3.16 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20240606120523-5a60cdf6a761 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/klauspost/compress v1.17.9 // indirect
	github.com/liggitt/tabwriter v0.0.0-20181228230101-89fcab3d43de // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/mattn/go-colorable v0.1.13 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/moby/spdystream v0.4.0 // indirect
	github.com/moby/term v0.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/peterbourgon/diskv v2.0.1+incompatible // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_model v0.6.1 // indirect
	github.com/prometheus/common v0.59.1 // indirect
	github.com/prometheus/procfs v0.15.1 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/x448/float16 v0.8.4 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	go.starlark.net v0.0.0-20240925182052-1207426daebd // indirect
	golang.org/x/crypto v0.28.0 // indirect
	golang.org/x/exp v0.0.0-20240909161429-701f63a606c0 // indirect
	golang.org/x/net v0.30.0 // indirect
	golang.org/x/oauth2 v0.23.0 // indirect
	golang.org/x/sync v0.8.0 // indirect
	golang.org/x/sys v0.27.0 // indirect
	golang.org/x/text v0.19.0 // indirect
	golang.org/x/time v0.7.0 // indirect
	golang.org/x/tools v0.26.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20240903143218-8af14fe29dc1 // indirect
	google.golang.org/protobuf v1.35.1 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.12.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/kube-openapi v0.0.0-20240903163716-9e1beecbcb38 // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/kustomize/api v0.17.3 // indirect
	sigs.k8s.io/kustomize/kyaml v0.17.2 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.4.1 // indirect
)
