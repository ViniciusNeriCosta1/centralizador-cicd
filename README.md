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
    uses: ViniciusNeriCosta1/centralizador-cicd/.github/workflows/plan-gate.yml@v2
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
| plan | `<prefixo>:pull_request` | só leitura |
| apply | `<prefixo>:ref:refs/heads/main` | o que o módulo precisa criar |

O `<prefixo>` depende do repositório. Os criados depois de 15/07/2026 (ou que
optaram) usam o `sub` **imutável**, com os IDs numéricos do dono e do
repositório: `repo:dono@123/repo@456`. Os demais usam o formato antigo,
`repo:dono/repo`. Uma trust policy no formato errado simplesmente nega o acesso.
Este repositório foi criado depois da data, e o job `oidc-claims` do CI mostrou
o formato imutável.

Por isso são duas roles: um pull request nunca assume a role do apply, mesmo que
alguém altere o workflow. A condição é `StringEquals` no `sub` inteiro; com
`repo:dono/*` qualquer repositório do dono assumiria a role.

O job `oidc-claims` do CI pede o token de verdade, confere `aud` e `sub` e diz
em qual formato o `sub` veio (sem nuvem: o token só vai ao endpoint do GitHub). O exemplo de trust policy passa
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

O consumidor escolhe: `@v2` recebe as correções compatíveis; `@v2.0.0` é
imutável e reproduzível. Subir de major é decisão do consumidor.

O `v1` ficou congelado em `1.1.0`. A `2.0.0` é major por dois motivos de
contrato: o chamador precisa conceder `id-token: write`, e `plan-gate.yml` e
`apply.yml` passam a bloquear recurso sem as tags obrigatórias. Para migrar
do `v1`: troque `@v1` por `@v2`, adicione `id-token: write` às `permissions` do
chamador e confira se os módulos têm as tags de `config/tag-policy.json`.

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

### Tags obrigatórias

`cicd tag-check` confere se os recursos que o plano **cria ou atualiza** têm as
tags de [`config/tag-policy.json`](config/tag-policy.json) (hoje `Project`,
`Environment` e `Owner`). A conferência é feita no plano, e não no código: o
`tags_all` de um recurso AWS já vem com o `default_tags` do provider misturado,
que é o que de fato vai para a nuvem.

| Situação | Resultado |
|---|---|
| Todas as tags presentes | liberado |
| Falta tag, ou valor vazio | **reprova**, dizendo qual recurso e qual tag |
| Recurso sem suporte a tags (`terraform_data`, por exemplo) | não conta |
| Só destruir, sem mudança, data source | não conta |
| `tags_all` só será conhecido depois do apply | **reprova** como "não verificável" |
| Severidade `aviso` na política | nunca reprova |
| `required_tags` vazio ou severidade desconhecida | **erro (código 2)**: o gate não exigiria nada |

O caso "não verificável" é de propósito: se o plano não permite conferir, o gate
não finge que conferiu. O gate de tags roda no `plan-gate.yml` (no pull request)
e de novo no `apply.yml`, porque o plano pode ter mudado entre os dois.

Os testes usam planos **reais** do Terraform com o provider da AWS, sem nuvem e
sem credenciais ([`scripts/gerar-planos-tags.sh`](scripts/gerar-planos-tags.sh)).

### Validação de Terraform

O workflow reutilizável
[`terraform-validate.yml`](.github/workflows/terraform-validate.yml) roda
`terraform fmt -check` e `terraform validate` (com `init -backend=false`), sem
credenciais e sem state. Os dois rodam mesmo quando o primeiro reprova, para o
autor ver os dois problemas de uma vez. Não pede `id-token`: quem só valida não
precisa de nuvem.

No CI, dois módulos inválidos garantem que o gate reprova de verdade: um está
mal formatado e é válido, o outro está bem formatado e é inválido. Assim cada
reprovação aponta para a checagem certa.

### Auditoria de dependências

`cicd dep-audit` aplica uma política ao relatório do `govulncheck -format json`.
A ideia central: **nem toda vulnerabilidade merece reprovar o pipeline**. O
`govulncheck` diz até onde o código alcança cada uma, e a política usa isso:

| Nível | Significa | Padrão |
|---|---|---|
| `chamada` | uma função vulnerável é chamada pelo código | **reprova** |
| `pacote` | o pacote afetado é importado, mas a função não é chamada | aviso |
| `modulo` | o módulo está no build, nenhum pacote afetado é importado | aviso |

O scanner emite **vários findings por vulnerabilidade**, um por nível. O nível
que vale é o mais profundo: o finding de módulo não pode rebaixar uma
vulnerabilidade cuja função é chamada (há teste para isso).

A política é [`config/dependency-policy.json`](config/dependency-policy.json).
Exceções precisam de **motivo e prazo**:

```json
{
  "severity": "obrigatoria",
  "block_on": ["chamada"],
  "ignore": [
    { "id": "GO-2021-0113", "reason": "só usada em teste", "expires": "2026-12-31" }
  ]
}
```

- O `id` pode ser o do banco do Go, um CVE ou um GHSA.
- Exceção **vencida** não vale: a vulnerabilidade volta a reprovar e o relatório
  avisa que a exceção precisa de revisão. O prazo vale até o fim do dia.
- Exceção que não casa com nada no relatório aparece como "sem uso".
- Exceção sem motivo ou sem prazo é **erro de política** (código 2).
- Relatório vazio ou que não é do govulncheck é **erro (código 2)**: se o scanner
  falhou, isso nunca vira "nenhuma vulnerabilidade".

No GitHub Actions, o workflow reutilizável
[`dependency-audit.yml`](.github/workflows/dependency-audit.yml) roda o scanner
(versão fixa do `govulncheck`, com o Go do próprio projeto, porque a biblioteca
padrão entra na análise) e aplica a política. Não precisa de credenciais.

Limites: só Go (é o que o `govulncheck` cobre), e o banco de vulnerabilidades
muda com o tempo, então um pipeline pode ficar vermelho sem nenhum commit novo.
É o comportamento desejado, mas surpreende quem não espera.

O **patch do Go entra na análise**. Numa execução real, o mesmo código passou com
o Go 1.26.11 e reprovou com o 1.26.10: o SDK da AWS usa o cliente HTTP, e uma
falha em `net/http` corrigida no 1.26.11 passou a ser *chamada*. O runner tinha
o 1.26.10 em cache, e `go 1.26` no `go.mod` aceita qualquer patch. Duas
correções: a action usa `check-latest` (o patch mais novo, e não o do cache), e
o `go.mod` deste repositório fixa o piso em `go 1.26.11`.

### Espera de rollout por estado

Depois de um deploy no ECS, a pergunta é "terminou?". Esperar um tempo fixo erra
nos dois sentidos: fica vermelho quando o deploy deu certo e demorou mais que o
limite, e gasta o limite inteiro quando o deploy falhou logo no começo.

`cicd rollout-wait` consulta o serviço (`DescribeServices`) e decide pelo
**estado**. O tempo é só o teto:

| Estado do serviço | Resultado |
|---|---|
| deployment PRIMARY `COMPLETED`, único, tasks rodando = desejadas, nenhuma pendente | **estável**, termina na hora |
| PRIMARY `FAILED` (circuit breaker) | **falhou**, termina na hora, com o motivo do ECS |
| serviço `DRAINING` ou `INACTIVE` | falhou |
| `IN_PROGRESS`, deployment antigo ainda drenando, tasks faltando | segue esperando |
| teto de tempo estourou | **reprova** (código 1), com o último estado |
| serviço inexistente, credenciais, rede | **erro (código 2)** |

Dois cuidados que fazem diferença:

- **Confere a task definition.** Logo depois do `update-service`, o serviço ainda
  pode mostrar a versão ANTIGA como estável. Com `-task-definition <arn>`, só
  vale se o PRIMARY for essa versão; sem isso a espera acabaria na hora,
  declarando pronta a versão velha. Há um cenário dedicado para isso.
- **Erro de consulta não vira resultado.** Limite de taxa e falha de rede são
  repetidos até 3 vezes seguidas; passado disso, é erro (código 2), nunca
  "estável" nem "falhou".

#### Provado contra uma AWS simulada

Não há conta AWS neste repositório, então a prova roda contra um ECS simulado
([`internal/fakeecs`](internal/fakeecs), servido pelo comando
[`cmd/fake-ecs`](cmd/fake-ecs)). Ele implementa só o `DescribeServices`, com
cenários por **número de consulta**, sem relógio: a N-ésima consulta recebe o
N-ésimo estado. Por isso os testes são determinísticos.

| Cenário | O que prova |
|---|---|
| `sucesso` | converge em 4 consultas, sem usar o teto |
| `falha` | o circuit breaker encerra na 2ª consulta, não no teto |
| `versao-antiga` | a versão antiga estável não encerra a espera |
| `travado` | só o teto encerra |
| `inexistente` | serviço que não existe é erro, não repetição |

O **SDK da AWS real** faz a chamada e interpreta a resposta; o simulador só
responde. Se os nomes de campo dele não batessem com o modelo da API, os valores
não chegariam ao teste. O CI roda a action contra o simulador, e os cenários que
devem reprovar são conferidos depois.

Limite, dito com todas as letras: o simulador foi escrito a partir da
documentação da API. Ele prova a lógica de decisão e a integração com o SDK, e
**não prova que o ECS de verdade se comporta assim**. Sem circuit breaker, um
deploy que nunca converge só é encerrado pelo teto.

```bash
go run ./cmd/fake-ecs -scenario sucesso &        # escuta em 127.0.0.1:8765
AWS_ACCESS_KEY_ID=falso AWS_SECRET_ACCESS_KEY=falso AWS_REGION=us-east-1 \
  go run ./cmd/cicd rollout-wait -cluster demo -service web \
  -endpoint http://127.0.0.1:8765 -interval 200ms \
  -task-definition arn:aws:ecs:us-east-1:123456789012:task-definition/web:2
```

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

A imagem final usa `distroless` e tem cerca de 20 MB (o SDK da AWS, usado pela
espera de rollout, é a maior parte).

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
- **Tags**: valor vazio, `tags_all` que vale mais que `tags`, tag desconhecida
  mas com chave presente, `tags_all` inteiro desconhecido, atualização e
  recriação, tipo ignorado, severidade `aviso`, política vazia.
- **Dependências**: relatórios **reais** do `govulncheck`
  ([`scripts/gerar-relatorios-vuln.sh`](scripts/gerar-relatorios-vuln.sh)), um
  com função chamada e outro só com pacote importado; o nível mais profundo
  vence, exceção vencida e exceção com alias, prazo até o fim do dia, relatório
  vazio ou de outro scanner.
- **Rollout**: o estado decide (estável, falho, em andamento) em tabela de
  casos; o laço de espera com relógio e pausa falsos (termina assim que o estado
  decide, falha na hora, estoura o teto, tolera erro transitório mas não para
  sempre); o cliente do SDK real contra o simulador; e o comando inteiro. Uma
  mutação rápida (remover a conferência da task definition) faz testes falharem.
- Regra de recriação coberta: só destruir não conta, `create_before_destroy`
  conta, *data sources* não contam.
- **Apply gateado**: comentário forjado por quem não é o bot, marcador ilegível,
  comentários em várias páginas (`gh api --paginate`), módulo diferente no mesmo
  PR e endereço com `-->`. Cada cenário que deve reprovar no CI é conferido
  depois de rodar.

## Estrutura

```
cmd/cicd/            ponto de entrada e códigos de saída
cmd/fake-ecs/        servidor do ECS simulado
internal/plan/       leitura do plano em JSON
internal/policy/     carga e validação da política
internal/gate/       regras e relatório do gate de recriação
internal/tags/       política e gate de tags obrigatórias
internal/vuln/       relatório do govulncheck e política de dependências
internal/rollout/    decisão e espera de rollout do ECS, por estado
internal/ecs/        cliente do ECS (SDK da AWS) para o rollout
internal/fakeecs/    ECS simulado, com cenários
internal/delta/      destruição do apply x plano revisado; comentário do PR
config/              políticas distribuídas (recriação, tags e dependências)
testdata/            planos, comentários de PR e módulos de exemplo
examples/            como um projeto consumidor chama os workflows
scripts/             geradores dos planos reais
.github/actions/     plan-gate, apply-gate, tag-check, terraform-validate, dependency-audit e rollout-wait
.github/workflows/   plan-gate, apply, terraform-validate, dependency-audit, release, CI e demonstração
```

## Fora de escopo

- **Emulador de AWS (LocalStack e similares).** Não há ambiente de demonstração
  com um emulador de terceiros: depender de conta ou de token num repositório
  público não compensa o que se ganharia. A prova do rollout é o simulador
  próprio (`internal/fakeecs`), com o limite descrito na seção de rollout: ele
  prova a lógica e a integração com o SDK, não o ECS de verdade.
- **Conta AWS real.** O OIDC e o rollout nunca rodaram contra uma. Está dito
  nos dois lugares em que isso importa.
