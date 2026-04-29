terraform {
  required_version = ">= 1.6"

  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.60"
    }
    time = {
      source  = "hashicorp/time"
      version = "~> 0.11"
    }
  }
}

# Two AWS provider instances, one per region. Resources tag
# providers explicitly via `provider = aws.primary` /
# `provider = aws.secondary` (or modules pass them through the
# `providers = { aws = aws.secondary }` map).
provider "aws" {
  alias  = "primary"
  region = var.aws_region

  default_tags {
    tags = local.common_tags
  }
}

provider "aws" {
  alias  = "secondary"
  region = var.aws_region_secondary

  default_tags {
    tags = local.common_tags
  }
}
