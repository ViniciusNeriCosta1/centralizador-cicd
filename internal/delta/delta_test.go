package delta

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

func TestCompare(t *testing.T) {
	revisado := &Reviewed{Dir: ".", Destroy: []string{"aws_instance.web", "aws_s3_bucket.logs"}}
	casos := []struct {
		nome       string
		destroying []string
		reviewed   *Reviewed
		status     Status
		bloqueia   bool
		fora       []string
	}{
		{"nada a destruir libera sem referência", nil, nil, NadaADestruir, false, nil},
		{"nada a destruir libera com referência", []string{}, revisado, NadaADestruir, false, nil},
		{"tudo dentro do revisado", []string{"aws_instance.web"}, revisado, DentroDoRevisado, false, nil},
		{"destruir menos do que o revisado é seguro", []string{"aws_s3_bucket.logs"}, revisado, DentroDoRevisado, false, nil},
		{"um recurso fora do revisado", []string{"aws_instance.web", "aws_db_instance.main"}, revisado, ForaDoRevisado, true, []string{"aws_db_instance.main"}},
		{"revisado vazio não cobre nada", []string{"aws_instance.web"}, &Reviewed{Dir: "."}, ForaDoRevisado, true, []string{"aws_instance.web"}},
		{"destruição sem referência bloqueia", []string{"b.x", "a.x"}, nil, SemReferencia, true, []string{"a.x", "b.x"}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			r := Compare(c.destroying, c.reviewed)
			if r.Status != c.status {
				t.Errorf("Status = %s, quer %s", r.Status, c.status)
			}
			if r.Blocking() != c.bloqueia {
				t.Errorf("Blocking = %v, quer %v", r.Blocking(), c.bloqueia)
			}
			if !reflect.DeepEqual(r.Unreviewed, c.fora) {
				t.Errorf("Unreviewed = %v, quer %v", r.Unreviewed, c.fora)
			}
		})
	}
}

func TestRender(t *testing.T) {
	casos := []struct {
		nome     string
		r        Result
		approved bool
		trechos  []string
	}{
		{"nada", Result{Status: NadaADestruir}, false, []string{"nada-a-destruir"}},
		{"dentro", Result{Status: DentroDoRevisado}, false, []string{"dentro-do-revisado"}},
		{"fora bloqueia", Result{Status: ForaDoRevisado, Unreviewed: []string{"a.b"}}, false, []string{"BLOQUEADO", "fora-do-revisado", "- a.b"}},
		{"fora aprovado", Result{Status: ForaDoRevisado, Unreviewed: []string{"a.b"}}, true, []string{"APROVADO", "- a.b"}},
		{"sem referência", Result{Status: SemReferencia, Unreviewed: []string{"a.b"}}, false, []string{"BLOQUEADO", "não existe plano revisado"}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			var b bytes.Buffer
			c.r.Render(&b, c.approved)
			for _, tr := range c.trechos {
				if !strings.Contains(b.String(), tr) {
					t.Errorf("saída deveria conter %q:\n%s", tr, b.String())
				}
			}
		})
	}
}
