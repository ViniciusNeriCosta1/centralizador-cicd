# Changelog

Formato baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/),
versões em [SemVer](https://semver.org/lang/pt-BR/).

## [Não lançado]

## [1.0.0] - 2026-11-13

### Adicionado
- `cicd plan-gate`: lê o plano do Terraform em JSON e reprova a recriação de
  recursos sensíveis, com política em arquivo, aprovação explícita e código de
  saída `2` para qualquer erro de leitura.
- Action `plan-gate` e workflow reutilizável `plan-gate.yml`, com aprovação pelo
  label `destruicao-aprovada` e plano apagado no fim da execução.
- CI em três camadas: testes (Go e Docker), dogfood da action e contrato do
  workflow reutilizável.
- Release com tag semver estrita e alias móvel da major (`v1`).
- Demonstração do label em pull request, sem nuvem.
