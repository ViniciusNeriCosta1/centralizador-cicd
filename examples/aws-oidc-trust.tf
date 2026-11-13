# Exemplo: as duas roles que o centralizador assume por OIDC.
#
# Quem decide o que o workflow pode fazer é a TRUST POLICY da role, não o
# workflow. Duas roles, porque plan e apply têm riscos diferentes:
#
#   plan  : roda em pull request, só lê. A trust policy aceita o `sub` de
#           pull request e a role só tem permissão de leitura.
#   apply : roda no push na branch de produção. A trust policy aceita SÓ o `sub`
#           dessa branch, então um pull request (ou outra branch) nunca assume
#           esta role, mesmo que alguém altere o workflow.
#
# A condição usa StringEquals e o `sub` inteiro. `repo:dono/*` (StringLike)
# deixaria qualquer repositório do dono assumir a role.
#
# O formato do `sub` depende do repositório:
#   imutável (criado depois de 15/07/2026, ou que optou):
#       repo:dono@ID_DO_DONO/repositorio@ID_DO_REPO:pull_request
#   antigo (por nome):
#       repo:dono/repositorio:pull_request
# Os IDs vêm de `gh api repos/dono/repositorio --jq '.id, .owner.id'`. Confira o
# formato real do seu repositório pelo job `oidc-claims` do CI deste projeto, ou
# imprimindo o claim `sub`: uma trust policy no formato errado nega o acesso.
#
# Os valores abaixo são fictícios.

terraform {
  required_providers {
    aws = {
      source  = "hashicorp/aws"
      version = ">= 5.81"
    }
  }
}

variable "github_sub_prefix" {
  description = "Prefixo do `sub` do repositório: repo:dono@ID/nome@ID (imutável) ou repo:dono/nome (antigo)."
  type        = string
  default     = "repo:dono@1000000/repositorio@2000000"
}

variable "producao_branch" {
  description = "Branch cujo push pode rodar o apply."
  type        = string
  default     = "main"
}

variable "apply_policy_arn" {
  description = "Política com o que o apply precisa criar. Defina o mínimo necessário."
  type        = string
  default     = "arn:aws:iam::aws:policy/PowerUserAccess"
}

locals {
  oidc_host = "token.actions.githubusercontent.com"
}

resource "aws_iam_openid_connect_provider" "github" {
  url            = "https://${local.oidc_host}"
  client_id_list = ["sts.amazonaws.com"]
}

data "aws_iam_policy_document" "confianca_plan" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }
    condition {
      test     = "StringEquals"
      variable = "${local.oidc_host}:aud"
      values   = ["sts.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "${local.oidc_host}:sub"
      values   = ["${var.github_sub_prefix}:pull_request"]
    }
  }
}

data "aws_iam_policy_document" "confianca_apply" {
  statement {
    actions = ["sts:AssumeRoleWithWebIdentity"]
    principals {
      type        = "Federated"
      identifiers = [aws_iam_openid_connect_provider.github.arn]
    }
    condition {
      test     = "StringEquals"
      variable = "${local.oidc_host}:aud"
      values   = ["sts.amazonaws.com"]
    }
    condition {
      test     = "StringEquals"
      variable = "${local.oidc_host}:sub"
      values   = ["${var.github_sub_prefix}:ref:refs/heads/${var.producao_branch}"]
    }
  }
}

resource "aws_iam_role" "plan" {
  name               = "cicd-plan"
  assume_role_policy = data.aws_iam_policy_document.confianca_plan.json
}

resource "aws_iam_role_policy_attachment" "plan_leitura" {
  role       = aws_iam_role.plan.name
  policy_arn = "arn:aws:iam::aws:policy/ReadOnlyAccess"
}

resource "aws_iam_role" "apply" {
  name               = "cicd-apply"
  assume_role_policy = data.aws_iam_policy_document.confianca_apply.json
}

resource "aws_iam_role_policy_attachment" "apply" {
  role       = aws_iam_role.apply.name
  policy_arn = var.apply_policy_arn
}

output "plan_role_arn" {
  value = aws_iam_role.plan.arn
}

output "apply_role_arn" {
  value = aws_iam_role.apply.arn
}
