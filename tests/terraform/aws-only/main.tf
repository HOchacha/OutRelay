# aws-only/
#
# Smoke topology for the configurations that don't need GCP. Four
# VPCs, four EC2 instances, two NAT GWs, one EIP for the relay,
# one EIP for the P2P-positive provider.
#
# +-----------------------------+        +------------------------------+
# | VPC control       (public)  |        | VPC provider-eip   (public)  |
# |   EC2: controller + relay   |        |   EC2: agent (provider role) |
# |        EIP, SG: 7443/udp    |        |        EIP, SG: udp/1024-... |
# +-----------------------------+        +------------------------------+
#
# +-----------------------------+        +------------------------------+
# | VPC provider-nat            |        | VPC consumer                 |
# |   private subnet + NAT GW   |        |   private subnet + NAT GW    |
# |   EC2: agent (provider)     |        |   EC2: agent (consumer)      |
# |        no inbound rules     |        |        no inbound rules      |
# +-----------------------------+        +------------------------------+

# Frozen at apply time so expires-at doesn't drift on every refresh
# and so the cleanup-stale safety net can compare numbers reliably.
resource "time_static" "now" {}

locals {
  azs = ["${var.aws_region}a", "${var.aws_region}c"]

  # Unix seconds at which this run's resources are considered
  # expired. cleanup-stale.sh deletes anything with expires-at < now.
  expires_at_unix = time_static.now.unix + var.expires_after_hours * 3600

  common_tags = {
    owner        = var.owner_tag
    "run-id"     = var.run_id
    "expires-at" = tostring(local.expires_at_unix)
  }
}

# Latest Amazon Linux 2023 AMI for x86_64. SSM Parameter Store is
# the current best practice — no AMI lookups by tag-pattern.
data "aws_ssm_parameter" "al2023" {
  name = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64"
}

# ---- Per-run artifact bucket ------------------------------------------

module "artifacts" {
  source = "../modules/artifact-bucket"

  run_id        = var.run_id
  artifacts_dir = var.artifacts_dir
  common_tags   = local.common_tags
}

# ---- Four VPCs ---------------------------------------------------------
# CIDRs intentionally non-overlapping (we don't peer them, but the
# discipline catches mistakes early) and small (/24 is plenty —
# we provision four IPs total).

module "vpc_control" {
  source = "../modules/vpc"

  name        = "control"
  cidr        = "10.10.0.0/24"
  azs         = local.azs
  common_tags = local.common_tags
}

module "vpc_provider_eip" {
  source = "../modules/vpc"

  name        = "provider-eip"
  cidr        = "10.20.0.0/24"
  azs         = local.azs
  common_tags = local.common_tags
}

module "vpc_provider_nat" {
  source = "../modules/vpc"

  name             = "provider-nat"
  cidr             = "10.30.0.0/24"
  azs              = local.azs
  with_nat_gateway = true
  common_tags      = local.common_tags
}

module "vpc_consumer" {
  source = "../modules/vpc"

  name             = "consumer"
  cidr             = "10.40.0.0/24"
  azs              = local.azs
  with_nat_gateway = true
  common_tags      = local.common_tags
}

# ---- Control host (controller + relay) --------------------------------

module "control_host" {
  source = "../modules/control-host"

  run_id               = var.run_id
  vpc_id               = module.vpc_control.vpc_id
  subnet_id            = module.vpc_control.public_subnet_id
  ami_id               = data.aws_ssm_parameter.al2023.value
  bucket_name          = module.artifacts.bucket_name
  fetch_policy_arn     = module.artifacts.fetch_policy_arn
  enable_tcp_fallback  = true
  enable_forward_plane = true
  common_tags          = local.common_tags
}

# ---- Three agents ------------------------------------------------------
# All UUIDs match smoke-pki's hardcoded constants.

module "agent_provider_eip" {
  source = "../modules/agent-host"

  run_id             = var.run_id
  role               = "provider-eip"
  vpc_id             = module.vpc_provider_eip.vpc_id
  subnet_id          = module.vpc_provider_eip.public_subnet_id
  ami_id             = data.aws_ssm_parameter.al2023.value
  bucket_name        = module.artifacts.bucket_name
  fetch_policy_arn   = module.artifacts.fetch_policy_arn
  relay_endpoint     = module.control_host.relay_endpoint
  agent_uuid         = "00000000-0000-0000-0000-000000000001"
  cert_prefix        = "agent-provider-eip"
  agent_mode         = "provider"
  service_name       = "svc-eip"
  attach_eip         = true
  allow_inbound_quic = true
  common_tags        = local.common_tags
}

module "agent_provider_nat" {
  source = "../modules/agent-host"

  run_id             = var.run_id
  role               = "provider-nat"
  vpc_id             = module.vpc_provider_nat.vpc_id
  subnet_id          = module.vpc_provider_nat.private_subnet_id
  ami_id             = data.aws_ssm_parameter.al2023.value
  bucket_name        = module.artifacts.bucket_name
  fetch_policy_arn   = module.artifacts.fetch_policy_arn
  relay_endpoint     = module.control_host.relay_endpoint
  agent_uuid         = "00000000-0000-0000-0000-000000000003"
  cert_prefix        = "agent-provider-nat"
  agent_mode         = "provider"
  service_name       = "svc-nat"
  attach_eip         = false
  allow_inbound_quic = false
  common_tags        = local.common_tags
}

module "agent_consumer" {
  source = "../modules/agent-host"

  run_id             = var.run_id
  role               = "consumer"
  vpc_id             = module.vpc_consumer.vpc_id
  subnet_id          = module.vpc_consumer.private_subnet_id
  ami_id             = data.aws_ssm_parameter.al2023.value
  bucket_name        = module.artifacts.bucket_name
  fetch_policy_arn   = module.artifacts.fetch_policy_arn
  relay_endpoint     = module.control_host.relay_endpoint
  agent_uuid         = "00000000-0000-0000-0000-000000000002"
  cert_prefix        = "agent-consumer"
  agent_mode         = "consumer"
  service_name       = "svc-eip"
  consume_bind_addr  = "127.0.0.1:30001"
  extra_consumes     = ["svc-nat@127.0.0.1:30002"]
  attach_eip         = false
  allow_inbound_quic = false
  common_tags        = local.common_tags
}

# A second consumer that exercises the L2.5 TCP+TLS fallback path:
# --relay points at a guaranteed-to-fail .invalid hostname (DNS
# NXDOMAIN per RFC 6761) so the QUIC dial errors out fast, and
# --relay-tcp is the real relay's TCP/443 endpoint. Asserts the
# agent connects via TCP and the relay roundtrip still works.
module "agent_consumer_tcp" {
  source = "../modules/agent-host"

  run_id             = var.run_id
  role               = "consumer-tcp"
  vpc_id             = module.vpc_consumer.vpc_id
  subnet_id          = module.vpc_consumer.private_subnet_id
  ami_id             = data.aws_ssm_parameter.al2023.value
  bucket_name        = module.artifacts.bucket_name
  fetch_policy_arn   = module.artifacts.fetch_policy_arn
  relay_endpoint     = "tcp-fallback-test.invalid:9999"
  relay_tcp_endpoint = module.control_host.relay_tcp_endpoint
  agent_uuid         = "00000000-0000-0000-0000-000000000005"
  cert_prefix        = "agent-consumer-tcp"
  agent_mode         = "consumer"
  service_name       = "svc-nat"
  consume_bind_addr  = "127.0.0.1:30002"
  attach_eip         = false
  allow_inbound_quic = false
  common_tags        = local.common_tags
}
