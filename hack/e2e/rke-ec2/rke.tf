module "nodes" {
  source        = "./aws"
  region        = var.region
  instance_type = var.instance_type
  cluster_id    = var.cluster_id
  cluster_name  = var.cluster_name
  source_ip_address = var.source_ip_address
}

# Create the RKE Cluster resource
resource "rke_cluster" "cluster" {
  cloud_provider {
    name = "aws"
  }
  cluster_name       = var.cluster_name
  kubernetes_version = var.k8s_version

  nodes {
    address = module.nodes.addresses[0]
    internal_address = module.nodes.internal_ips[0]
    user    = module.nodes.ssh_username
    ssh_key = module.nodes.private_key
    role    = ["controlplane", "etcd"]
  }
  nodes {
    address = module.nodes.addresses[1]
    internal_address = module.nodes.internal_ips[1]
    user    = module.nodes.ssh_username
    ssh_key = module.nodes.private_key
    role    = ["worker"]
  }
  nodes {
    address = module.nodes.addresses[2]
    internal_address = module.nodes.internal_ips[2]
    user    = module.nodes.ssh_username
    ssh_key = module.nodes.private_key
    role    = ["worker"]
  }
  nodes {
    address = module.nodes.addresses[3]
    internal_address = module.nodes.internal_ips[3]
    user    = module.nodes.ssh_username
    ssh_key = module.nodes.private_key
    role    = ["worker"]
  }
  services {
    kubeproxy {
      extra_args = {
        proxy-mode = "ipvs"
        masquerade-all = "true"
        ipvs-min-sync-period = "0s"
        ipvs-sync-period = "1s"
      }
    }
  }
  ingress {
    provider = "none"
  }
  addons_include = [
      "${path.root}/../kind-fluentd.yaml",
      "${path.root}/storage-class.yaml",
    ]
}

# Create the Kubeconfigfile to access the RKE cluster
resource "local_file" "kube_cluster_yaml" {
  filename = "./kube_config_cluster.yml"
  content  = rke_cluster.cluster.kube_config_yaml
}

# private_key to access the instances via SSH
output "id_rsa" {
  value = module.nodes.private_key
  sensitive   = true
}

# public_dns of the instances
output "public_dns" {
  value = module.nodes.addresses
}
