output "instance_id" {
  value = aws_instance.this.id
}

output "endpoint" {
  description = "host:port that agent --relay can dial. Stable across boots (EIP)."
  value       = "${aws_eip.this.public_ip}:7443"
}

output "public_ip" {
  value = aws_eip.this.public_ip
}
