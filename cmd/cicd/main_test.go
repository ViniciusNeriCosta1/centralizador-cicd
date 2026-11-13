package main

import (
	"bytes"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"centralizador-cicd/internal/fakeecs"
)

func caminho(rel ...string) string {
	return filepath.Join(append([]string{"..", ".."}, rel...)...)
}

func exec(t *testing.T, args ...string) (codigo int, out, errOut string) {
	t.Helper()
	var o, e bytes.Buffer
	codigo = run(args, &o, &e)
	return codigo, o.String(), e.String()
}

func TestPlanGateCodigosDeSaida(t *testing.T) {
	pol := caminho("config", "plan-policy.json")
	casos := []struct {
		nome   string
		args   []string
		codigo int
		trecho string
	}{
		{"plano limpo libera", []string{"plan-gate", "-plan", caminho("testdata", "plans", "clean.json"), "-policy", pol}, 0, "OK"},
		{"banco recriado reprova", []string{"plan-gate", "-plan", caminho("testdata", "plans", "replace-sensitive.json"), "-policy", pol}, 1, "REPROVADO"},
		{"banco recriado e aprovado libera", []string{"plan-gate", "-plan", caminho("testdata", "plans", "replace-sensitive.json"), "-policy", pol, "-approved"}, 0, "APROVADO"},
		{"task definition do ECS é ignorada", []string{"plan-gate", "-plan", caminho("testdata", "plans", "replace-ignored.json"), "-policy", pol}, 0, "IGNORADO"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			codigo, out, errOut := exec(t, c.args...)
			if codigo != c.codigo {
				t.Fatalf("código = %d, quer %d\nstdout: %s\nstderr: %s", codigo, c.codigo, out, errOut)
			}
			if !strings.Contains(out, c.trecho) {
				t.Errorf("stdout deveria conter %q:\n%s", c.trecho, out)
			}
		})
	}
}

func TestApplyGate(t *testing.T) {
	pol := caminho("config", "plan-policy.json")
	plano := func(nome string) string { return caminho("testdata", "plans", nome) }
	com := func(nome string) string { return caminho("testdata", "comments", nome) }

	casos := []struct {
		nome   string
		args   []string
		codigo int
		trecho string
	}{
		{"nada a destruir libera mesmo sem comentários",
			[]string{"apply-gate", "-plan", plano("clean.json"), "-policy", pol}, 0, "nada-a-destruir"},
		{"destruição sem comentários bloqueia (sem referência)",
			[]string{"apply-gate", "-plan", plano("replace-common.json"), "-policy", pol}, 1, "sem-referencia"},
		{"destruição revisada libera",
			[]string{"apply-gate", "-plan", plano("replace-common.json"), "-policy", pol, "-comments", com("revisado-instance-web.json")}, 0, "dentro-do-revisado"},
		{"destruição de outro recurso bloqueia (fora do revisado)",
			[]string{"apply-gate", "-plan", plano("replace-common.json"), "-policy", pol, "-comments", com("revisado-bucket-logs.json")}, 1, "fora-do-revisado"},
		{"comentário forjado por quem não é o bot não vale",
			[]string{"apply-gate", "-plan", plano("replace-common.json"), "-policy", pol, "-comments", com("forjado-instance-web.json")}, 1, "sem-referencia"},
		{"comentários sem marcador não são referência",
			[]string{"apply-gate", "-plan", plano("replace-common.json"), "-policy", pol, "-comments", com("sem-referencia.json")}, 1, "sem-referencia"},
		{"várias páginas de comentários",
			[]string{"apply-gate", "-plan", plano("replace-common.json"), "-policy", pol, "-comments", com("paginado.json")}, 0, "dentro-do-revisado"},
		{"outro diretório não enxerga a referência de .",
			[]string{"apply-gate", "-plan", plano("replace-common.json"), "-policy", pol, "-comments", com("revisado-instance-web.json"), "-dir", "infra"}, 1, "sem-referencia"},
		{"aprovação explícita libera destruição sem referência",
			[]string{"apply-gate", "-plan", plano("replace-common.json"), "-policy", pol, "-approved"}, 0, "APROVADO"},
		// O gate de recriação continua valendo no apply, mesmo com delta limpo.
		{"banco recriado e revisado ainda bloqueia sem aprovação",
			[]string{"apply-gate", "-plan", plano("replace-sensitive.json"), "-policy", pol, "-comments", com("revisado-db-main.json")}, 1, "BLOQUEADO"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			codigo, out, errOut := exec(t, c.args...)
			if codigo != c.codigo {
				t.Fatalf("código = %d, quer %d\nstdout: %s\nstderr: %s", codigo, c.codigo, out, errOut)
			}
			if !strings.Contains(out, c.trecho) {
				t.Errorf("stdout deveria conter %q:\n%s", c.trecho, out)
			}
		})
	}
}

func TestTagCheck(t *testing.T) {
	pol := caminho("config", "tag-policy.json")
	real := func(nome string) string { return caminho("testdata", "real", nome) }
	casos := []struct {
		nome   string
		args   []string
		codigo int
		trecho string
	}{
		{"plano real com as três tags libera",
			[]string{"tag-check", "-plan", real("tags-ok.json"), "-policy", pol}, 0, "OK: 1 recurso(s)"},
		{"plano real sem Environment e Owner reprova",
			[]string{"tag-check", "-plan", real("tags-faltando.json"), "-policy", pol}, 1, "falta Environment, Owner"},
		{"recurso sem suporte a tags não é conferido",
			[]string{"tag-check", "-plan", caminho("testdata", "plans", "clean.json"), "-policy", pol}, 0, "OK"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			codigo, out, errOut := exec(t, c.args...)
			if codigo != c.codigo {
				t.Fatalf("código = %d, quer %d\nstdout: %s\nstderr: %s", codigo, c.codigo, out, errOut)
			}
			if !strings.Contains(out, c.trecho) {
				t.Errorf("stdout deveria conter %q:\n%s", c.trecho, out)
			}
		})
	}
}

func TestDepAudit(t *testing.T) {
	pol := caminho("config", "dependency-policy.json")
	real := func(nome string) string { return caminho("testdata", "real", nome) }
	casos := []struct {
		nome   string
		args   []string
		codigo int
		trecho string
	}{
		{"função vulnerável chamada reprova",
			[]string{"dep-audit", "-report", real("vuln-chamada.json"), "-policy", pol}, 1, "BLOQUEADO"},
		{"só importar o pacote libera, com aviso",
			[]string{"dep-audit", "-report", real("vuln-pacote.json"), "-policy", pol}, 0, "AVISO"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			codigo, out, errOut := exec(t, c.args...)
			if codigo != c.codigo {
				t.Fatalf("código = %d, quer %d\nstdout: %s\nstderr: %s", codigo, c.codigo, out, errOut)
			}
			if !strings.Contains(out, c.trecho) {
				t.Errorf("stdout deveria conter %q:\n%s", c.trecho, out)
			}
		})
	}

	// Exceção com prazo: em vigor libera; vencida volta a reprovar.
	excecao := filepath.Join(t.TempDir(), "dep.json")
	conteudo := `{"severity":"obrigatoria","block_on":["chamada"],"ignore":[{"id":"GO-2021-0113","reason":"só em teste","expires":"2026-12-31"},{"id":"GO-2022-1059","reason":"só em teste","expires":"2026-12-31"}]}`
	if err := os.WriteFile(excecao, []byte(conteudo), 0o600); err != nil {
		t.Fatal(err)
	}
	if codigo, out, _ := exec(t, "dep-audit", "-report", real("vuln-chamada.json"), "-policy", excecao, "-today", "2026-11-13"); codigo != 0 || !strings.Contains(out, "IGNORADO") {
		t.Errorf("exceção em vigor: código %d\n%s", codigo, out)
	}
	if codigo, out, _ := exec(t, "dep-audit", "-report", real("vuln-chamada.json"), "-policy", excecao, "-today", "2027-01-01"); codigo != 1 || !strings.Contains(out, "EXCEÇÃO VENCIDA") {
		t.Errorf("exceção vencida: código %d\n%s", codigo, out)
	}
}

// rollout-wait de ponta a ponta: o comando, o SDK da AWS e o ECS simulado.
func TestRolloutWait(t *testing.T) {
	t.Setenv("AWS_ACCESS_KEY_ID", "falso")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "falso")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", os.DevNull)
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", os.DevNull)
	t.Setenv("AWS_REGION", "us-east-1")

	casos := []struct {
		nome    string
		cenario string
		extra   []string
		codigo  int
		trecho  string
	}{
		{"sucesso", "sucesso", []string{"-task-definition", fakeecs.TaskDefinitionNova}, 0, "estável depois de 4 consulta(s)"},
		{"falha do circuit breaker reprova na hora", "falha", []string{"-task-definition", fakeecs.TaskDefinitionNova}, 1, "circuit breaker"},
		{"versão antiga estável não encerra a espera", "versao-antiga", []string{"-task-definition", fakeecs.TaskDefinitionNova}, 0, "estável depois de 4 consulta(s)"},
		{"travado estoura o teto", "travado", []string{"-task-definition", fakeecs.TaskDefinitionNova, "-timeout", "60ms"}, 1, "teto de 60ms estourou"},
		{"serviço inexistente é erro", "inexistente", nil, 2, ""},
		{"serviço com outro nome é erro", "sucesso", []string{"-service", "outro"}, 2, ""},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			srv, err := fakeecs.New(c.cenario)
			if err != nil {
				t.Fatal(err)
			}
			ts := httptest.NewServer(srv.Handler())
			defer ts.Close()
			args := []string{"rollout-wait", "-cluster", "demo", "-service", "web", "-endpoint", ts.URL,
				"-interval", "5ms", "-timeout", "5s"}
			args = append(args, c.extra...) // os últimos flags valem
			codigo, out, errOut := exec(t, args...)
			if codigo != c.codigo {
				t.Fatalf("código = %d, quer %d\nstdout: %s\nstderr: %s", codigo, c.codigo, out, errOut)
			}
			if !strings.Contains(out, c.trecho) {
				t.Errorf("stdout deveria conter %q:\n%s", c.trecho, out)
			}
		})
	}
}

func TestReviewCommentEReviewedIDFazemIdaEVolta(t *testing.T) {
	codigo, corpo, errOut := exec(t, "review-comment", "-plan", caminho("testdata", "plans", "replace-common.json"), "-dir", "infra")
	if codigo != 0 {
		t.Fatalf("review-comment saiu %d: %s", codigo, errOut)
	}
	if !strings.Contains(corpo, "aws_instance.web") || !strings.Contains(corpo, "<!-- cicd:revisado ") {
		t.Fatalf("comentário inesperado:\n%s", corpo)
	}

	// Monta o JSON que a API devolveria com esse comentário publicado pelo bot.
	arq := filepath.Join(t.TempDir(), "comentarios.json")
	if err := os.WriteFile(arq, []byte(fmt.Sprintf(`[{"id":4242,"user":{"login":"github-actions[bot]"},"body":%q}]`, corpo)), 0o600); err != nil {
		t.Fatal(err)
	}
	codigo, out, errOut := exec(t, "reviewed-id", "-comments", arq, "-dir", "infra")
	if codigo != 0 || strings.TrimSpace(out) != "4242" {
		t.Fatalf("reviewed-id = %d %q (%s)", codigo, out, errOut)
	}
	// Outro diretório: sem comentário, não escreve nada.
	if codigo, out, _ = exec(t, "reviewed-id", "-comments", arq, "-dir", "outro"); codigo != 0 || strings.TrimSpace(out) != "" {
		t.Fatalf("reviewed-id(outro) = %d %q", codigo, out)
	}
	// E o apply-gate aceita esse comentário como referência.
	codigo, out, errOut = exec(t, "apply-gate", "-plan", caminho("testdata", "plans", "replace-common.json"),
		"-policy", caminho("config", "plan-policy.json"), "-comments", arq, "-dir", "infra")
	if codigo != 0 || !strings.Contains(out, "dentro-do-revisado") {
		t.Fatalf("apply-gate = %d\n%s\n%s", codigo, out, errOut)
	}
}

// Erro de leitura NUNCA vira "liberado": sai 2, não 0.
func TestErrosSaemComCodigo2(t *testing.T) {
	pol := caminho("config", "plan-policy.json")
	ruim := filepath.Join(t.TempDir(), "politica-ruim.json")
	if err := os.WriteFile(ruim, []byte(`{"severity":"talvez"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	casos := map[string][]string{
		"sem argumentos":                      {},
		"apply-gate sem plano":                {"apply-gate", "-policy", pol},
		"comentários inexistentes":            {"apply-gate", "-plan", caminho("testdata", "plans", "clean.json"), "-policy", pol, "-comments", "nao-existe.json"},
		"comentários que não são lista":       {"apply-gate", "-plan", caminho("testdata", "plans", "clean.json"), "-policy", pol, "-comments", caminho("testdata", "plans", "clean.json")},
		"marcador do bot ilegível":            {"apply-gate", "-plan", caminho("testdata", "plans", "replace-common.json"), "-policy", pol, "-comments", caminho("testdata", "comments", "marcador-ilegivel.json")},
		"review-comment sem plano":            {"review-comment"},
		"reviewed-id sem comentários":         {"reviewed-id"},
		"subcomando desconhecido":             {"deploy"},
		"falta -policy":                       {"plan-gate", "-plan", caminho("testdata", "plans", "clean.json")},
		"plano inexistente":                   {"plan-gate", "-plan", "nao-existe.json", "-policy", pol},
		"state no lugar de plano":             {"plan-gate", "-plan", caminho("testdata", "plans", "not-a-plan.json"), "-policy", pol},
		"política inválida":                   {"plan-gate", "-plan", caminho("testdata", "plans", "clean.json"), "-policy", ruim},
		"flag desconhecida":                   {"plan-gate", "-nada"},
		"tag-check sem política":              {"tag-check", "-plan", caminho("testdata", "real", "tags-ok.json")},
		"tag-check sem plano":                 {"tag-check", "-policy", caminho("config", "tag-policy.json")},
		"tag-check com política de plan-gate": {"tag-check", "-plan", caminho("testdata", "real", "tags-ok.json"), "-policy", caminho("config", "plan-policy.json")},
		"dep-audit sem política":              {"dep-audit", "-report", caminho("testdata", "real", "vuln-chamada.json")},
		"dep-audit sem relatório":             {"dep-audit", "-policy", caminho("config", "dependency-policy.json")},
		"dep-audit com plano do Terraform":    {"dep-audit", "-report", caminho("testdata", "plans", "clean.json"), "-policy", caminho("config", "dependency-policy.json")},
		"dep-audit com data inválida":         {"dep-audit", "-report", caminho("testdata", "real", "vuln-chamada.json"), "-policy", caminho("config", "dependency-policy.json"), "-today", "ontem"},
		"dep-audit com relatório inexistente": {"dep-audit", "-report", "nao-existe.json", "-policy", caminho("config", "dependency-policy.json")},
		"rollout-wait sem cluster":            {"rollout-wait", "-service", "web"},
		"rollout-wait sem serviço":            {"rollout-wait", "-cluster", "demo"},
		"rollout-wait com teto zero":          {"rollout-wait", "-cluster", "demo", "-service", "web", "-timeout", "0s"},
		"tag-check com state":                 {"tag-check", "-plan", caminho("testdata", "plans", "not-a-plan.json"), "-policy", caminho("config", "tag-policy.json")},
	}
	for nome, args := range casos {
		t.Run(nome, func(t *testing.T) {
			if codigo, out, errOut := exec(t, args...); codigo != 2 {
				t.Fatalf("código = %d, quer 2\nstdout: %s\nstderr: %s", codigo, out, errOut)
			}
		})
	}
}
