package specs

import (
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"

	apiv1 "github.com/cloudnative-pg/cloudnative-pg/api/v1"
)

// CreateMajorUpgradeJob creates a job to upgrade the primary node to a new major version
func CreateMajorUpgradeJob(cluster *apiv1.Cluster, nodeSerial int, oldImage string) *batchv1.Job {
	initContainerCommand := []string{
		"bash",
		"-exc",
		`rm -fr /controller/old

bindir=$(pg_config --bindir)
mkdir -p "/controller/old${bindir}"
cp -ax "${bindir}"/. "/controller/old${bindir}"

pkglibdir=$(pg_config --pkglibdir)
mkdir -p "/controller/old${pkglibdir}"
cp -ax "${pkglibdir}"/. "/controller/old${pkglibdir}"

sharedir=$(pg_config --sharedir)
mkdir -p "/controller/old${sharedir}"
cp -ax "${sharedir}"/. "/controller/old${sharedir}"

echo "/controller/old${bindir}" > /controller/old-bindir.txt
`,
	}

	upgradeCommand := []string{
		"bash",
		"-exc",
		`rm -fr /var/lib/postgresql/data/new

# Init the new data directory
mkdir /var/lib/postgresql/data/new
chmod 0700 /var/lib/postgresql/data/new
cd /var/lib/postgresql/data/new
initdb .

# Check if we have anything to update
new_version=$(cat PG_VERSION)
version=$(cat /var/lib/postgresql/data/pgdata/PG_VERSION)
if [ "$version" == "$new_version" ]; then
    cd /var/lib/postgresql/data
    rm -fr /var/lib/postgresql/data/new
    exit 0
fi

# Take out all the unused stuff from the custom.conf
# TODO: when running inside a real job create proper configurations
cat > custom.conf << EOF
unix_socket_directories = '/controller/run'
EOF
cat >> postgresql.conf << EOF
# load CloudNativePG custom.conf configuration
include 'custom.conf'
EOF
cat > /var/lib/postgresql/data/pgdata/custom.conf << EOF
unix_socket_directories = '/controller/run'
EOF

# The magic happens here
pg_upgrade --link \
    --old-bindir "$(cat /controller/old-bindir.txt)" \
    --old-datadir /var/lib/postgresql/data/pgdata \
    --new-datadir /var/lib/postgresql/data/new

# We don't need the delete_ols_cluster.sh script because we swap it with the new one'
rm -f /var/lib/postgresql/data/new/delete_old_cluster.sh

# Clean up the old pgdata
cd /var/lib/postgresql/data/pgdata/
find . -depth ! -path . ! -path ./pg_wal -delete
find pg_wal/ -depth ! -path pg_wal/ -delete

# Move the new pgdata in place
mv /var/lib/postgresql/data/new/pg_wal/*  pg_wal/
rmdir /var/lib/postgresql/data/new/pg_wal
mv /var/lib/postgresql/data/new/* .
rmdir /var/lib/postgresql/data/new/

# Cleanup the previous version directory from tablespaces
rm -fr /var/lib/postgresql/data/pgdata/pg_tblspc/*/PG_${version}_*/
`,
	}
	job := createPrimaryJob(*cluster, nodeSerial, jobMajorUpgrade, upgradeCommand)

	oldVersionInitContainer := corev1.Container{
		Name:            "old",
		Image:           oldImage,
		ImagePullPolicy: cluster.Spec.ImagePullPolicy,
		Command:         initContainerCommand,
		VolumeMounts:    createPostgresVolumeMounts(*cluster),
		Resources:       cluster.Spec.Resources,
		SecurityContext: CreateContainerSecurityContext(cluster.GetSeccompProfile()),
	}
	job.Spec.Template.Spec.InitContainers = append(job.Spec.Template.Spec.InitContainers, oldVersionInitContainer)

	return job
}
