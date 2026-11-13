# Centralizador de CI/CD

Verificações de pipeline reutilizáveis, escritas em Go, para que cada projeto
**chame** uma regra central em vez de copiar a sua própria.

> Projeto de estudo. Roda sem conta em nuvem: os testes usam planos de Terraform
> em JSON (alguns escritos à mão, um gerado pelo Terraform de verdade).

## O problema

Quando cada repositório mantém o seu pipeline, as regras divergem: um valida,
outro não; um bloqueia mudança perigosa, outro aplica direto em produção. Uma
correção precisa ser repetida em todos os repositórios.

A ideia aqui é o contrário: **a regra mora em um lugar só** e é versionada. Os
projetos a consomem, e mudar a regra é um pull request neste repositório.

## O que já existe

### `plan-gate`: recriação de recurso sensível

O `terraform plan` em texto não diferencia *destruir* de *recriar*. O JSON do
plano diferencia: uma recriação aparece como as ações `["delete","create"]`
(ou `["create","delete"]` com `create_before_destroy`).

Recriar um banco de dados ou um security group costuma ser perigoso: o recurso
antigo deixa de existir. O gate lê o plano e aplica uma política:

| Situação | Resultado |
|---|---|
| Nenhuma recriação | liberado |
| Recria tipo **sensível** (banco, security group, bucket…) | **reprova** |
| Recria tipo sensível, com aprovação explícita (`-approved`) | liberado, registrado como aprovado |
| Recria tipo comum | aviso |
| Recria tipo que é recriado por natureza (ex.: task definition do ECS) | ignorado |
| Severidade `aviso` na política | nunca reprova |
| Plano ou política ilegíveis | **erro (código 2)**, nunca "liberado" |

O relatório diz **qual recurso** e **qual atributo** forçou a recriação:

```
[BLOQUEADO] recriação de recurso sensível sem aprovação (1)
  - aws_db_instance.main (aws_db_instance) por mudança em: identifier

Resultado: REPROVADO. Para seguir, aprove a destruição de forma explícita (flag -approved).
```

Códigos de saída: `0` liberado, `1` reprovado, `2` erro de uso ou de leitura.

### Política em arquivo

A política é [`config/plan-policy.json`](config/plan-policy.json): severidade,
tipos sensíveis e tipos ignorados. Não existe configuração por repositório; se
um projeto precisa de outro comportamento, muda-se o padrão central.
Severidade desconhecida é erro, para o gate não ficar mais frouxo em silêncio.

### Workflow reutilizável do GitHub Actions

O projeto consumidor escreve só uma chamada
([`examples/chamador-no-consumidor.yml`](examples/chamador-no-consumidor.yml)):

```yaml
jobs:
  plan-gate:
    uses: ViniciusNeriCosta1/centralizador-cicd/.github/workflows/plan-gate.yml@v1
    with:
      working-directory: infra
```

O workflow roda `terraform init`, `plan` e `show -json`, e aplica o gate. Pontos
de desenho:

- **Aprovação por label.** Adicionar o label `destruicao-aprovada` ao pull
  request libera a recriação de recursos sensíveis. Para o gate rodar de novo ao
  adicionar ou remover o label, o chamador dispara também em `labeled` e
  `unlabeled` (já está no exemplo).
- **Sem override de política.** O workflow não tem input de política: todo
  consumidor recebe a regra de `config/plan-policy.json` na versão escolhida em
  `versao`.
- **O plano não vira artifact.** Ele fica em `RUNNER_TEMP` e é apagado no fim,
  porque carrega o state em texto puro.
- **Credenciais por OIDC, sem chave guardada.** Com `aws-role-arn` e
  `aws-region`, o workflow assume a role da AWS com um token de curta duração
  (veja [OIDC](#oidc-sem-chave-de-longa-duração)). Sem `aws-role-arn`, nenhum
  passo de login roda: serve para módulos sem nuvem.

O CI deste repositório exercita as três camadas: testes unitários, a action
usada como um consumidor a usa (cada cenário que deve reprovar é conferido
depois) e o workflow reutilizável inteiro.

### OIDC: sem chave de longa duração

Os dois workflows reutilizáveis aceitam `aws-role-arn` e `aws-region`. Quando a
role é informada, o workflow pede um token OIDC ao GitHub e troca por
credenciais temporárias da AWS (`aws-actions/configure-aws-credentials`). Não
existe `AWS_ACCESS_KEY_ID` guardado no repositório para vazar.

O que importa não está no workflow, e sim na **trust policy da role**
([`examples/aws-oidc-trust.tf`](examples/aws-oidc-trust.tf)):

| Role | Quem pode assumir (`sub` do token) | Permissão |
|---|---|---|
| plan | `repo:dono/repo:pull_request` | só leitura |
| apply | `repo:dono/repo:ref:refs/heads/main` | o que o módulo precisa criar |

Por isso são duas roles: um pull request nunca assume a role do apply, mesmo que
alguém altere o workflow. A condição é `StringEquals` no `sub` inteiro; com
`repo:dono/*` qualquer repositório do dono assumiria a role.

O job `oidc-claims` do CI pede o token de verdade e confere `aud` e `sub` (sem
nuvem: o token só vai ao endpoint do GitHub). O exemplo de trust policy passa
por `terraform fmt` e `validate`.

Limites:

- **Não foi provado contra uma conta AWS real.** O passo de credenciais só roda
  com `aws-role-arn`, e este repositório não tem conta. O que está provado: o
  token tem o `sub` esperado, o exemplo é um Terraform válido, e sem role o
  workflow segue como antes.
- **O chamador precisa conceder `id-token: write` sempre.** Um workflow
  reutilizável não consegue pedir essa permissão só quando há OIDC. Por isso a
  mudança é de major (`v2`): chamadores do `v1` falhariam ao iniciar.
- Pull request de fork não recebe token OIDC, então o plan na AWS não roda ali.
  É o comportamento desejado.

### Versões

Uma tag `vX.Y.Z` (semver estrito, sem pré-release) dispara o
[`release.yml`](.github/workflows/release.yml), que:

1. recusa tag fora do semver e commit que não está na `main`;
2. roda `go vet` e `go test`, para não publicar versão quebrada;
3. publica a release com as notas da seção correspondente do
   [`CHANGELOG.md`](CHANGELOG.md) (sem a seção, a release falha);
4. move o alias da major (`v1`) para a tag nova, **só se ela for a maior versão
   daquela major**: publicar `1.0.1` depois de `1.1.0` não recua o `v1`.

O consumidor escolhe: `@v1` recebe as correções compatíveis; `@v1.0.0` é
imutável e reproduzível. Subir de major (`v2`) é decisão do consumidor.

### Demonstração do label

[`demo-label.yml`](.github/workflows/demo-label.yml) prova a aprovação em pull
request de verdade, sem nuvem. A pasta
[`testdata/tf/recria-bucket`](testdata/tf/recria-bucket) tem um estado fictício
que diz que o bucket `demo-recriacao-a` existe, e um código que pede
`demo-recriacao-b`. O nome do bucket não muda no lugar, então o plano o recria, e
`aws_s3_bucket` é tipo sensível. O plano roda com `-refresh=false` e
credenciais falsas; nenhuma chamada vai para a AWS.

Em um PR com o label `demo-recriacao`:

| Labels no PR | Resultado do gate |
|---|---|
| `demo-recriacao` | reprova (`BLOQUEADO`) |
| `demo-recriacao` + `destruicao-aprovada` | libera (`APROVADO`) |

O job só roda com `demo-recriacao`, para PRs comuns não ficarem vermelhos de
propósito.

### Apply gateado

Revisar um plano no pull request só vale se o apply executar **o que foi
revisado**. Entre a revisão e o merge o estado da infraestrutura pode mudar, e o
plano do apply pode destruir algo que ninguém viu.

O fluxo do [`apply.yml`](.github/workflows/apply.yml) (chamado em push na branch
de produção, exemplo em
[`examples/chamador-apply-no-consumidor.yml`](examples/chamador-apply-no-consumidor.yml)):

1. **No pull request**, o `plan-gate.yml` publica um comentário com a lista de
   recursos que serão destruídos (resumo legível + marcador invisível).
2. **No merge**, o `apply.yml` gera o plano, acha o pull request do commit e
   compara a destruição do plano do apply com a lista revisada.
3. O gate roda e, só se liberar, o workflow aplica **o arquivo do plano** que o
   gate avaliou. Não existe um segundo plano que o gate nunca viu.

O status do delta:

| Status | Significa | Resultado |
|---|---|---|
| `nada-a-destruir` | o apply não destrói nada | libera, sem precisar de referência |
| `dentro-do-revisado` | toda destruição estava no plano revisado | libera |
| `fora-do-revisado` | destrói algo que o revisado não previa | **bloqueia** |
| `sem-referencia` | há destruição e não existe plano revisado | **bloqueia** |

Destruir **menos** do que foi revisado é seguro e libera. O gate de recriação de
recursos sensíveis continua valendo no apply, e o label `destruicao-aprovada`
libera os dois.

Pontos de desenho:

- **Só o bot vale.** Qualquer pessoa pode comentar em um pull request, e um
  comentário forjado com "tudo revisado" liberaria qualquer destruição. Só conta
  o comentário de `github-actions[bot]`. Isso é testado, inclusive com um
  comentário forjado (`testdata/comments/forjado-instance-web.json`).
- **Marcador ilegível do bot é erro**, e não "sem referência": cair para um
  comentário antigo compararia contra um plano que já não vale.
- **Vários módulos no mesmo PR.** O marcador leva o diretório (`dir`), então
  cada módulo tem a sua referência.
- **O marcador não quebra com `-->`.** O JSON é escrito com `<` e `>` escapados.

A demonstração sem nuvem está em
[`testdata/tf/recria-estado`](testdata/tf/recria-estado): um estado local diz que
um `terraform_data` existe com `"v1"` e o código pede `"v2"`, o que recria o
recurso. No pull request o plano revisado lista a destruição; no push na `main`
o job `apply-e2e` do CI acha o pull request do commit, compara e aplica.

Limites conhecidos:

- O label vale para o pull request como está no momento do merge; não há
  aprovação por commit.
- O comentário do bot é a referência. Outro workflow do mesmo repositório que
  use o `GITHUB_TOKEN` também aparece como esse autor: a confiança é no
  repositório, não em um único job.
- Sem login na nuvem, sem ambiente protegido e sem `-target`.

## Como rodar

Com Go instalado:

```bash
make test     # go vet + go test
make demo     # mostra o gate reprovando e depois liberando com aprovação
```

Só com Docker:

```bash
make test-docker                      # suíte dentro de um container
docker compose --profile demo run --rm gate plan-gate \
  -plan /work/testdata/plans/replace-sensitive.json \
  -policy /etc/cicd/plan-policy.json
```

A imagem final usa `distroless` e tem menos de 10 MB.

Usar com um plano seu:

```bash
terraform plan -out=plano.tfplan
terraform show -json plano.tfplan > plano.json
cicd plan-gate -plan plano.json -policy config/plan-policy.json
```

## Como está testado

- **Casos negativos de propósito**: entrada que não é plano (um *state*), JSON
  quebrado, política com severidade inválida, flag desconhecida. Todos precisam
  sair com código `2`. É isso que prova que o gate reprova de verdade.
- **Plano real**: [`scripts/gerar-plano-real.sh`](scripts/gerar-plano-real.sh)
  gera um plano com recriação usando só o provider embutido do Terraform
  (`terraform_data`), sem nuvem. Garante que o parser lê o formato que o
  Terraform produz, e não só os JSONs escritos à mão.
- Regra de recriação coberta: só destruir não conta, `create_before_destroy`
  conta, *data sources* não contam.
- **Apply gateado**: comentário forjado por quem não é o bot, marcador ilegível,
  comentários em várias páginas (`gh api --paginate`), módulo diferente no mesmo
  PR e endereço com `-->`. Cada cenário que deve reprovar no CI é conferido
  depois de rodar.

## Estrutura

```
cmd/cicd/            ponto de entrada e códigos de saída
internal/plan/       leitura do plano em JSON
internal/policy/     carga e validação da política
internal/gate/       regras e relatório do gate de recriação
internal/delta/      destruição do apply x plano revisado; comentário do PR
config/              política distribuída
testdata/            planos, comentários de PR e módulos de exemplo
examples/            como um projeto consumidor chama os workflows
scripts/             gerador do plano real
.github/actions/     plan-gate e apply-gate
.github/workflows/   plan-gate, apply, release, CI e demonstração
```

## Planejado

Ainda **não implementado**; entra por etapas, cada uma com testes:

- Login na nuvem (OIDC) no workflow reutilizável, para planos com backend remoto.
- Validação de Terraform (formato, `validate`, tags obrigatórias).
- Auditoria de dependências com política de severidade em arquivo.
- Espera de *rollout* por estado, não por tempo fixo, testada contra uma AWS
  simulada.
- Ambiente de demonstração com LocalStack em Docker.
