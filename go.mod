module github.com/EnterpriseDB/cloud-native-postgresql

go 1.13

require (
	github.com/go-logr/logr v0.1.0
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51
	github.com/lib/pq v1.3.0
	github.com/onsi/ginkgo v1.13.0
	github.com/onsi/gomega v1.10.1
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.0.0
	github.com/prometheus/common v0.4.1
	github.com/robfig/cron v1.2.0
	github.com/sethvargo/go-password v0.1.3
	github.com/spf13/cobra v1.0.0
	go.uber.org/zap v1.10.0
	k8s.io/api v0.17.3
	k8s.io/apimachinery v0.17.3
	k8s.io/cli-runtime v0.17.3
	k8s.io/client-go v0.17.3
	sigs.k8s.io/controller-runtime v0.5.0
)
