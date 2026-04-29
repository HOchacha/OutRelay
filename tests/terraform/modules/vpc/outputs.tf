output "vpc_id" {
  value = aws_vpc.this.id
}

output "public_subnet_id" {
  value = aws_subnet.public.id
}

output "private_subnet_id" {
  description = "Empty string when with_nat_gateway = false."
  value       = var.with_nat_gateway ? aws_subnet.private[0].id : ""
}

output "nat_gateway_eip" {
  description = "Public IP of the NAT GW, used only when the test needs to assert NAT GW symmetric mapping behaviour."
  value       = var.with_nat_gateway ? aws_eip.nat[0].public_ip : ""
}
