package gate

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"centralizador-cicd/internal/plan"
	"centralizador-cicd/internal/policy"
)

func load(t *testing.T, rel string) *plan.Plan {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", rel))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p, err := plan.Parse(f)
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func politica(sev string) policy.Policy {
	return policy.Policy{
		Severity:       sev,
		SensitiveTypes: []string{"aws_db_instance", "aws_rds_cluster", "aws_security_group", "terraform_data"},
		IgnoreTypes:    []string{"aws_ecs_task_definition"},
	}
}

func TestEvaluate(t *testing.T) {
	casos := []struct {
		nome          string
		fixture       string
		severidade    string
		aprovado      bool
		bloq, aprov   int
		avisos, ign   int
		saidaEsperada int
	}{
		{"sem mudanças", "plans/no-changes.json", policy.Obrigatoria, false, 0, 0, 0, 0, 0},
		{"só atualização", "plans/clean.json", policy.Obrigatoria, false, 0, 0, 0, 0, 0},
		{"só destruir não é recriação", "plans/destroy-only.json", policy.Obrigatoria, false, 0, 0, 0, 0, 0},
		{"sensível bloqueia", "plans/replace-sensitive.json", policy.Obrigatoria, false, 1, 0, 0, 0, 1},
		{"sensível com aprovação libera", "plans/replace-sensitive.json", policy.Obrigatoria, true, 0, 1, 0, 0, 0},
		{"sensível em modo aviso só avisa", "plans/replace-sensitive.json", policy.Aviso, false, 0, 0, 1, 0, 0},
		{"tipo comum só avisa", "plans/replace-common.json", policy.Obrigatoria, false, 0, 0, 1, 0, 0},
		{"tipo ignorado não avisa nem bloqueia", "plans/replace-ignored.json", policy.Obrigatoria, false, 0, 0, 0, 1, 0},
		{"create_before_destroy também é recriação", "plans/create-before-destroy.json", policy.Obrigatoria, false, 1, 0, 0, 0, 1},
		{"misto: data source é ignorado pelo modo", "plans/mixed.json", policy.Obrigatoria, false, 1, 0, 1, 1, 1},
		{"plano real do Terraform bloqueia", "real/replace.json", policy.Obrigatoria, false, 1, 0, 0, 0, 1},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			r := Evaluate(load(t, c.fixture), politica(c.severidade), c.aprovado)
			if len(r.Blocking) != c.bloq || len(r.Approved) != c.aprov || len(r.Warnings) != c.avisos || len(r.Ignored) != c.ign {
				t.Errorf("bloq/aprov/avisos/ign = %d/%d/%d/%d, quer %d/%d/%d/%d",
					len(r.Blocking), len(r.Approved), len(r.Warnings), len(r.Ignored),
					c.bloq, c.aprov, c.avisos, c.ign)
			}
			if got := r.ExitCode(); got != c.saidaEsperada {
				t.Errorf("ExitCode = %d, quer %d", got, c.saidaEsperada)
			}
		})
	}
}

func TestRenderMostraAtributoQueForcaARecriacao(t *testing.T) {
	r := Evaluate(load(t, "plans/replace-sensitive.json"), politica(policy.Obrigatoria), false)
	var b bytes.Buffer
	r.Render(&b)
	out := b.String()
	for _, trecho := range []string{"[BLOQUEADO]", "aws_db_instance.main", "por mudança em: identifier", "REPROVADO"} {
		if !strings.Contains(out, trecho) {
			t.Errorf("saída deveria conter %q:\n%s", trecho, out)
		}
	}
}

func TestRenderSemRecriacao(t *testing.T) {
	r := Evaluate(load(t, "plans/clean.json"), politica(policy.Obrigatoria), false)
	var b bytes.Buffer
	r.Render(&b)
	if !strings.Contains(b.String(), "OK: nenhum recurso será recriado.") {
		t.Errorf("saída inesperada: %s", b.String())
	}
}
