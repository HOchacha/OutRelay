variable "run_id" {
  type = string
}

variable "vpc_id" {
  type = string
}

variable "subnet_id" {
  description = "Public subnet — relay needs an EIP."
  type        = string
}

variable "instance_type" {
  type    = string
  default = "t3.small"
}

variable "ami_id" {
  type = string
}

variable "bucket_name" {
  description = "S3 bucket holding cross-compiled binaries + PKI. Cross-region GETs work; the IAM policy attached to the VM is bucket-scoped, not region-scoped."
  type        = string
}

variable "bucket_region" {
  description = "Region the bucket lives in. Required for the relay-host (which runs in a *different* region) — aws s3 cp on AL2023 doesn't auto-redirect on the first request, so we set AWS_DEFAULT_REGION to the bucket's region for the download. Defaults to empty (uses VM's own region)."
  type        = string
  default     = ""
}

variable "fetch_policy_arn" {
  type = string
}

variable "relay_id" {
  description = "Relay id baked into the cert URI SAN (e.g. r2). Must match a leaf emitted by smoke-pki --relay-ids."
  type        = string
}

variable "region_label" {
  description = "Region label passed to outrelay-relay --region. The controller stamps this onto each registered relay row; the registry's Resolve uses it to prefer same-region providers."
  type        = string
}

variable "controller_endpoint" {
  description = "host:port of the controller's gRPC listener. The relay dials this on boot to subscribe policies and call Resolve."
  type        = string
}

variable "tenant" {
  type    = string
  default = "acme"
}

variable "common_tags" {
  type = map(string)
}
