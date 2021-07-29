# Create an RKE Cluster on EC2 instances

Based on https://github.com/rancher/terraform-provider-rke.

This Terraform project is meant to be used by the E2E continuos-delivery pipeline
to setup an RKE cluster on EC2.
This code works with the assumption that the `cluster_name` variable is a
unique value, which means that there can't exist multiple clusters on the
`edb-cloudnative` IAM USER using the same `cluster_name`.

The CD pipepline generates a unique `cluster_name` by using a combination of the
`github action run_number` and the ID fields of the json returned by
the `e2e-matrix-generator`.

This project can be used to setup an RKS cluster on EC2 using the `edb-cloudnative`
IAM USER, just remember to set a unique `cluster_name` otherwise your Terraform
deploy will fail saying that certain resources are already existing.

How to use it:

Install terraform CLI on local machine by following the document https://learn.hashicorp.com/tutorials/terraform/install-cli
based on your operating system.

```
# Change directory
cd to hack/e2e/rke-ec2

# AWS credentials
export AWS_ACCESS_KEY_ID="<your-access-key>"
export AWS_SECRET_ACCESS_KEY="<your-secret-key>"

# Cluster variables
export TF_VAR_cluster_name="<your-unique-cluster-name>"
export TF_VAR_source_ip_address="<my-public-ip-address>"

# Deploy
terraform init && terraform apply -auto-approve

# Export kubeconfig file
export KUBECONFIG=${PWD}/kube_config_cluster.yml

# Access the K8S cluster using kubectl
```

# Logs
RKE logs are produced automatically in `rke_debug.log` inside the workspace.

More verbose Terraform logs can be retrieved by exporting:
```
export TF_LOG=TRACE
export TF_LOG_PATH=terraform.log
```

# Other variable customization

Use a specific K8s version:

```
Select a compatible k8s version from the ones listed under the
terraform-provider-rke release being currently used (v1.2.3 as now):
https://github.com/rancher/terraform-provider-rke/releases/tag/v1.2.3.

# Export the version
export TF_VAR_k8s_version=v1.19.10-rancher1-1
```

# AWS Resources
Pre-Existing:
* a VPC named `rke`
* a subnet named `rke-subnet` created in AZ=`eu-central-1a` (part of the `rke` VPC)
* an IAM policy named `rke-access-policy`
* an IAM role named `rke-role` (attached to the `rke-access-policy`)

Generated:
* an IAM instance profile named as `$cluster_name`
* an AWS key pair named as `$cluster_name`
* Network interfaces for the instances
* EC2 instances named `$cluster_name-$index_count`
* a security group named `$cluster_name-security-group`
