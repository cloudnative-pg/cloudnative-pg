variable "region" {
  default = "eu-central-1"
}

variable "instance_type" {
  default = "m5.large"
}

variable "cluster_id" {
  default = "rke"
}

variable "docker_install_url" {
  default = "https://releases.rancher.com/install-docker/19.03.sh"
}

variable "k8s_version" {
  default = "v1.24.2-rancher1-1"
}

variable "cluster_name" {
  default = "rke"
}

variable "source_ip_address" {
  default = ""
}
