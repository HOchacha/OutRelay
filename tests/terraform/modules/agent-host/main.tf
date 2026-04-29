# modules/agent-host
#
# A single EC2 instance running outrelay-agent in either provider or
# consumer mode. The same module is invoked once per agent in the
# topology — variation between agents is parameterised, not forked.
#
# Provider hosts also run a tiny python3 echo HTTP server on
# 127.0.0.1:8080; that's what the agent's --expose-target points at.

# ---- IAM ---------------------------------------------------------------

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
  name               = "outrelay-smoke-${var.run_id}-${var.role}"
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
  name = "outrelay-smoke-${var.run_id}-${var.role}"
  role = aws_iam_role.this.name
  tags = var.common_tags
}

# ---- Security group ----------------------------------------------------
#
# Outbound is always open. Inbound is conditional:
#   allow_inbound_quic = true  → UDP 1024-65535 from 0.0.0.0/0
#       Required for the P2P-positive provider so that the consumer
#       can dial the host candidate. The agent's QUIC socket is the
#       same one used for the relay connection, with an OS-chosen
#       ephemeral port — we can't pin it, hence the wide range.
#   allow_inbound_quic = false → no inbound rules at all
#       This is what makes §3.2 outbound-only meaningful: the agent
#       still works through the relay despite literally no ingress.

resource "aws_security_group" "this" {
  name        = "outrelay-smoke-${var.run_id}-${var.role}"
  description = "agent: ${var.role}"
  vpc_id      = var.vpc_id

  dynamic "ingress" {
    for_each = var.allow_inbound_quic ? [1] : []
    content {
      description      = "P2P direct dial -- required when this host is a P2P-positive provider"
      from_port        = 1024
      to_port          = 65535
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

  tags = merge(var.common_tags, { Name = "outrelay-smoke-${var.run_id}-${var.role}-sg" })
}

# ---- Optional EIP ------------------------------------------------------

resource "aws_eip" "this" {
  count  = var.attach_eip ? 1 : 0
  domain = "vpc"
  tags   = merge(var.common_tags, { Name = "outrelay-smoke-${var.run_id}-${var.role}-eip" })
}

# ---- Instance ----------------------------------------------------------

resource "aws_instance" "this" {
  ami                    = var.ami_id
  instance_type          = var.instance_type
  subnet_id              = var.subnet_id
  vpc_security_group_ids = [aws_security_group.this.id]
  iam_instance_profile   = aws_iam_instance_profile.this.name

  user_data = templatefile("${path.module}/cloud-init.tftpl", {
    bucket_name        = var.bucket_name
    bucket_region      = var.bucket_region
    role               = var.role
    relay_endpoint     = var.relay_endpoint
    relay_tcp_endpoint = var.relay_tcp_endpoint
    tenant             = var.tenant
    agent_uuid         = var.agent_uuid
    cert_prefix        = var.cert_prefix
    agent_mode         = var.agent_mode
    service_name       = var.service_name
    consume_bind_addr  = var.consume_bind_addr
    extra_consumes     = var.extra_consumes
    # Manual --p2p-advertise override; left empty so cloud-init's
    # template `if` skips the flag and the agent relies on srflx
    # auto-discovery. Set this to "<ip>:7445" if you ever need to
    # bypass the relay-mediated discovery (e.g. running outside
    # AWS where srflx returns something unexpected).
    p2p_advertise = ""
  })
  user_data_replace_on_change = true

  metadata_options {
    http_tokens = "required"
  }

  tags = merge(var.common_tags, { Name = "outrelay-smoke-${var.run_id}-${var.role}" })
}

resource "aws_eip_association" "this" {
  count         = var.attach_eip ? 1 : 0
  instance_id   = aws_instance.this.id
  allocation_id = aws_eip.this[0].id
}
