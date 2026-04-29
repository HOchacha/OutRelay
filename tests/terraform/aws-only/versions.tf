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

provider "aws" {
  region = var.aws_region

  # default_tags hits every taggable AWS resource, but a handful of
  # APIs (some IAM bits) silently drop default tags, so modules also
  # pass `common_tags` into resource-level `tags`.
  default_tags {
    tags = local.common_tags
  }
}
