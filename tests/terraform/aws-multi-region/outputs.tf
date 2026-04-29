output "instance_targets" {
  description = "Targets for wait-ready.sh. Multi-region: each entry is <unit>:<iid>:<region> so SSM is queried in the right region (the API rejects cross-region InstanceIds with InvalidInstanceId)."
  value = [
    "controller:${module.control_host.instance_id}:${var.aws_region}",
    "relay:${module.control_host.instance_id}:${var.aws_region}",
    "relay-r2:${module.relay_secondary.instance_id}:${var.aws_region_secondary}",
    "consumer:${module.agent_consumer.instance_id}:${var.aws_region}",
    "provider-remote:${module.agent_provider_remote.instance_id}:${var.aws_region_secondary}",
  ]
}

output "control_instance_id" {
  value = module.control_host.instance_id
}

output "relay_secondary_instance_id" {
  value = module.relay_secondary.instance_id
}

output "consumer_instance_id" {
  value = module.agent_consumer.instance_id
}

output "provider_remote_instance_id" {
  description = "assertion 11 keys off this. Empty in aws-only/aws-gcp so 11 skips there."
  value       = module.agent_provider_remote.instance_id
}

output "relay_endpoint" {
  value = module.control_host.relay_endpoint
}

output "relay_secondary_endpoint" {
  value = module.relay_secondary.endpoint
}

output "primary_url" {
  description = "Generic curl target for the relay-roundtrip family of assertions. Points at the consumer's bind for svc-remote."
  value       = "http://127.0.0.1:30001/"
}

# Empty stubs for assertions that key off other configs' outputs.
# Each assertion that hits one of these uses tf_out's "skip when
# empty" idiom to no-op instead of erroring on a missing output.
output "consumer_tcp_instance_id" { value = "" }
output "provider_eip_instance_id" { value = "" }
output "provider_gcp_instance_id" { value = "" }
output "provider_nat_instance_id" { value = "" }
output "forward_endpoint" { value = "" }
output "bucket_name" { value = module.artifacts.bucket_name }
