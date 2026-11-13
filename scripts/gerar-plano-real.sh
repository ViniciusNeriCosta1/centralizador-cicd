#!/usr/bin/env bash
# Gera um plano REAL com recriação, sem nuvem e sem credenciais.
#
# Usa só o provider embutido do Terraform (terraform_data): `triggers_replace`
# força a recriação quando o valor muda. Serve para provar que o parser lê o
# formato que o Terraform de fato produz, e não só os JSONs escritos à mão.
#
# Uso: scripts/gerar-plano-real.sh > testdata/real/replace.json
set -euo pipefail

dir="$(mktemp -d -p "${TMPDIR_PLANO:-$HOME}" plano-real.XXXXXX)"
trap 'rm -rf "$dir"' EXIT

cat > "$dir/main.tf" <<'TF'
variable "versao" { type = string }

resource "terraform_data" "exemplo" {
  triggers_replace = var.versao
}
TF

cd "$dir"
terraform init -input=false -no-color >&2
terraform apply -auto-approve -input=false -no-color -var versao=v1 >&2
terraform plan -input=false -no-color -var versao=v2 -out=plano.tfplan >&2
# "| cat": com o Terraform instalado via snap, a saída redirecionada direto
# para arquivo chega vazia; passando por um pipe ela sai inteira.
terraform show -json plano.tfplan | cat
