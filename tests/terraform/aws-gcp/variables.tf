variable "run_id" {
  type = string
}

variable "owner_tag" {
  type    = string
  default = "outrelay-smoke"
}

variable "aws_region" {
  type    = string
  default = "ap-northeast-2"
}

variable "expires_after_hours" {
  type    = number
  default = 2
}

variable "artifacts_dir" {
  type = string
}

# GCP-side variables. Plumbed but not yet referenced — the GCP
# resources are commented placeholders pending credentials.

variable "gcp_project" {
  description = "GCP project id. Required for aws-gcp; pass via env var TF_VAR_gcp_project."
  type        = string
  validation {
    condition     = length(var.gcp_project) > 0
    error_message = "gcp_project must be set (TF_VAR_gcp_project=<project-id>)."
  }
}

variable "gcp_region" {
  type    = string
  default = "asia-northeast3"
}

variable "gcp_zone" {
  type    = string
  default = "asia-northeast3-a"
}
