# Outputs consumed by the harness scripts (run.sh, wait-ready.sh,
# assertion scripts). Anything an assertion needs to find by name
# must surface here — the assertions don't peek into Terraform
# state directly.

output "instance_targets" {
  description = "List of <unit-suffix>:<instance-id>. wait-ready.sh waits for outrelay-<suffix>.service to be active on each. The control host emits two entries because it runs both controller and relay."
  value = [
    "controller:${module.control_host.instance_id}",
    "relay:${module.control_host.instance_id}",
    "provider-eip:${module.agent_provider_eip.instance_id}",
    "provider-nat:${module.agent_provider_nat.instance_id}",
    "consumer:${module.agent_consumer.instance_id}",
    "consumer-tcp:${module.agent_consumer_tcp.instance_id}",
  ]
}

output "consumer_tcp_instance_id" {
  description = "Used by assertion 09 (TCP fallback round-trip). Empty in aws-gcp."
  value       = module.agent_consumer_tcp.instance_id
}

output "control_instance_id" {
  value = module.control_host.instance_id
}

output "control_public_ip" {
  value = module.control_host.public_ip
}

output "relay_endpoint" {
  value = module.control_host.relay_endpoint
}

output "consumer_instance_id" {
  value = module.agent_consumer.instance_id
}

output "provider_eip_instance_id" {
  value = module.agent_provider_eip.instance_id
}

output "provider_eip_public_ip" {
  value = module.agent_provider_eip.public_ip
}

output "provider_nat_instance_id" {
  value = module.agent_provider_nat.instance_id
}

output "consumer_nat_gateway_eip" {
  description = "Public IP of the NAT GW in front of the consumer. Useful for asserting the consumer's srflx candidate ends up here, which proves the symmetric-NAT path is being exercised."
  value       = module.vpc_consumer.nat_gateway_eip
}

output "provider_nat_gateway_eip" {
  value = module.vpc_provider_nat.nat_gateway_eip
}

output "bucket_name" {
  value = module.artifacts.bucket_name
}

# Generic "any provider" curl target the relay-roundtrip / failover /
# control-outage assertions use. aws-only points at the EIP provider;
# aws-gcp points at the NAT-bound provider. Each config picks
# whichever stays alive across all the failure scenarios.
output "primary_url" {
  value = "http://127.0.0.1:30001/"
}

# Empty in aws-only — assertion 11 keys off this to skip.
output "provider_remote_instance_id" {
  value = ""
}

output "forward_endpoint" {
  description = "Used by assertion 12 (mini-TURN forward plane round-trip)."
  value       = module.control_host.forward_endpoint
}
