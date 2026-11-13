# Módulo mínimo, sem nuvem: o plano só cria um recurso, então o gate libera.
resource "terraform_data" "exemplo" {
  input = "ola"
}
