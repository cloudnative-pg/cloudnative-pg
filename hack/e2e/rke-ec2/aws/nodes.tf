locals {
  cluster_id_tag = {
    "kubernetes.io/cluster/${var.cluster_id}" = "owned"
  }
}

# Set the RKE VPC
data "aws_vpc" "selected" {
  id = "vpc-02a89ab0a414b1d8e"
}

# Set the RKE Subnet
data "aws_subnet" "selected" {
  filter {
    name   = "tag:Name"
    values = ["rke-subnet"]
  }
}

# Create 4 network Ifaces inside the rke-subnet
resource "aws_network_interface" "eni" {
  subnet_id       = data.aws_subnet.selected.id
  count           = 4
  security_groups = [aws_security_group.allow-all.id]
}

resource "aws_security_group" "allow-all" {
  name        = "${var.cluster_name}-security-group"
  description = "rke"
  vpc_id      = data.aws_vpc.selected.id

  ingress {
    from_port   = 22
    to_port     = 22
    protocol    = "tcp"
    cidr_blocks = ["${var.source_ip_address}/32"]
  }

  ingress {
    from_port   = 6443
    to_port     = 6443
    protocol    = "tcp"
    cidr_blocks = ["${var.source_ip_address}/32"]
  }

  ingress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["10.0.0.0/16"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = local.cluster_id_tag
}

# Create 4 EC2 instances
resource "aws_instance" "rke-node" {
  count = 4
  ami                    = data.aws_ami.ubuntu.id
  instance_type          = var.instance_type
  key_name               = aws_key_pair.rke-node-key.id
  iam_instance_profile   = aws_iam_instance_profile.rke-aws.name

  network_interface {
    network_interface_id = aws_network_interface.eni[count.index].id
    device_index         = 0
  }

  root_block_device {
    volume_size = 25
  }

  tags                   = {
    "kubernetes.io/cluster/${var.cluster_id}" = "owned"
    Name = "${var.cluster_name}-${count.index}"
  }

  provisioner "remote-exec" {
    connection {
      host        = coalesce(self.public_ip, self.private_ip)
      type        = "ssh"
      user        = "ubuntu"
      private_key = tls_private_key.node-key.private_key_pem
    }

    inline = [
      "sleep 10",
      "curl ${var.docker_install_url} | sh",
      "sudo usermod -a -G docker ubuntu",
    ]
  }
}
