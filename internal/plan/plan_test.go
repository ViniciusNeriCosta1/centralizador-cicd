package plan

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func load(t *testing.T, rel string) *Plan {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", rel))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	p, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse(%s): %v", rel, err)
	}
	return p
}

func TestParseRejeitaEntradaQueNaoEPlano(t *testing.T) {
	casos := map[string]string{
		"state, não plano": `{"version":4,"resources":[]}`,
		"json quebrado":    `{"format_version":`,
		"vazio":            ``,
	}
	for nome, in := range casos {
		t.Run(nome, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(in)); err == nil {
				t.Fatal("esperava erro, veio nil: um gate que aceita qualquer JSON aprova por engano")
			}
		})
	}
}

func TestParsePlanoSemMudancas(t *testing.T) {
	p := load(t, "plans/no-changes.json")
	if len(p.ResourceChanges) != 0 {
		t.Fatalf("esperava 0 mudanças, veio %d", len(p.ResourceChanges))
	}
}

func TestIsReplacement(t *testing.T) {
	casos := []struct {
		nome    string
		actions []string
		want    bool
		cbd     bool
	}{
		{"recriação", []string{"delete", "create"}, true, false},
		{"create_before_destroy", []string{"create", "delete"}, true, true},
		{"só destruir", []string{"delete"}, false, false},
		{"só criar", []string{"create"}, false, false},
		{"atualizar", []string{"update"}, false, false},
		{"sem ação", nil, false, false},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			ch := Change{Actions: c.actions}
			if got := ch.IsReplacement(); got != c.want {
				t.Errorf("IsReplacement = %v, quer %v", got, c.want)
			}
			if got := ch.CreateBeforeDestroy(); got != c.cbd {
				t.Errorf("CreateBeforeDestroy = %v, quer %v", got, c.cbd)
			}
		})
	}
}

func TestReplaceAttributesMisturaNomeEIndice(t *testing.T) {
	p := load(t, "plans/create-before-destroy.json")
	got := p.ResourceChanges[0].Change.ReplaceAttributes()
	want := []string{"name", "ingress.0.cidr_blocks"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ReplaceAttributes = %v, quer %v", got, want)
	}
}

// O plano real vem do Terraform de verdade (scripts/gerar-plano-real.sh):
// prova que o parser lê o formato que o Terraform produz, não só os JSONs
// escritos à mão.
func TestPlanoRealDoTerraform(t *testing.T) {
	p := load(t, "real/replace.json")
	if len(p.ResourceChanges) != 1 {
		t.Fatalf("esperava 1 mudança, veio %d", len(p.ResourceChanges))
	}
	rc := p.ResourceChanges[0]
	if !rc.Change.IsReplacement() {
		t.Fatalf("o plano real deveria ser recriação, actions=%v", rc.Change.Actions)
	}
	if got := rc.Change.ReplaceAttributes(); !reflect.DeepEqual(got, []string{"triggers_replace"}) {
		t.Fatalf("atributo que força a recriação = %v", got)
	}
}
