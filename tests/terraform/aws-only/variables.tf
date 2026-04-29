variable "run_id" {
  description = "Per-run identifier; included in resource names + tags."
  type        = string
}

variable "owner_tag" {
  description = "owner tag value. The cleanup-stale safety net targets this exact value."
  type        = string
  default     = "outrelay-smoke"
}

variable "aws_region" {
  description = "AWS region to deploy into."
  type        = string
  default     = "ap-northeast-2"
}

variable "expires_after_hours" {
  description = "expires-at = now + this many hours. cleanup-stale.sh deletes anything past expires-at."
  type        = number
  default     = 2
}

variable "artifacts_dir" {
  description = "Local directory containing bin/ and pki/ produced by build-binaries.sh."
  type        = string
}
