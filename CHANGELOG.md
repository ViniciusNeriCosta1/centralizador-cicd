# Changelog

Formato baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/),
versões em [SemVer](https://semver.org/lang/pt-BR/).

## [Não lançado]

## [1.1.0] - 2026-11-13

### Adicionado
- Apply gateado: `cicd apply-gate`, a action `apply-gate` e o workflow
  reutilizável `apply.yml`. O apply compara a destruição do seu plano com o plano
  revisado no pull request (status `nada-a-destruir`, `dentro-do-revisado`,
  `fora-do-revisado`, `sem-referencia`) e aplica o arquivo do plano avaliado.
- `cicd review-comment` e `cicd reviewed-id`: o `plan-gate.yml` publica no pull
  request o plano revisado, em um comentário que só vale se for do bot.

### Alterado
- O `plan-gate.yml` passa a exigir `pull-requests: write` do chamador, para
  publicar o plano revisado.

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
