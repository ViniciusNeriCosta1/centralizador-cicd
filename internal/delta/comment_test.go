package delta

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func comentario(id int64, login, body string) Comment {
	c := Comment{ID: id, Body: body}
	c.User.Login = login
	return c
}

func TestEncodeEFindFazemIdaEVolta(t *testing.T) {
	r := Reviewed{Dir: "infra", Destroy: []string{"aws_instance.web", "aws_s3_bucket.logs"}}
	corpo, err := EncodeComment(r)
	if err != nil {
		t.Fatal(err)
	}
	for _, trecho := range []string{"### Plano revisado (`infra`)", "- `aws_instance.web`", "(2)"} {
		if !strings.Contains(corpo, trecho) {
			t.Errorf("comentário deveria conter %q:\n%s", trecho, corpo)
		}
	}
	got, id, err := Find([]Comment{comentario(9, BotLogin, corpo)}, "infra")
	if err != nil || got == nil || id != 9 {
		t.Fatalf("Find = %v, %d, %v", got, id, err)
	}
	if !reflect.DeepEqual(*got, r) {
		t.Errorf("Find = %+v, quer %+v", *got, r)
	}
}

func TestEncodeSemDestruicao(t *testing.T) {
	corpo, err := EncodeComment(Reviewed{Dir: "."})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(corpo, "Nenhum recurso será destruído.") || !strings.Contains(corpo, `"destroy":[]`) {
		t.Errorf("corpo inesperado:\n%s", corpo)
	}
}

// Um endereço com "-->" não pode fechar o comentário HTML antes da hora.
func TestEnderecoComSetaNaoQuebraOMarcador(t *testing.T) {
	r := Reviewed{Dir: ".", Destroy: []string{`aws_x.y["a-->b"]`}}
	corpo, err := EncodeComment(r)
	if err != nil {
		t.Fatal(err)
	}
	// A lista visível pode mostrar o endereço cru (está entre crases, fora de
	// comentário HTML). O que importa é o marcador: só o fechamento dele tem "-->".
	marcador := corpo[strings.Index(corpo, markerStart):]
	if n := strings.Count(marcador, "-->"); n != 1 {
		t.Fatalf("o marcador deveria ter só o fechamento, veio %d ocorrências de -->:\n%s", n, marcador)
	}
	got, _, err := Find([]Comment{comentario(1, BotLogin, corpo)}, ".")
	if err != nil || got == nil || !reflect.DeepEqual(got.Destroy, r.Destroy) {
		t.Fatalf("ida e volta falhou: %v %v", got, err)
	}
}

func TestFindIgnoraQuemNaoEOBot(t *testing.T) {
	corpo, _ := EncodeComment(Reviewed{Dir: ".", Destroy: []string{"tudo.revisado"}})
	got, id, err := Find([]Comment{comentario(5, "mallory", corpo)}, ".")
	if err != nil || got != nil || id != 0 {
		t.Fatalf("comentário forjado foi aceito: %v %d %v", got, id, err)
	}
}

func TestFindEscolheODiretorioEOMaisRecente(t *testing.T) {
	antigo, _ := EncodeComment(Reviewed{Dir: ".", Destroy: []string{"a.velho"}})
	novo, _ := EncodeComment(Reviewed{Dir: ".", Destroy: []string{"a.novo"}})
	outro, _ := EncodeComment(Reviewed{Dir: "infra", Destroy: []string{"b.outro"}})
	cs := []Comment{
		comentario(1, BotLogin, antigo),
		comentario(2, BotLogin, outro),
		comentario(3, "maria", "ok"),
		comentario(4, BotLogin, novo),
	}
	got, id, err := Find(cs, ".")
	if err != nil || got == nil || id != 4 || !reflect.DeepEqual(got.Destroy, []string{"a.novo"}) {
		t.Fatalf("Find(.) = %v, %d, %v", got, id, err)
	}
	got, id, _ = Find(cs, "infra")
	if got == nil || id != 2 {
		t.Fatalf("Find(infra) = %v, %d", got, id)
	}
	if got, id, _ = Find(cs, "nao-existe"); got != nil || id != 0 {
		t.Fatalf("Find(nao-existe) = %v, %d", got, id)
	}
}

// Marcador do bot ilegível é erro: cair para um comentário mais antigo
// compararia contra um plano que já não vale.
func TestFindMarcadorIlegivelDoBotEErro(t *testing.T) {
	valido, _ := EncodeComment(Reviewed{Dir: ".", Destroy: []string{"a.b"}})
	cs := []Comment{
		comentario(1, BotLogin, valido),
		comentario(2, BotLogin, "<!-- cicd:revisado {\"dir\": -->"),
	}
	if _, _, err := Find(cs, "."); err == nil {
		t.Fatal("esperava erro")
	}
	if _, _, err := Find([]Comment{comentario(3, BotLogin, "<!-- cicd:revisado {sem fechar")}, "."); err == nil {
		t.Fatal("esperava erro para marcador sem fechamento")
	}
}

func TestLoadComments(t *testing.T) {
	abre := func(nome string) []Comment {
		f, err := os.Open(filepath.Join("..", "..", "testdata", "comments", nome))
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		cs, err := LoadComments(f)
		if err != nil {
			t.Fatal(err)
		}
		return cs
	}
	if got := len(abre("revisado-instance-web.json")); got != 2 {
		t.Errorf("um array: %d comentários, quer 2", got)
	}
	// `gh api --paginate` escreve um array por página.
	paginado := abre("paginado.json")
	if len(paginado) != 2 {
		t.Fatalf("duas páginas: %d comentários, quer 2", len(paginado))
	}
	got, id, err := Find(paginado, ".")
	if err != nil || got == nil || id != 2 {
		t.Fatalf("Find nas páginas = %v, %d, %v", got, id, err)
	}
	if _, err := LoadComments(strings.NewReader(`{"nao":"array"}`)); err == nil {
		t.Error("esperava erro para JSON que não é lista")
	}
}
