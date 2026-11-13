# Demonstração do gate de tags sem nuvem e sem credenciais: o bucket recebe as
# três tags obrigatórias, uma pelo recurso e duas pelo default_tags do provider.
# O plano mostra o resultado em `tags_all`, que é o que o gate lê.
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

  default_tags {
    tags = {
      Project     = "demo"
      Environment = "dev"
    }
  }
}

resource "aws_s3_bucket" "demo" {
  bucket = "demo-tags-ok"
  tags = {
    Owner = "plataforma"
  }
}
