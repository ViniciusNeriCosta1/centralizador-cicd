package policy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func escreve(t *testing.T, conteudo string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "politica.json")
	if err := os.WriteFile(p, []byte(conteudo), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadValida(t *testing.T) {
	p, err := Load(escreve(t, `{"severity":"obrigatoria","sensitive_types":["aws_db_instance"],"ignore_types":["aws_ecs_task_definition"]}`))
	if err != nil {
		t.Fatal(err)
	}
	if !p.IsSensitive("aws_db_instance") || p.IsSensitive("aws_instance") {
		t.Error("IsSensitive errado")
	}
	if !p.IsIgnored("aws_ecs_task_definition") || p.IsIgnored("aws_db_instance") {
		t.Error("IsIgnored errado")
	}
}

func TestLoadRejeita(t *testing.T) {
	casos := map[string]struct{ conteudo, trecho string }{
		"severidade desconhecida": {`{"severity":"talvez"}`, "desconhecida"},
		"severidade ausente":      {`{"sensitive_types":["x"]}`, "desconhecida"},
		"json quebrado":           {`{`, "politica.json"},
	}
	for nome, c := range casos {
		t.Run(nome, func(t *testing.T) {
			_, err := Load(escreve(t, c.conteudo))
			if err == nil {
				t.Fatal("esperava erro: severidade inválida não pode cair em padrão silencioso")
			}
			if !strings.Contains(err.Error(), c.trecho) {
				t.Errorf("erro %q deveria conter %q", err, c.trecho)
			}
		})
	}
}

func TestLoadArquivoInexistente(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nao-existe.json")); err == nil {
		t.Fatal("esperava erro")
	}
}

// A política que o projeto distribui precisa ser válida.
func TestPoliticaDistribuidaEValida(t *testing.T) {
	p, err := Load(filepath.Join("..", "..", "config", "plan-policy.json"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Severity != Obrigatoria {
		t.Errorf("severity = %q", p.Severity)
	}
	if !p.IsSensitive("aws_db_instance") {
		t.Error("aws_db_instance deveria ser sensível")
	}
	if !p.IsIgnored("aws_ecs_task_definition") {
		t.Error("aws_ecs_task_definition deveria ser ignorada")
	}
}
