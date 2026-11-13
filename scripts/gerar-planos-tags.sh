#!/usr/bin/env bash
# Regenera os planos REAIS de tags (testdata/real/tags-ok.json e tags-faltando.json).
#
# Usa o provider da AWS com credenciais falsas e -refresh=false: nenhuma chamada
# vai para a nuvem. O plano real mostra `tags_all` já com o default_tags do
# provider misturado, que é o que o gate de tags lê.
#
# Uso: scripts/gerar-planos-tags.sh
set -euo pipefail

raiz="$(cd "$(dirname "$0")/.." && pwd)"
for modulo in tags-ok tags-faltando; do
  dir="$(mktemp -d -p "${TMPDIR_PLANO:-$HOME}" tfplan.XXXXXX)"
  cp "$raiz/testdata/tf/$modulo/main.tf" "$dir/"
  (
    cd "$dir"
    terraform init -input=false -no-color >&2
    terraform plan -input=false -no-color -refresh=false -out=plano.tfplan >&2
    # "| cat": com o Terraform via snap, a saída redirecionada direto sai vazia.
    terraform show -json plano.tfplan | cat > "$raiz/testdata/real/$modulo.json"
  )
  rm -rf "${dir:?}"
  echo "gerado testdata/real/$modulo.json" >&2
done
