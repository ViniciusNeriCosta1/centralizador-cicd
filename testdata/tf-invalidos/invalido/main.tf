# Bem formatado, mas referencia uma variável que não existe: só o
# `terraform validate` reprova.
resource "terraform_data" "exemplo" {
  input = var.nao_declarada
}
