variable "name" {
  description = "Short identifier baked into resource names and Name tag (e.g. control, provider-eip)."
  type        = string
}

variable "cidr" {
  description = "VPC CIDR block. Must not overlap with peer VPCs in this run."
  type        = string
}

variable "with_nat_gateway" {
  description = "If true, also creates a private subnet with a NAT GW for it. If false, a single public subnet is enough — the agent there gets an EIP and is reachable from peers."
  type        = bool
  default     = false
}

variable "common_tags" {
  description = "Tags merged onto every resource. Must include owner and expires-at — the cleanup-stale safety net relies on these."
  type        = map(string)
}

variable "azs" {
  description = "Availability zones to place subnets in. The first is used for the public subnet, the second (if any) for the private subnet."
  type        = list(string)
}
