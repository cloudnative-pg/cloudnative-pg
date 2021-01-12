module github.com/EnterpriseDB/cloud-native-postgresql

go 1.14

require (
	github.com/go-logr/logr v0.3.0
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510
	github.com/kballard/go-shellquote v0.0.0-20180428030007-95032a82bc51
	github.com/lib/pq v1.9.0
	github.com/onsi/ginkgo v1.14.2
	github.com/onsi/gomega v1.10.4
	github.com/prometheus/client_golang v1.7.1
	github.com/robfig/cron v1.2.0
	github.com/sethvargo/go-password v0.2.0
	github.com/spf13/cobra v1.1.1
	go.uber.org/zap v1.15.0
	k8s.io/api v0.20.1
	k8s.io/apimachinery v0.20.1
	k8s.io/cli-runtime v0.20.1
	k8s.io/client-go v0.20.1
	sigs.k8s.io/controller-runtime v0.7.0
)
