variable "instance_type" {
  default = "t3.medium"
}

resource "aws_instance" "web" {
  instance_type = var.instance_type
  root_block_device {
    volume_size = 30
  }
}
