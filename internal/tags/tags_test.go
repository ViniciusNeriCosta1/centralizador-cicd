package tags

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"centralizador-cicd/internal/plan"
	"centralizador-cicd/internal/policy"
)

var exigidas = Policy{Severity: policy.Obrigatoria, RequiredTags: []string{"Project", "Environment", "Owner"}}

func planoDe(t *testing.T, json string) *plan.Plan {
	t.Helper()
	p, err := plan.Parse(strings.NewReader(json))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func carrega(t *testing.T, rel string) *plan.Plan {
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

// Planos produzidos pelo Terraform de verdade (scripts/gerar-planos-tags.sh):
// provam que `tags_all` já vem com o default_tags do provider misturado.
func TestPlanosReaisDoTerraform(t *testing.T) {
	ok := Evaluate(carrega(t, "real/tags-ok.json"), exigidas)
	if ok.Checked != 1 || len(ok.Blocking) != 0 {
		t.Fatalf("tags-ok: Checked=%d Blocking=%v", ok.Checked, ok.Blocking)
	}
	falta := Evaluate(carrega(t, "real/tags-faltando.json"), exigidas)
	if len(falta.Blocking) != 1 {
		t.Fatalf("tags-faltando: Blocking=%v", falta.Blocking)
	}
	if got := falta.Blocking[0].Missing; !reflect.DeepEqual(got, []string{"Environment", "Owner"}) {
		t.Errorf("Missing = %v", got)
	}
	if falta.ExitCode() != 1 || ok.ExitCode() != 0 {
		t.Errorf("códigos de saída: ok=%d falta=%d", ok.ExitCode(), falta.ExitCode())
	}
}

func recurso(addr, tipo, acoes, after, unknown string) string {
	return `{"address":"` + addr + `","mode":"managed","type":"` + tipo + `","name":"x","change":{"actions":` + acoes +
		`,"after":` + after + `,"after_unknown":` + unknown + `}}`
}

func plano(rs ...string) string {
	return `{"format_version":"1.2","resource_changes":[` + strings.Join(rs, ",") + `]}`
}

func TestEvaluate(t *testing.T) {
	todas := `{"tags_all":{"Project":"p","Environment":"e","Owner":"o"}}`
	casos := []struct {
		nome         string
		plano        string
		pol          Policy
		bloqueia     []string // endereços
		avisa        []string
		checked      int
		naoVerifica  bool
		faltando     []string
		faltandoDeEn string
	}{
		{"tudo presente", plano(recurso("a.b", "aws_x", `["create"]`, todas, `{}`)), exigidas, nil, nil, 1, false, nil, ""},
		{"falta uma", plano(recurso("a.b", "aws_x", `["create"]`, `{"tags_all":{"Project":"p","Environment":"e"}}`, `{}`)), exigidas, []string{"a.b"}, nil, 1, false, []string{"Owner"}, "a.b"},
		{"valor vazio conta como ausente", plano(recurso("a.b", "aws_x", `["create"]`, `{"tags_all":{"Project":"p","Environment":"e","Owner":"  "}}`, `{}`)), exigidas, []string{"a.b"}, nil, 1, false, []string{"Owner"}, "a.b"},
		{"tags nulas faltam todas", plano(recurso("a.b", "aws_x", `["create"]`, `{"tags":null}`, `{}`)), exigidas, []string{"a.b"}, nil, 1, false, []string{"Project", "Environment", "Owner"}, "a.b"},
		{"usa tags quando não há tags_all", plano(recurso("a.b", "google_x", `["create"]`, `{"tags":{"Project":"p","Environment":"e","Owner":"o"}}`, `{}`)), exigidas, nil, nil, 1, false, nil, ""},
		{"tags_all vale mais que tags", plano(recurso("a.b", "aws_x", `["create"]`, `{"tags":{"Project":"p","Environment":"e","Owner":"o"},"tags_all":{"Project":"p"}}`, `{}`)), exigidas, []string{"a.b"}, nil, 1, false, []string{"Environment", "Owner"}, "a.b"},
		{"valor desconhecido mas chave existe", plano(recurso("a.b", "aws_x", `["create"]`, `{"tags_all":{"Project":"p","Environment":"e"}}`, `{"tags_all":{"Owner":true}}`)), exigidas, nil, nil, 1, false, nil, ""},
		{"tags_all inteiro desconhecido não é verificável", plano(recurso("a.b", "aws_x", `["create"]`, `{}`, `{"tags_all":true}`)), exigidas, []string{"a.b"}, nil, 1, true, nil, ""},
		{"atualização também conta", plano(recurso("a.b", "aws_x", `["update"]`, `{"tags_all":{}}`, `{}`)), exigidas, []string{"a.b"}, nil, 1, false, []string{"Project", "Environment", "Owner"}, "a.b"},
		{"recriação conta", plano(recurso("a.b", "aws_x", `["delete","create"]`, `{"tags_all":{}}`, `{}`)), exigidas, []string{"a.b"}, nil, 1, false, []string{"Project", "Environment", "Owner"}, "a.b"},
		{"só destruir não conta", plano(recurso("a.b", "aws_x", `["delete"]`, `null`, `null`)), exigidas, nil, nil, 0, false, nil, ""},
		{"sem mudança não conta", plano(recurso("a.b", "aws_x", `["no-op"]`, `{"tags_all":{}}`, `{}`)), exigidas, nil, nil, 0, false, nil, ""},
		{"recurso sem tags não é conferido", plano(recurso("a.b", "aws_x", `["create"]`, `{"name":"x"}`, `{}`)), exigidas, nil, nil, 0, false, nil, ""},
		{"data source não conta", `{"format_version":"1.2","resource_changes":[{"address":"data.a.b","mode":"data","type":"aws_x","change":{"actions":["read"],"after":{"tags":{}}}}]}`, exigidas, nil, nil, 0, false, nil, ""},
		{"tipo ignorado", plano(recurso("a.b", "aws_x", `["create"]`, `{"tags_all":{}}`, `{}`)), Policy{Severity: policy.Obrigatoria, RequiredTags: []string{"Project"}, IgnoreTypes: []string{"aws_x"}}, nil, nil, 0, false, nil, ""},
		{"aviso não bloqueia", plano(recurso("a.b", "aws_x", `["create"]`, `{"tags_all":{}}`, `{}`)), Policy{Severity: policy.Aviso, RequiredTags: []string{"Project"}}, nil, []string{"a.b"}, 1, false, []string{"Project"}, "a.b"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			r := Evaluate(planoDe(t, c.plano), c.pol)
			if r.Checked != c.checked {
				t.Errorf("Checked = %d, quer %d", r.Checked, c.checked)
			}
			if got := enderecos(r.Blocking); !reflect.DeepEqual(got, c.bloqueia) {
				t.Errorf("Blocking = %v, quer %v", got, c.bloqueia)
			}
			if got := enderecos(r.Warnings); !reflect.DeepEqual(got, c.avisa) {
				t.Errorf("Warnings = %v, quer %v", got, c.avisa)
			}
			achados := append(append([]Finding{}, r.Blocking...), r.Warnings...)
			if c.naoVerifica && (len(achados) != 1 || !achados[0].Unverifiable) {
				t.Errorf("esperava um achado não verificável: %+v", achados)
			}
			if c.faltandoDeEn != "" {
				if len(achados) != 1 || !reflect.DeepEqual(achados[0].Missing, c.faltando) {
					t.Errorf("Missing = %+v, quer %v", achados, c.faltando)
				}
			}
		})
	}
}

func enderecos(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Address)
	}
	return out
}

func TestSaidaOrdenadaPorEndereco(t *testing.T) {
	r := Evaluate(planoDe(t, plano(
		recurso("z.b", "aws_x", `["create"]`, `{"tags_all":{}}`, `{}`),
		recurso("a.b", "aws_x", `["create"]`, `{"tags_all":{}}`, `{}`),
	)), exigidas)
	if got := enderecos(r.Blocking); !reflect.DeepEqual(got, []string{"a.b", "z.b"}) {
		t.Fatalf("ordem = %v", got)
	}
}

func TestRender(t *testing.T) {
	var b bytes.Buffer
	Evaluate(planoDe(t, plano()), exigidas).Render(&b)
	if !strings.Contains(b.String(), "nenhum recurso com suporte a tags") {
		t.Errorf("plano vazio: %q", b.String())
	}
	b.Reset()
	Evaluate(planoDe(t, plano(recurso("a.b", "aws_x", `["create"]`, `{"tags_all":{"Project":"p","Environment":"e","Owner":"o"}}`, `{}`))), exigidas).Render(&b)
	if !strings.Contains(b.String(), "OK: 1 recurso(s)") {
		t.Errorf("tudo certo: %q", b.String())
	}
	b.Reset()
	Evaluate(planoDe(t, plano(
		recurso("a.b", "aws_x", `["create"]`, `{"tags_all":{"Project":"p"}}`, `{}`),
		recurso("c.d", "aws_x", `["create"]`, `{}`, `{"tags_all":true}`),
	)), exigidas).Render(&b)
	for _, tr := range []string{"BLOQUEADO", "a.b (aws_x): falta Environment, Owner", "c.d (aws_x): tags só serão conhecidas", "REPROVADO"} {
		if !strings.Contains(b.String(), tr) {
			t.Errorf("saída deveria conter %q:\n%s", tr, b.String())
		}
	}
}

func TestLoad(t *testing.T) {
	escreve := func(conteudo string) string {
		p := filepath.Join(t.TempDir(), "tags.json")
		if err := os.WriteFile(p, []byte(conteudo), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if _, err := Load(filepath.Join("..", "..", "config", "tag-policy.json")); err != nil {
		t.Fatalf("a política que acompanha a ferramenta precisa carregar: %v", err)
	}
	ruins := map[string]string{
		"severidade desconhecida": `{"severity":"talvez","required_tags":["A"]}`,
		"sem severidade":          `{"required_tags":["A"]}`,
		"lista vazia":             `{"severity":"obrigatoria","required_tags":[]}`,
		"sem lista":               `{"severity":"obrigatoria"}`,
		"tag com nome vazio":      `{"severity":"obrigatoria","required_tags":["A"," "]}`,
		"json quebrado":           `{"severity":`,
	}
	for nome, c := range ruins {
		t.Run(nome, func(t *testing.T) {
			if _, err := Load(escreve(c)); err == nil {
				t.Fatal("esperava erro")
			}
		})
	}
	if _, err := Load("nao-existe.json"); err == nil {
		t.Fatal("esperava erro para arquivo inexistente")
	}
}
