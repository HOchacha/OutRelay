output "instance_id" {
  value = aws_instance.this.id
}

output "private_ip" {
  value = aws_instance.this.private_ip
}

output "public_ip" {
  description = "Empty when attach_eip = false."
  value       = var.attach_eip ? aws_eip.this[0].public_ip : ""
}

output "role" {
  value = var.role
}
