# Mesmo bucket, mas só com a tag Project: faltam Environment e Owner. O gate de
# tags reprova. Sem nuvem e sem credenciais.
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
      Project = "demo"
    }
  }
}

resource "aws_s3_bucket" "demo" {
  bucket = "demo-tags-faltando"
}
