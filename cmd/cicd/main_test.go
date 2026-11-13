package main

import (
	"bytes"
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

// Erro de leitura NUNCA vira "liberado": sai 2, não 0.
func TestErrosSaemComCodigo2(t *testing.T) {
	pol := caminho("config", "plan-policy.json")
	ruim := filepath.Join(t.TempDir(), "politica-ruim.json")
	if err := os.WriteFile(ruim, []byte(`{"severity":"talvez"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	casos := map[string][]string{
		"sem argumentos":          {},
		"subcomando desconhecido": {"deploy"},
		"falta -policy":           {"plan-gate", "-plan", caminho("testdata", "plans", "clean.json")},
		"plano inexistente":       {"plan-gate", "-plan", "nao-existe.json", "-policy", pol},
		"state no lugar de plano": {"plan-gate", "-plan", caminho("testdata", "plans", "not-a-plan.json"), "-policy", pol},
		"política inválida":       {"plan-gate", "-plan", caminho("testdata", "plans", "clean.json"), "-policy", ruim},
		"flag desconhecida":       {"plan-gate", "-nada"},
	}
	for nome, args := range casos {
		t.Run(nome, func(t *testing.T) {
			if codigo, out, errOut := exec(t, args...); codigo != 2 {
				t.Fatalf("código = %d, quer 2\nstdout: %s\nstderr: %s", codigo, out, errOut)
			}
		})
	}
}
