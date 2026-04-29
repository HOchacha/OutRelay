variable "run_id" {
  type = string
}

variable "subnet_id" {
  description = "Public subnet — control host must have an EIP so agents can dial it."
  type        = string
}

variable "vpc_id" {
  description = "VPC of the control host; needed to scope the security group."
  type        = string
}

variable "instance_type" {
  description = "Defaulted to t3.small — controller + relay together fit comfortably; t3.micro is too tight for the SQLite control-plane writes during cert rotation."
  type        = string
  default     = "t3.small"
}

variable "ami_id" {
  description = "Amazon Linux 2023 AMI. Caller resolves it via SSM parameter at the root level."
  type        = string
}

variable "bucket_name" {
  description = "S3 bucket holding the cross-compiled binaries and PKI material."
  type        = string
}

variable "fetch_policy_arn" {
  description = "IAM policy granting s3:GetObject on the artifact bucket."
  type        = string
}

variable "tenant" {
  description = "Tenant label baked into the agents' URI SAN. Must match smoke-pki's --tenant."
  type        = string
  default     = "acme"
}

variable "relay_id" {
  description = "Relay id used in its URI SAN. Must match smoke-pki's --relay-id."
  type        = string
  default     = "r1"
}

variable "common_tags" {
  type = map(string)
}

variable "enable_tcp_fallback" {
  description = "If true, the relay also listens on TCP/443 (yamux+TLS) and the SG opens that port. Used by smoke variants that exercise the UDP-blocked fallback path."
  type        = bool
  default     = false
}

variable "expose_controller" {
  description = "If true, controller binds 0.0.0.0:7444 and the SG opens TCP/7444 from anywhere — required for multi-region deployments where peer relays in other regions need to reach this controller. Insecure for production (controller's gRPC has no auth); fine for smoke tests on short-lived infra."
  type        = bool
  default     = false
}

variable "enable_forward_plane" {
  description = "If true, the relay also runs the mini-TURN forward plane on UDP/9443 and the SG opens that port. Used by smoke assertion 12."
  type        = bool
  default     = false
}
