# Setting up Google Cloud for the E2E tests

This guide prepares your own Google Cloud project so that the `gke` and
`openshift` E2E jobs can run on your fork.

The jobs create real clusters and cost money. Use a project that is only for
the tests, and set a budget alert.

## Before you start

You need `gcloud` and `gh`, both logged in, and a Google Cloud project with
billing enabled where you have the Owner role. Run all commands in one
shell: later steps use variables from earlier ones.

## What the tests create

- `gke`: a regional GKE cluster with one node per zone (3 nodes,
  `e2-standard-4` by default, 80 GB disks). It is deleted at the end of the
  job.
- `openshift`: an OpenShift cluster with 3 control plane and 3 worker VMs
  (`e2-standard-8`), plus a bootstrap VM that is deleted after the
  installation. At peak that is 7 VMs and 56 vCPUs. It also creates load
  balancers, firewall rules, service accounts and a private DNS zone, and uses
  the public DNS zone of your domain.

Check the quotas of the region you use (`GCP_REGION`, `europe-west4` by
default), including `E2 CPUs`. A new project often has too few vCPUs, SSD
space and static addresses for OpenShift. Red Hat lists what a default
cluster needs in
[Google Cloud account limits](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/installing_on_google_cloud/installing-gcp-account#installation-gcp-limits_installing-gcp-account).
The machine types used here are larger than the defaults in that table.

## 1. Enable the APIs

```bash
export PROJECT_ID=<your-project-id>

gcloud services enable --project=$PROJECT_ID \
  compute.googleapis.com container.googleapis.com \
  cloudresourcemanager.googleapis.com dns.googleapis.com \
  iam.googleapis.com iamcredentials.googleapis.com \
  serviceusage.googleapis.com
```

`container.googleapis.com` is only needed for GKE. The others are the APIs
that the OpenShift installer requires, see
[Enabling API services](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/installing_on_google_cloud/installing-gcp-account#installation-gcp-enabling-api-services_installing-gcp-account).

## 2. Create the service account

The workflows act as this service account.

```bash
export SA_EMAIL=e2e-tests@$PROJECT_ID.iam.gserviceaccount.com

gcloud iam service-accounts create e2e-tests --project=$PROJECT_ID \
  --display-name="E2E tests"

for role in \
  roles/compute.admin roles/container.admin roles/dns.admin \
  roles/storage.admin roles/iam.securityAdmin roles/iam.roleAdmin \
  roles/iam.serviceAccountAdmin roles/iam.serviceAccountKeyAdmin \
  roles/iam.serviceAccountUser roles/iam.serviceAccountTokenCreator \
  roles/serviceusage.serviceUsageAdmin roles/servicemanagement.admin \
  roles/servicemanagement.quotaAdmin roles/cloudquotas.admin \
  roles/orgpolicy.policyViewer; do
  gcloud projects add-iam-policy-binding $PROJECT_ID \
    --member="serviceAccount:$SA_EMAIL" --role="$role" --condition=None
done
```

This set is tested with both jobs. The GKE job needs the Kubernetes Engine,
Compute Engine and service account permissions. The OpenShift installer needs
the rest: it creates custom roles and service accounts, checks quotas and
reads organization policies, and it signs a URL as the service account, which
needs `serviceAccountTokenCreator`. Do not trim the set for OpenShift.
These roles let the account grant itself any permission in the project, so
the project must hold nothing else.

## 3. Let GitHub Actions use the service account

Workload Identity Federation lets a workflow get short-lived credentials for
the service account, so no key is stored in GitHub.

```bash
export GITHUB_OWNER=<your-github-user-or-organization>
export GITHUB_REPO=$GITHUB_OWNER/<your-fork-name>
export PROJECT_NUMBER=$(gcloud projects describe $PROJECT_ID --format="value(projectNumber)")

gcloud iam workload-identity-pools create github-actions-pool \
  --project=$PROJECT_ID --location=global --display-name="GitHub Actions"

gcloud iam workload-identity-pools providers create-oidc github-provider \
  --project=$PROJECT_ID --location=global \
  --workload-identity-pool=github-actions-pool \
  --display-name="GitHub OIDC" \
  --issuer-uri="https://token.actions.githubusercontent.com" \
  --attribute-mapping="google.subject=assertion.sub,attribute.actor=assertion.actor,attribute.repository=assertion.repository,attribute.repository_owner=assertion.repository_owner" \
  --attribute-condition="assertion.repository_owner == '$GITHUB_OWNER'"

gcloud iam service-accounts add-iam-policy-binding $SA_EMAIL \
  --project=$PROJECT_ID --role=roles/iam.workloadIdentityUser \
  --member="principalSet://iam.googleapis.com/projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/github-actions-pool/attribute.repository/$GITHUB_REPO"
```

The provider accepts every repository of your owner, and the binding on the
service account decides which repository may use it. To allow another
repository, run the last command again with another `GITHUB_REPO`.

The condition and the binding match names. If you delete a repository, remove
its binding too: someone else could create a repository with the same name.

Anyone who can run workflows in an allowed repository can use the
permissions of the service account. Keep the project dedicated to the tests.

Print the value for the `GCP_WORKLOAD_IDENTITY_PROVIDER` variable. It uses the
project number, not the project ID:

```bash
gcloud iam workload-identity-pools providers describe github-provider \
  --project=$PROJECT_ID --location=global \
  --workload-identity-pool=github-actions-pool --format="value(name)"
```

## 4. Create the DNS zone (OpenShift only)

OpenShift needs a public Cloud DNS zone in the same project, authoritative for
the base domain of the cluster (the cluster name is added in front of it). See
[Configuring DNS for Google Cloud](https://docs.redhat.com/en/documentation/openshift_container_platform/latest/html/installing_on_google_cloud/installing-gcp-account#installation-gcp-dns_installing-gcp-account).

```bash
gcloud dns managed-zones create e2e-openshift --project=$PROJECT_ID \
  --dns-name="<your-base-domain>." --visibility=public \
  --description="OpenShift E2E tests"

gcloud dns managed-zones describe e2e-openshift --project=$PROJECT_ID \
  --format="value(nameServers)"
```

Set those name servers at the registrar of the domain (or as `NS` records in
the parent zone, if the domain is a subdomain). Check the delegation with
`dig NS <your-base-domain>` before the first run.

## 5. Set the repository variables

Set these under *Settings, Secrets and variables, Actions* of your fork.
Variables:

- `GKE_ENABLED`: set it to `true` to enable the `gke` job.
- `GCP_REGION` and `GKE_MACHINE_TYPE`: optional, they default to
  `europe-west4` and `e2-standard-4`.
- `GCP_WORKLOAD_IDENTITY_PROVIDER`: the full name of the Workload Identity
  Provider,
  `projects/<number>/locations/global/workloadIdentityPools/<pool>/providers/<provider>`.
- `GCP_SERVICE_ACCOUNT`: the email of the service account to impersonate.

Secrets:

- `GCP_PROJECT_ID`: the ID of the Google Cloud project.
- `GCP_SERVICE_ACCOUNT`: the JSON key of a service account. It is only used
  when `GCP_WORKLOAD_IDENTITY_PROVIDER` is not set. It has the same name as
  the variable above but holds something else. Never put the key in the
  variable, variables are not encrypted.

For the `openshift` job on Google Cloud also set:

- `OPENSHIFT_ENABLED`: set it to `true` to enable the job.
- `OPENSHIFT_PLATFORM`: set it to `gcp`. The default is `aws`.
- `OPENSHIFT_BASE_DOMAIN`: the base domain, the domain of the public Cloud
  DNS zone.
- the `REDHAT_PULL` secret with your Red Hat pull secret.
- `OPENSHIFT_SSH_PUBLIC_KEY`: optional, a public SSH key to install on the
  nodes, to debug an installation over SSH.

The `openshift` job does not need `GKE_ENABLED`, but it reuses the project,
`GCP_REGION` and the authentication settings of the `gke` job.

For example:

```bash
gh variable set GKE_ENABLED --body true -R $GITHUB_REPO
gh secret set GCP_PROJECT_ID --body $PROJECT_ID -R $GITHUB_REPO
gh variable set GCP_SERVICE_ACCOUNT --body $SA_EMAIL -R $GITHUB_REPO
gh variable set GCP_WORKLOAD_IDENTITY_PROVIDER -R $GITHUB_REPO \
  --body "projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/github-actions-pool/providers/github-provider"
```

For the `openshift` job also:

```bash
gh variable set OPENSHIFT_ENABLED --body true -R $GITHUB_REPO
gh variable set OPENSHIFT_PLATFORM --body gcp -R $GITHUB_REPO
gh variable set OPENSHIFT_BASE_DOMAIN --body <your-base-domain> -R $GITHUB_REPO
gh secret set REDHAT_PULL < pull-secret.txt -R $GITHUB_REPO
```

## 6. Run a test

In a pull request of your fork, as a user with `write` permission:

```text
/test limit=gke type=smoke d=pull_request
/test limit=openshift type=smoke d=pull_request
```

Read the code of a pull request before you comment `/test`: the tests run it
with your cloud credentials.

The `push` depth creates no cloud jobs. You can also start the
`continuous-delivery` workflow by hand from the Actions tab, with the same
inputs.

## 7. After a run

A job deletes its cluster at the end, also when tests fail. After an
interrupted run, or when a job is cancelled, look for leftovers and delete
them, they cost money:

```bash
gcloud container clusters list --project=$PROJECT_ID
gcloud compute instances list --project=$PROJECT_ID
gcloud compute disks list --project=$PROJECT_ID
gcloud compute forwarding-rules list --project=$PROJECT_ID
gcloud compute addresses list --project=$PROJECT_ID
gcloud dns managed-zones list --project=$PROJECT_ID
```

Sometimes a disk named `pvc-...` stays behind after a GKE cluster is deleted.
Delete it. An OpenShift cluster is named after the infrastructure ID of its
installation, delete everything that carries it.
