# modules/vpc
#
# A small VPC with one public subnet and (optionally) one private
# subnet behind a NAT Gateway. The smoke topology composes one
# instance of this module per "cluster" so each agent sits in its
# own VPC — that's the whole point of the test.

resource "aws_vpc" "this" {
  cidr_block           = var.cidr
  enable_dns_hostnames = true
  enable_dns_support   = true

  tags = merge(var.common_tags, { Name = "outrelay-${var.name}" })
}

resource "aws_internet_gateway" "this" {
  vpc_id = aws_vpc.this.id
  tags   = merge(var.common_tags, { Name = "outrelay-${var.name}-igw" })
}

# Public subnet — always present. Hosts whatever needs an EIP
# (controller+relay, the EIP-attached provider) and serves as the
# location for the NAT GW when `with_nat_gateway` is true.
resource "aws_subnet" "public" {
  vpc_id                  = aws_vpc.this.id
  availability_zone       = var.azs[0]
  cidr_block              = cidrsubnet(var.cidr, 4, 0)
  map_public_ip_on_launch = true

  tags = merge(var.common_tags, { Name = "outrelay-${var.name}-public" })
}

resource "aws_route_table" "public" {
  vpc_id = aws_vpc.this.id
  tags   = merge(var.common_tags, { Name = "outrelay-${var.name}-public-rt" })
}

resource "aws_route" "public_default" {
  route_table_id         = aws_route_table.public.id
  destination_cidr_block = "0.0.0.0/0"
  gateway_id             = aws_internet_gateway.this.id
}

resource "aws_route_table_association" "public" {
  subnet_id      = aws_subnet.public.id
  route_table_id = aws_route_table.public.id
}

# Private subnet + NAT GW — only when the caller asks for it.
# count is the idiomatic Terraform way to make a whole resource
# optional; aws_subnet.private[0] is the conditional reference.

resource "aws_eip" "nat" {
  count  = var.with_nat_gateway ? 1 : 0
  domain = "vpc"
  tags   = merge(var.common_tags, { Name = "outrelay-${var.name}-nat-eip" })
}

resource "aws_nat_gateway" "this" {
  count         = var.with_nat_gateway ? 1 : 0
  subnet_id     = aws_subnet.public.id
  allocation_id = aws_eip.nat[0].id

  tags = merge(var.common_tags, { Name = "outrelay-${var.name}-nat" })

  depends_on = [aws_internet_gateway.this]
}

resource "aws_subnet" "private" {
  count             = var.with_nat_gateway ? 1 : 0
  vpc_id            = aws_vpc.this.id
  availability_zone = length(var.azs) > 1 ? var.azs[1] : var.azs[0]
  cidr_block        = cidrsubnet(var.cidr, 4, 1)

  tags = merge(var.common_tags, { Name = "outrelay-${var.name}-private" })
}

resource "aws_route_table" "private" {
  count  = var.with_nat_gateway ? 1 : 0
  vpc_id = aws_vpc.this.id
  tags   = merge(var.common_tags, { Name = "outrelay-${var.name}-private-rt" })
}

resource "aws_route" "private_default" {
  count                  = var.with_nat_gateway ? 1 : 0
  route_table_id         = aws_route_table.private[0].id
  destination_cidr_block = "0.0.0.0/0"
  nat_gateway_id         = aws_nat_gateway.this[0].id
}

resource "aws_route_table_association" "private" {
  count          = var.with_nat_gateway ? 1 : 0
  subnet_id      = aws_subnet.private[0].id
  route_table_id = aws_route_table.private[0].id
}
