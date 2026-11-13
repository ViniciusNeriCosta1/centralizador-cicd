# Demonstração do apply com destruição, sem nuvem e sem credenciais.
#
# O terraform.tfstate desta pasta diz que o recurso já existe com "v1". O código
# pede "v2": mudar triggers_replace RECRIA o recurso, então o plano destrói o
# antigo. Em pull request o plan-gate publica essa destruição no comentário do
# bot; no merge o apply compara e libera porque estava no plano revisado.
resource "terraform_data" "exemplo" {
  triggers_replace = "v2"
}
