package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
		"sem argumentos":                {},
		"apply-gate sem plano":          {"apply-gate", "-policy", pol},
		"comentários inexistentes":      {"apply-gate", "-plan", caminho("testdata", "plans", "clean.json"), "-policy", pol, "-comments", "nao-existe.json"},
		"comentários que não são lista": {"apply-gate", "-plan", caminho("testdata", "plans", "clean.json"), "-policy", pol, "-comments", caminho("testdata", "plans", "clean.json")},
		"marcador do bot ilegível":      {"apply-gate", "-plan", caminho("testdata", "plans", "replace-common.json"), "-policy", pol, "-comments", caminho("testdata", "comments", "marcador-ilegivel.json")},
		"review-comment sem plano":      {"review-comment"},
		"reviewed-id sem comentários":   {"reviewed-id"},
		"subcomando desconhecido":       {"deploy"},
		"falta -policy":                 {"plan-gate", "-plan", caminho("testdata", "plans", "clean.json")},
		"plano inexistente":             {"plan-gate", "-plan", "nao-existe.json", "-policy", pol},
		"state no lugar de plano":       {"plan-gate", "-plan", caminho("testdata", "plans", "not-a-plan.json"), "-policy", pol},
		"política inválida":             {"plan-gate", "-plan", caminho("testdata", "plans", "clean.json"), "-policy", ruim},
		"flag desconhecida":             {"plan-gate", "-nada"},
	}
	for nome, args := range casos {
		t.Run(nome, func(t *testing.T) {
			if codigo, out, errOut := exec(t, args...); codigo != 2 {
				t.Fatalf("código = %d, quer 2\nstdout: %s\nstderr: %s", codigo, out, errOut)
			}
		})
	}
}
