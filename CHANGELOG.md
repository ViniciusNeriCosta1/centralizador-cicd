# Changelog

Formato baseado em [Keep a Changelog](https://keepachangelog.com/pt-BR/1.1.0/),
versões em [SemVer](https://semver.org/lang/pt-BR/).

## [Não lançado]

### Adicionado
- `cicd dep-audit`, a action `dependency-audit` e o workflow reutilizável
  `dependency-audit.yml`: política de dependências sobre o `govulncheck`, por
  nível de alcançabilidade (`chamada` reprova; `pacote` e `modulo` avisam), com
  exceções que têm motivo e prazo. `config/dependency-policy.json`.

### Alterado
- Go mínimo sobe de 1.22 para 1.26 (`go.mod` e `Dockerfile`). A própria auditoria
  apontou, no primeiro uso real, uma vulnerabilidade chamada pelo código na
  biblioteca padrão do 1.22 (GO-2025-3750, `os.ReadFile`), já fora de suporte.

## [2.0.0] - 2026-11-13

Major por duas quebras de contrato (veja "Alterado"). Para migrar do `v1`: troque
`@v1` por `@v2`, adicione `id-token: write` às `permissions` do chamador e
confira se os módulos têm as tags de `config/tag-policy.json`.

### Adicionado
- OIDC nos workflows `plan-gate.yml` e `apply.yml`: inputs `aws-role-arn` e
  `aws-region` assumem a role da AWS com token de curta duração, sem chave
  guardada. Sem `aws-role-arn`, nada muda.
- `examples/aws-oidc-trust.tf`: as duas roles (plan e apply) com trust policy
  presa ao `sub` do token.
- Job `oidc-claims` no CI: pede o token OIDC e confere `aud` e `sub`.
- `cicd tag-check`, a action `tag-check` e `config/tag-policy.json`: reprova
  recurso criado ou atualizado sem as tags obrigatórias, lidas do `tags_all` do
  plano (com o `default_tags` do provider). Tags que só serão conhecidas depois
  do apply reprovam como "não verificáveis".
- Workflow reutilizável `terraform-validate.yml` e action `terraform-validate`:
  `terraform fmt -check` e `terraform validate`, sem credenciais.

### Alterado
- **Quebra de contrato:** os dois workflows reutilizáveis passam a pedir
  `id-token: write`. O chamador precisa concedê-la.
- **Quebra de contrato:** `plan-gate.yml` e `apply.yml` passam a rodar o gate de
  tags. Um módulo que cria recurso sem as tags obrigatórias, e antes passava,
  agora reprova.

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
