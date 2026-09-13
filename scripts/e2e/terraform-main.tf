terraform {
  required_providers {
    null = { source = "hashicorp/null", version = "3.2.2" }
  }
}
resource "null_resource" "x" {}
module "hello" {
  source  = "host.docker.internal:18443/hl/hello/null"
  version = "1.0.0"
}
output "hello" { value = module.hello.hello }
