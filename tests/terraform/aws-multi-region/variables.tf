variable "run_id" {
  type = string
}

variable "owner_tag" {
  type    = string
  default = "outrelay-smoke"
}

variable "aws_region" {
  description = "Primary region (control plane). Kept as `aws_region` so the harness scripts can pass it via the same env var as the other configs."
  type        = string
  default     = "ap-northeast-2"
}

variable "aws_region_secondary" {
  description = "Region for the peer relay + remote provider — must differ from aws_region for the smoke to actually exercise cross-region forwarding."
  type        = string
  default     = "us-east-1"
}

variable "expires_after_hours" {
  type    = number
  default = 2
}

variable "artifacts_dir" {
  type = string
}
