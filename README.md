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

## Estrutura

```
cmd/cicd/            ponto de entrada e códigos de saída
internal/plan/       leitura do plano em JSON
internal/policy/     carga e validação da política
internal/gate/       regras e relatório
config/              política distribuída
testdata/            planos de exemplo (à mão e real)
scripts/             gerador do plano real
```

## Planejado

Ainda **não implementado**; entra por etapas, cada uma com testes:

- Workflow reutilizável do GitHub Actions que chama o `plan-gate` e lê a
  aprovação de um *label* do pull request.
- Gate de *apply*: aplicar o próprio arquivo do plano que foi avaliado.
- Validação de Terraform (formato, `validate`, tags obrigatórias).
- Auditoria de dependências com política de severidade em arquivo.
- Espera de *rollout* por estado, não por tempo fixo, testada contra uma AWS
  simulada.
- Ambiente de demonstração com LocalStack em Docker.
