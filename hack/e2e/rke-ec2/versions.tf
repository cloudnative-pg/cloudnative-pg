terraform {
  required_version = ">= 0.13"
  required_providers {
    rke = {
      source  = "rancher/rke"
      version = "1.4.0"
    }
  }
}
