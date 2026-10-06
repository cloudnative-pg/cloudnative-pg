# Setting up Google Cloud for the E2E tests

This guide prepares your own Google Cloud project so that the `gke` E2E job
can run on your fork.

The job creates a real cluster and costs money. Use a project that is only for
the tests, and set a budget alert.

## Before you start

You need `gcloud` and `gh`, both logged in, and a Google Cloud project with
billing enabled where you have the Owner role. Run all commands in one
shell: later steps use variables from earlier ones.

## What the tests create

The `gke` job creates a regional GKE cluster with one node per zone (3 nodes,
`e2-standard-4` by default, 80 GB disks). It is deleted at the end of the job.

Check the quotas of the region you use (`GKE_REGION`, `europe-west4` by
default), including `E2 CPUs`.

## 1. Enable the APIs

```bash
export PROJECT_ID=<your-project-id>

gcloud services enable --project=$PROJECT_ID \
  compute.googleapis.com container.googleapis.com \
  cloudresourcemanager.googleapis.com iam.googleapis.com \
  iamcredentials.googleapis.com serviceusage.googleapis.com
```

## 2. Create the service account

The workflows act as this service account.

```bash
export SA_EMAIL=e2e-tests@$PROJECT_ID.iam.gserviceaccount.com

gcloud iam service-accounts create e2e-tests --project=$PROJECT_ID \
  --display-name="E2E tests"

for role in roles/container.admin roles/compute.admin \
  roles/iam.serviceAccountUser; do
  gcloud projects add-iam-policy-binding $PROJECT_ID \
    --member="serviceAccount:$SA_EMAIL" --role="$role" --condition=None
done
```

The job creates and deletes GKE clusters and disks, and the cluster nodes run
as a service account, so the account needs the Kubernetes Engine, Compute
Engine and service account permissions. These roles are a working example,
not a minimal set.

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

## 4. Set the repository variables

Set these under *Settings, Secrets and variables, Actions* of your fork.
Variables:

- `GKE_ENABLED`: set it to `true` to enable the `gke` job.
- `GKE_REGION` and `GKE_MACHINE_TYPE`: optional, they default to
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

For example:

```bash
gh variable set GKE_ENABLED --body true -R $GITHUB_REPO
gh secret set GCP_PROJECT_ID --body $PROJECT_ID -R $GITHUB_REPO
gh variable set GCP_SERVICE_ACCOUNT --body $SA_EMAIL -R $GITHUB_REPO
gh variable set GCP_WORKLOAD_IDENTITY_PROVIDER -R $GITHUB_REPO \
  --body "projects/$PROJECT_NUMBER/locations/global/workloadIdentityPools/github-actions-pool/providers/github-provider"
```

## 5. Run a test

In a pull request of your fork, as a user with `write` permission:

```text
/test limit=gke type=smoke d=pull_request
```

Read the code of a pull request before you comment `/test`: the tests run it
with your cloud credentials.

The `push` depth creates no cloud jobs. You can also start the
`continuous-delivery` workflow by hand from the Actions tab, with the same
inputs.

## 6. After a run

The job deletes its cluster at the end, also when tests fail. After an
interrupted run, or when a job is cancelled, look for leftovers and delete
them, they cost money:

```bash
gcloud container clusters list --project=$PROJECT_ID
gcloud compute instances list --project=$PROJECT_ID
gcloud compute disks list --project=$PROJECT_ID
```

Sometimes a disk named `pvc-...` stays behind after a GKE cluster is deleted.
Delete it.
