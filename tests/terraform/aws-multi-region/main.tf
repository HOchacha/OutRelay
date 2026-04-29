# aws-multi-region/
#
# Validates the inter-relay forwarding path on real cross-region
# infrastructure: a single controller in the primary region, two
# relays (r1 in primary, r2 in secondary), and a consumer/provider
# pair split across regions. The provider registers svc-remote with
# relay-r2; the consumer's --relay list contains both relay
# endpoints and DialAnyHappy picks the lower-RTT one (r1, same
# region). When the consumer opens a stream, relay-r1 calls the
# controller's Resolve, gets back a provider whose relay_id is r2,
# returns ErrProviderRemote, and forwards via intra.Pool over the
# inter-relay QUIC link to r2 — which then splices to the provider.
#
# +-------------------------+        +-------------------------+
# |  Region A (primary)     |        |  Region B (secondary)   |
# |                         |        |                         |
# |  control_host           |        |  relay_host             |
# |   - controller (public) |        |   - relay-r2            |
# |   - relay-r1            |        |                         |
# |                         |        |                         |
# |  agent_consumer         |        |  agent_provider_remote  |
# |    (public IP)          |        |    (public IP)          |
# +-------------------------+        +-------------------------+
#
# No NAT GW in either region — both consumer and provider have
# public IPs to keep cost minimal. The point of this config is
# the inter-relay path, not NAT realism (already covered by
# aws-only).

resource "time_static" "now" {}

locals {
  expires_at_unix = time_static.now.unix + var.expires_after_hours * 3600

  common_tags = {
    owner        = var.owner_tag
    "run-id"     = var.run_id
    "expires-at" = tostring(local.expires_at_unix)
  }

  azs_primary   = ["${var.aws_region}a", "${var.aws_region}c"]
  azs_secondary = ["${var.aws_region_secondary}a", "${var.aws_region_secondary}b"]
}

data "aws_ssm_parameter" "al2023_primary" {
  provider = aws.primary
  name     = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64"
}

data "aws_ssm_parameter" "al2023_secondary" {
  provider = aws.secondary
  name     = "/aws/service/ami-amazon-linux-latest/al2023-ami-kernel-default-x86_64"
}

# ---- Artifact bucket (in primary region) ------------------------------

module "artifacts" {
  source = "../modules/artifact-bucket"
  providers = {
    aws = aws.primary
  }

  run_id        = var.run_id
  artifacts_dir = var.artifacts_dir
  common_tags   = local.common_tags
}

# ---- Primary region (A): controller + relay-r1 + consumer -------------

module "vpc_primary_control" {
  source = "../modules/vpc"
  providers = {
    aws = aws.primary
  }
  name        = "primary-control"
  cidr        = "10.10.0.0/24"
  azs         = local.azs_primary
  common_tags = local.common_tags
}

module "vpc_primary_consumer" {
  source = "../modules/vpc"
  providers = {
    aws = aws.primary
  }
  name        = "primary-consumer"
  cidr        = "10.20.0.0/24"
  azs         = local.azs_primary
  common_tags = local.common_tags
}

module "control_host" {
  source = "../modules/control-host"
  providers = {
    aws = aws.primary
  }

  run_id            = var.run_id
  vpc_id            = module.vpc_primary_control.vpc_id
  subnet_id         = module.vpc_primary_control.public_subnet_id
  ami_id            = data.aws_ssm_parameter.al2023_primary.value
  bucket_name       = module.artifacts.bucket_name
  fetch_policy_arn  = module.artifacts.fetch_policy_arn
  expose_controller = true
  common_tags       = local.common_tags
}

# ---- Secondary region (B): relay-r2 + provider-remote -----------------

module "vpc_secondary_relay" {
  source = "../modules/vpc"
  providers = {
    aws = aws.secondary
  }
  name        = "secondary-relay"
  cidr        = "10.30.0.0/24"
  azs         = local.azs_secondary
  common_tags = local.common_tags
}

module "vpc_secondary_provider" {
  source = "../modules/vpc"
  providers = {
    aws = aws.secondary
  }
  name        = "secondary-provider"
  cidr        = "10.40.0.0/24"
  azs         = local.azs_secondary
  common_tags = local.common_tags
}

module "relay_secondary" {
  source = "../modules/relay-host"
  providers = {
    aws = aws.secondary
  }

  run_id              = var.run_id
  vpc_id              = module.vpc_secondary_relay.vpc_id
  subnet_id           = module.vpc_secondary_relay.public_subnet_id
  ami_id              = data.aws_ssm_parameter.al2023_secondary.value
  bucket_name         = module.artifacts.bucket_name
  bucket_region       = var.aws_region
  fetch_policy_arn    = module.artifacts.fetch_policy_arn
  relay_id            = "r2"
  region_label        = var.aws_region_secondary
  controller_endpoint = module.control_host.controller_endpoint
  common_tags         = local.common_tags
}

# ---- Agents -----------------------------------------------------------

module "agent_consumer" {
  source = "../modules/agent-host"
  providers = {
    aws = aws.primary
  }

  run_id           = var.run_id
  role             = "consumer"
  vpc_id           = module.vpc_primary_consumer.vpc_id
  subnet_id        = module.vpc_primary_consumer.public_subnet_id
  ami_id           = data.aws_ssm_parameter.al2023_primary.value
  bucket_name      = module.artifacts.bucket_name
  fetch_policy_arn = module.artifacts.fetch_policy_arn

  # Consumer's --relay carries BOTH relay endpoints. DialAnyHappy
  # races the two and picks the lower-RTT one (relay-r1 in primary
  # region). The other endpoint becomes a passive failover.
  relay_endpoint = "${module.control_host.relay_endpoint},${module.relay_secondary.endpoint}"

  agent_uuid         = "00000000-0000-0000-0000-000000000002"
  cert_prefix        = "agent-consumer"
  agent_mode         = "consumer"
  service_name       = "svc-remote"
  consume_bind_addr  = "127.0.0.1:30001"
  attach_eip         = true
  allow_inbound_quic = true
  common_tags        = local.common_tags
}

module "agent_provider_remote" {
  source = "../modules/agent-host"
  providers = {
    aws = aws.secondary
  }

  run_id           = var.run_id
  role             = "provider-remote"
  vpc_id           = module.vpc_secondary_provider.vpc_id
  subnet_id        = module.vpc_secondary_provider.public_subnet_id
  ami_id           = data.aws_ssm_parameter.al2023_secondary.value
  bucket_name      = module.artifacts.bucket_name
  bucket_region    = var.aws_region
  fetch_policy_arn = module.artifacts.fetch_policy_arn

  # Provider only knows about relay-r2. Registers svc-remote there;
  # the controller stamps relay_id=r2 onto the registry row.
  relay_endpoint = module.relay_secondary.endpoint

  agent_uuid         = "00000000-0000-0000-0000-000000000001"
  cert_prefix        = "agent-provider-eip"
  agent_mode         = "provider"
  service_name       = "svc-remote"
  attach_eip         = true
  allow_inbound_quic = true
  common_tags        = local.common_tags
}
