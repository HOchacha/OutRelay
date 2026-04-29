variable "run_id" {
  type = string
}

variable "role" {
  description = "Role suffix for the systemd unit (controller, relay, provider-eip, provider-nat, consumer). Drives wait-ready.sh's unit name."
  type        = string
}

variable "subnet_id" {
  description = "Public subnet for an EIP-attached agent, private subnet for a NAT-bound one. Caller decides."
  type        = string
}

variable "vpc_id" {
  type = string
}

variable "instance_type" {
  type    = string
  default = "t3.micro"
}

variable "ami_id" {
  type = string
}

variable "bucket_name" {
  type = string
}

variable "bucket_region" {
  description = "Region the artifact bucket lives in. Required when this agent runs in a different region than the bucket — see modules/relay-host/variables.tf for the why."
  type        = string
  default     = ""
}

variable "fetch_policy_arn" {
  type = string
}

variable "relay_endpoint" {
  description = "host:port of the relay; agents put this on --relay."
  type        = string
}

variable "relay_tcp_endpoint" {
  description = "Optional TCP+TLS+yamux relay endpoint passed via --relay-tcp. Tried only after --relay (UDP/QUIC) fails. Set to a real endpoint to validate fallback; leave empty for the QUIC-only path."
  type        = string
  default     = ""
}

variable "tenant" {
  type    = string
  default = "acme"
}

variable "agent_uuid" {
  description = "UUID baked into the cert URI SAN. Must match a key emitted by smoke-pki."
  type        = string
}

variable "cert_prefix" {
  description = "Filename prefix for the agent's cert + key inside /opt/outrelay/pki/. e.g. agent-provider-eip → agent-provider-eip.crt + agent-provider-eip.key"
  type        = string
}

variable "agent_mode" {
  description = "provider or consumer. provider runs --expose-service; consumer runs --consume."
  type        = string
  validation {
    condition     = contains(["provider", "consumer"], var.agent_mode)
    error_message = "agent_mode must be one of: provider, consumer"
  }
}

variable "service_name" {
  description = "Service name to either expose (provider) or consume (consumer). Each provider exposes a distinct name so the relay's routing is unambiguous."
  type        = string
}

variable "consume_bind_addr" {
  description = "consumer-only: localhost address curl hits to enter the relay path. Ignored when agent_mode = provider."
  type        = string
  default     = "127.0.0.1:30001"
}

variable "extra_consumes" {
  description = "Optional extra --consume entries on the consumer side, formatted '<svc>@<bind-addr>'. Lets one consumer reach both providers."
  type        = list(string)
  default     = []
}

variable "attach_eip" {
  description = "If true, allocate an EIP and associate it. Required for the P2P-positive case so the agent has a host candidate that peers can dial."
  type        = bool
  default     = false
}

variable "allow_inbound_quic" {
  description = "If true, the SG accepts inbound UDP from anywhere (not just the relay) — required when attach_eip is set so peers can reach the host candidate. Provider-NAT and consumer hosts should set this false to keep §3.2 outbound-only honest."
  type        = bool
  default     = false
}

variable "common_tags" {
  type = map(string)
}
