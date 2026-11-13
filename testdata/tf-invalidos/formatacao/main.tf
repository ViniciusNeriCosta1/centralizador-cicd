# Sintaxe e referências corretas, mas mal formatado: só o `terraform fmt` reprova.
resource "terraform_data" "exemplo" {
input = "ola"
    triggers_replace   =   "v1"
}
