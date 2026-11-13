#!/usr/bin/env bash
# Regenera os relatórios REAIS do govulncheck (testdata/real/vuln-*.json).
#
# Cria dois módulos descartáveis que dependem de uma versão antiga e vulnerável
# de golang.org/x/text:
#   vuln-chamada : o código CHAMA a função vulnerável (language.ParseAcceptLanguage)
#   vuln-pacote  : o código só IMPORTA o pacote afetado, sem chamar a função
#
# Precisa de rede (baixa o módulo e o banco de vulnerabilidades) e do govulncheck
# (`go install golang.org/x/vuln/cmd/govulncheck@latest`). O resultado é uma
# fotografia: o banco muda com o tempo, então os testes usam os arquivos
# commitados, não uma nova execução.
#
# Para manter o arquivo pequeno, o script guarda só as mensagens `osv` citadas
# por algum finding; as demais não mudam o resultado.
#
# Uso: scripts/gerar-relatorios-vuln.sh
set -euo pipefail

raiz="$(cd "$(dirname "$0")/.." && pwd)"
govulncheck="$(command -v govulncheck || echo "$(go env GOPATH)/bin/govulncheck")"

gera() {
  local nome="$1" corpo="$2"
  local dir
  dir="$(mktemp -d -p "${TMPDIR_VULN:-$HOME}" vuln.XXXXXX)"
  (
    cd "$dir"
    cat > go.mod <<'MOD'
module exemplo.invalido/demo

go 1.22

require golang.org/x/text v0.3.6
MOD
    printf '%s\n' "$corpo" > main.go
    go mod tidy >&2
    "$govulncheck" -format json ./... > bruto.json
    python3 - bruto.json "$raiz/testdata/real/$nome.json" <<'PY'
import json, sys
dec = json.JSONDecoder()
texto = open(sys.argv[1]).read()
msgs, i = [], 0
while True:
    while i < len(texto) and texto[i].isspace():
        i += 1
    if i >= len(texto):
        break
    obj, i = dec.raw_decode(texto, i)
    msgs.append(obj)
citados = {m["finding"]["osv"] for m in msgs if "finding" in m}
saida = [m for m in msgs
         if "config" in m or ("finding" in m) or ("osv" in m and m["osv"]["id"] in citados)]
with open(sys.argv[2], "w") as f:
    for m in saida:
        f.write(json.dumps(m, ensure_ascii=False) + "\n")
PY
  )
  rm -rf "${dir:?}"
  echo "gerado testdata/real/$nome.json" >&2
}

gera vuln-chamada 'package main

import (
	"fmt"

	"golang.org/x/text/language"
)

func main() {
	t, _, err := language.ParseAcceptLanguage("pt-BR,en;q=0.8")
	fmt.Println(t, err)
}'

gera vuln-pacote 'package main

import (
	"fmt"

	"golang.org/x/text/language"
)

func main() {
	fmt.Println(language.English)
}'
