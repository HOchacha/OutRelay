# modules/relay-host
#
# A peer relay — single outrelay-relay daemon, no controller. Used
# by aws-multi-region to deploy a second relay in a different region.
# Connects to the (single) controller in the primary region via a
# public TCP/7444 listener.

data "aws_iam_policy_document" "assume" {
  statement {
    actions = ["sts:AssumeRole"]
    principals {
      type        = "Service"
      identifiers = ["ec2.amazonaws.com"]
    }
  }
}

resource "aws_iam_role" "this" {
  name               = "outrelay-smoke-${var.run_id}-relay-${var.relay_id}"
  assume_role_policy = data.aws_iam_policy_document.assume.json
  tags               = var.common_tags
}

resource "aws_iam_role_policy_attachment" "ssm" {
  role       = aws_iam_role.this.name
  policy_arn = "arn:aws:iam::aws:policy/AmazonSSMManagedInstanceCore"
}

resource "aws_iam_role_policy_attachment" "fetch" {
  role       = aws_iam_role.this.name
  policy_arn = var.fetch_policy_arn
}

resource "aws_iam_instance_profile" "this" {
  name = "outrelay-smoke-${var.run_id}-relay-${var.relay_id}"
  role = aws_iam_role.this.name
  tags = var.common_tags
}

resource "aws_security_group" "this" {
  name        = "outrelay-smoke-${var.run_id}-relay-${var.relay_id}"
  description = "peer relay (id=${var.relay_id})"
  vpc_id      = var.vpc_id

  # One UDP/7443 rule covers both agent QUIC and inter-relay QUIC —
  # they share the same listener.
  ingress {
    description      = "QUIC from agents and peer relays"
    from_port        = 7443
    to_port          = 7443
    protocol         = "udp"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.common_tags, { Name = "outrelay-smoke-${var.run_id}-relay-${var.relay_id}-sg" })
}

resource "aws_eip" "this" {
  domain = "vpc"
  tags   = merge(var.common_tags, { Name = "outrelay-smoke-${var.run_id}-relay-${var.relay_id}-eip" })
}

resource "aws_instance" "this" {
  ami                    = var.ami_id
  instance_type          = var.instance_type
  subnet_id              = var.subnet_id
  vpc_security_group_ids = [aws_security_group.this.id]
  iam_instance_profile   = aws_iam_instance_profile.this.name

  user_data = templatefile("${path.module}/cloud-init.tftpl", {
    bucket_name         = var.bucket_name
    bucket_region       = var.bucket_region
    relay_id            = var.relay_id
    region_label        = var.region_label
    controller_endpoint = var.controller_endpoint
    advertise           = "${aws_eip.this.public_ip}:7443"
    tenant              = var.tenant
  })
  user_data_replace_on_change = true

  metadata_options {
    http_tokens = "required"
  }

  tags = merge(var.common_tags, { Name = "outrelay-smoke-${var.run_id}-relay-${var.relay_id}" })
}

resource "aws_eip_association" "this" {
  instance_id   = aws_instance.this.id
  allocation_id = aws_eip.this.id
}
