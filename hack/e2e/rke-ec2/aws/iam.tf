# Create an Instance Profile resource associated to the rke-role
resource "aws_iam_instance_profile" "rke-aws" {
  name = var.cluster_name
  role = "rke-role"
}
