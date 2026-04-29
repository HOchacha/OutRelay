# modules/control-host
#
# A single EC2 instance running both outrelay-controller and
# outrelay-relay as separate systemd units. They share localhost
# (relay → controller is 127.0.0.1:7444) so we don't pay for
# cross-instance gRPC. The relay listens on 0.0.0.0:7443/udp;
# agents in the other VPCs dial it via this host's EIP.

# ---- IAM: SSM core + scoped S3 fetch -----------------------------------

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
  name               = "outrelay-smoke-${var.run_id}-control"
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
  name = "outrelay-smoke-${var.run_id}-control"
  role = aws_iam_role.this.name
  tags = var.common_tags
}

# ---- Security group ----------------------------------------------------
#
# Inbound: only the relay's QUIC port from any agent (UDP 7443 from
# 0.0.0.0/0). Controller's gRPC port is intentionally NOT exposed —
# the relay reaches it via 127.0.0.1, no remote access.
# Outbound: open (SSM and updates).

resource "aws_security_group" "this" {
  name        = "outrelay-smoke-${var.run_id}-control"
  description = "controller + relay host"
  vpc_id      = var.vpc_id

  ingress {
    description      = "QUIC from agents"
    from_port        = 7443
    to_port          = 7443
    protocol         = "udp"
    cidr_blocks      = ["0.0.0.0/0"]
    ipv6_cidr_blocks = ["::/0"]
  }

  dynamic "ingress" {
    for_each = var.enable_tcp_fallback ? [1] : []
    content {
      description      = "TCP+TLS+yamux fallback (UDP-blocked clients)"
      from_port        = 443
      to_port          = 443
      protocol         = "tcp"
      cidr_blocks      = ["0.0.0.0/0"]
      ipv6_cidr_blocks = ["::/0"]
    }
  }

  dynamic "ingress" {
    for_each = var.expose_controller ? [1] : []
    content {
      description      = "Controller gRPC for cross-region peer relays"
      from_port        = 7444
      to_port          = 7444
      protocol         = "tcp"
      cidr_blocks      = ["0.0.0.0/0"]
      ipv6_cidr_blocks = ["::/0"]
    }
  }

  dynamic "ingress" {
    for_each = var.enable_forward_plane ? [1] : []
    content {
      description      = "Mini-TURN forward plane (relay_mode=FORWARD)"
      from_port        = 9443
      to_port          = 9443
      protocol         = "udp"
      cidr_blocks      = ["0.0.0.0/0"]
      ipv6_cidr_blocks = ["::/0"]
    }
  }

  egress {
    from_port   = 0
    to_port     = 0
    protocol    = "-1"
    cidr_blocks = ["0.0.0.0/0"]
  }

  tags = merge(var.common_tags, { Name = "outrelay-smoke-${var.run_id}-control-sg" })
}

# ---- EIP for the relay endpoint advertised to agents -------------------
#
# We allocate the EIP first and bake it into user_data so the relay's
# --advertise flag is correct from the very first boot. This avoids a
# second-pass apply or runtime patching.

resource "aws_eip" "this" {
  domain = "vpc"
  tags   = merge(var.common_tags, { Name = "outrelay-smoke-${var.run_id}-control-eip" })
}

# ---- The instance ------------------------------------------------------

resource "aws_instance" "this" {
  ami                    = var.ami_id
  instance_type          = var.instance_type
  subnet_id              = var.subnet_id
  vpc_security_group_ids = [aws_security_group.this.id]
  iam_instance_profile   = aws_iam_instance_profile.this.name

  user_data = templatefile("${path.module}/cloud-init.tftpl", {
    bucket_name          = var.bucket_name
    advertise            = "${aws_eip.this.public_ip}:7443"
    tenant               = var.tenant
    relay_id             = var.relay_id
    enable_tcp_fallback  = var.enable_tcp_fallback
    expose_controller    = var.expose_controller
    enable_forward_plane = var.enable_forward_plane
  })

  # If user_data changes (e.g. relay flag tweak between runs) we
  # want the instance recreated, not just rebooted.
  user_data_replace_on_change = true

  metadata_options {
    http_tokens = "required"
  }

  tags = merge(var.common_tags, { Name = "outrelay-smoke-${var.run_id}-control" })
}

resource "aws_eip_association" "this" {
  instance_id   = aws_instance.this.id
  allocation_id = aws_eip.this.id
}
