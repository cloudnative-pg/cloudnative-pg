resource "tls_private_key" "node-key" {
  algorithm = "RSA"
}

# Create a pair of unique keys (associated to the cluster)
# to allow ssh authentication
resource "aws_key_pair" "rke-node-key" {
  key_name   = var.cluster_name
  public_key = tls_private_key.node-key.public_key_openssh
}
