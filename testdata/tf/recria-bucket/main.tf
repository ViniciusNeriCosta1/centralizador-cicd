# Demonstração do gate sem nuvem e sem credenciais.
#
# O arquivo terraform.tfstate desta pasta diz que o bucket "demo-recriacao-a"
# já existe. O código abaixo pede "demo-recriacao-b": o nome do bucket não pode
# ser alterado no lugar, então o plano RECRIA o recurso, e aws_s3_bucket está
# na lista de tipos sensíveis.
#
# Tudo é falso: credenciais fictícias, nenhuma chamada à AWS (o plano roda com
# -refresh=false e as validações de conta estão desligadas).
terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = "~> 5.0"
    }
  }
}

provider "aws" {
  region                      = "us-east-1"
  access_key                  = "fake"
  secret_key                  = "fake"
  skip_credentials_validation = true
  skip_metadata_api_check     = true
  skip_requesting_account_id  = true
  skip_region_validation      = true

  # As tags obrigatórias (config/tag-policy.json), para a demonstração do
  # label testar só a recriação.
  default_tags {
    tags = {
      Project     = "demo"
      Environment = "dev"
      Owner       = "plataforma"
    }
  }
}

resource "aws_s3_bucket" "demo" {
  bucket = "demo-recriacao-b"
}
