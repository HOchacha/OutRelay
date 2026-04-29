output "instance_id" {
  value = aws_instance.this.id
}

output "relay_endpoint" {
  description = "host:port that agent --relay flag dials. Stable across boots because it's an EIP."
  value       = "${aws_eip.this.public_ip}:7443"
}

output "relay_tcp_endpoint" {
  description = "TCP+TLS+yamux fallback endpoint. Empty when enable_tcp_fallback = false."
  value       = var.enable_tcp_fallback ? "${aws_eip.this.public_ip}:443" : ""
}

output "controller_endpoint" {
  description = "host:port that peer relays in other regions dial via gRPC. Empty when expose_controller = false."
  value       = var.expose_controller ? "${aws_eip.this.public_ip}:7444" : ""
}

output "forward_endpoint" {
  description = "host:port of the mini-TURN forward plane. Empty when enable_forward_plane = false."
  value       = var.enable_forward_plane ? "${aws_eip.this.public_ip}:9443" : ""
}


output "public_ip" {
  value = aws_eip.this.public_ip
}
