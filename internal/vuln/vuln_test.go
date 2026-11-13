package vuln

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func abre(t *testing.T, rel string) *Report {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", rel))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	r, err := Parse(f)
	if err != nil {
		t.Fatalf("Parse(%s): %v", rel, err)
	}
	return r
}

func acha(t *testing.T, r *Report, id string) Vuln {
	t.Helper()
	for _, v := range r.Vulns {
		if v.ID == id {
			return v
		}
	}
	t.Fatalf("%s não está no relatório: %+v", id, r.Vulns)
	return Vuln{}
}

// Relatórios produzidos pelo govulncheck de verdade (scripts/gerar-relatorios-vuln.sh).
// Cada vulnerabilidade aparece em VÁRIOS findings (módulo, pacote, função); o
// nível que vale é o mais profundo.
func TestRelatorioRealComFuncaoChamada(t *testing.T) {
	r := abre(t, "real/vuln-chamada.json")
	if !strings.HasPrefix(r.Scanner, "govulncheck v") {
		t.Errorf("Scanner = %q", r.Scanner)
	}
	v := acha(t, r, "GO-2021-0113")
	if v.Level != Chamada {
		t.Fatalf("nível = %s, quer chamada (os findings de módulo e pacote não podem rebaixar)", v.Level)
	}
	if v.Symbol != "language.ParseAcceptLanguage" {
		t.Errorf("Symbol = %q", v.Symbol)
	}
	if v.Module != "golang.org/x/text" || v.Version != "v0.3.6" || v.Fixed != "v0.3.7" {
		t.Errorf("módulo/versão/correção = %s %s %s", v.Module, v.Version, v.Fixed)
	}
	if !reflect.DeepEqual(v.Aliases, []string{"CVE-2021-38561", "GHSA-ppp9-7jff-5vj2"}) || v.Summary == "" {
		t.Errorf("aliases/resumo vêm da mensagem osv: %v %q", v.Aliases, v.Summary)
	}
}

func TestRelatorioRealSoComPacoteImportado(t *testing.T) {
	r := abre(t, "real/vuln-pacote.json")
	if got := acha(t, r, "GO-2021-0113").Level; got != Pacote {
		t.Errorf("GO-2021-0113 = %s, quer pacote", got)
	}
	// Vulnerabilidade em módulo que o código nem importa por esse caminho.
	if got := acha(t, r, "GO-2026-5970").Level; got != Modulo {
		t.Errorf("GO-2026-5970 = %s, quer modulo", got)
	}
	for _, v := range r.Vulns {
		if v.Level == Chamada {
			t.Errorf("nada deveria ser chamado neste relatório: %+v", v)
		}
	}
}

func TestParseRejeitaEntradaQueNaoERelatorio(t *testing.T) {
	casos := map[string]string{
		"vazio (scanner que falhou)": ``,
		"json quebrado":              `{"config":`,
		"sem config":                 `{"finding":{"osv":"GO-1","trace":[{"module":"m"}]}}`,
		"outro scanner":              `{"config":{"scanner_name":"npm","protocol_version":"v1.0.0"}}`,
		"protocolo desconhecido":     `{"config":{"scanner_name":"govulncheck","protocol_version":"v2.0.0"}}`,
		"finding sem trace":          `{"config":{"scanner_name":"govulncheck","protocol_version":"v1.0.0"}}{"finding":{"osv":"GO-1","trace":[]}}`,
		"finding sem osv":            `{"config":{"scanner_name":"govulncheck","protocol_version":"v1.0.0"}}{"finding":{"trace":[{"module":"m"}]}}`,
	}
	for nome, in := range casos {
		t.Run(nome, func(t *testing.T) {
			if _, err := Parse(strings.NewReader(in)); err == nil {
				t.Fatal("esperava erro: um relatório vazio ou estranho não pode virar \"sem vulnerabilidades\"")
			}
		})
	}
}

func TestParseSemFindingsEhLimpo(t *testing.T) {
	r, err := Parse(strings.NewReader(`{"config":{"scanner_name":"govulncheck","protocol_version":"v1.0.0","scanner_version":"v1.9.0"}}` + "\n" + `{"progress":{"message":"ok"}}`))
	if err != nil || len(r.Vulns) != 0 {
		t.Fatalf("Parse = %+v, %v", r, err)
	}
}

func TestFindingSemMensagemOSVAindaEntra(t *testing.T) {
	r, err := Parse(strings.NewReader(`{"config":{"scanner_name":"govulncheck","protocol_version":"v1.0.0"}}` + "\n" +
		`{"finding":{"osv":"GO-9","fixed_version":"v2","trace":[{"module":"m","version":"v1","package":"m/p","function":"F","receiver":"T"}]}}`))
	if err != nil || len(r.Vulns) != 1 {
		t.Fatalf("Parse = %+v, %v", r, err)
	}
	if v := r.Vulns[0]; v.Level != Chamada || v.Symbol != "p.T.F" || v.Summary != "" {
		t.Errorf("vuln = %+v", v)
	}
}

func escrevePolitica(t *testing.T, conteudo string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "dep.json")
	if err := os.WriteFile(p, []byte(conteudo), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadPolitica(t *testing.T) {
	if _, err := Load(filepath.Join("..", "..", "config", "dependency-policy.json")); err != nil {
		t.Fatalf("a política que acompanha a ferramenta precisa carregar: %v", err)
	}
	ruins := map[string]string{
		"severidade desconhecida": `{"severity":"talvez","block_on":["chamada"]}`,
		"block_on vazio":          `{"severity":"obrigatoria","block_on":[]}`,
		"nível desconhecido":      `{"severity":"obrigatoria","block_on":["funcao"]}`,
		"exceção sem motivo":      `{"severity":"obrigatoria","block_on":["chamada"],"ignore":[{"id":"GO-1","expires":"2027-01-01"}]}`,
		"exceção sem prazo":       `{"severity":"obrigatoria","block_on":["chamada"],"ignore":[{"id":"GO-1","reason":"x"}]}`,
		"prazo mal escrito":       `{"severity":"obrigatoria","block_on":["chamada"],"ignore":[{"id":"GO-1","reason":"x","expires":"01/01/2027"}]}`,
		"exceção sem id":          `{"severity":"obrigatoria","block_on":["chamada"],"ignore":[{"reason":"x","expires":"2027-01-01"}]}`,
		"id repetido":             `{"severity":"obrigatoria","block_on":["chamada"],"ignore":[{"id":"GO-1","reason":"x","expires":"2027-01-01"},{"id":"GO-1","reason":"y","expires":"2027-02-01"}]}`,
		"json quebrado":           `{"severity":`,
	}
	for nome, c := range ruins {
		t.Run(nome, func(t *testing.T) {
			if _, err := Load(escrevePolitica(t, c)); err == nil {
				t.Fatal("esperava erro")
			}
		})
	}
	if _, err := Load("nao-existe.json"); err == nil {
		t.Fatal("esperava erro para arquivo inexistente")
	}
}

func dia(s string) time.Time {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		panic(err)
	}
	return t
}

func politica(t *testing.T, conteudo string) Policy {
	t.Helper()
	p, err := Load(escrevePolitica(t, conteudo))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func ids(vs []Vuln) []string {
	out := []string{}
	for _, v := range vs {
		out = append(out, v.ID)
	}
	return out
}

func TestEvaluatePeloNivel(t *testing.T) {
	rep := &Report{Vulns: []Vuln{
		{ID: "GO-1", Level: Chamada},
		{ID: "GO-2", Level: Pacote},
		{ID: "GO-3", Level: Modulo},
	}}
	casos := []struct {
		nome     string
		pol      string
		bloqueia []string
		avisa    []string
	}{
		{"padrão: só chamada bloqueia", `{"severity":"obrigatoria","block_on":["chamada"]}`, []string{"GO-1"}, []string{"GO-2", "GO-3"}},
		{"pacote também", `{"severity":"obrigatoria","block_on":["chamada","pacote"]}`, []string{"GO-1", "GO-2"}, []string{"GO-3"}},
		{"tudo bloqueia", `{"severity":"obrigatoria","block_on":["chamada","pacote","modulo"]}`, []string{"GO-1", "GO-2", "GO-3"}, []string{}},
		{"aviso nunca bloqueia", `{"severity":"aviso","block_on":["chamada","pacote","modulo"]}`, []string{}, []string{"GO-1", "GO-2", "GO-3"}},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			r := Evaluate(rep, politica(t, c.pol), dia("2026-11-13"))
			if got := ids(r.Blocking); !reflect.DeepEqual(got, c.bloqueia) {
				t.Errorf("Blocking = %v, quer %v", got, c.bloqueia)
			}
			if got := ids(r.Warnings); !reflect.DeepEqual(got, c.avisa) {
				t.Errorf("Warnings = %v, quer %v", got, c.avisa)
			}
			if (len(c.bloqueia) > 0) != (r.ExitCode() == 1) {
				t.Errorf("ExitCode = %d", r.ExitCode())
			}
		})
	}
}

func TestExcecoes(t *testing.T) {
	rep := &Report{Vulns: []Vuln{
		{ID: "GO-1", Aliases: []string{"CVE-2026-0001"}, Level: Chamada},
		{ID: "GO-2", Level: Chamada},
	}}
	pol := politica(t, `{"severity":"obrigatoria","block_on":["chamada"],"ignore":[
		{"id":"CVE-2026-0001","reason":"usa só em teste","expires":"2026-12-31"},
		{"id":"GO-2","reason":"vai sair na próxima release","expires":"2026-11-12"},
		{"id":"GO-404","reason":"já corrigida","expires":"2027-06-30"}]}`)

	r := Evaluate(rep, pol, dia("2026-11-13"))
	// GO-1: coberta por um alias em vigor. GO-2: exceção vencida volta a bloquear.
	if got := ids(r.Blocking); !reflect.DeepEqual(got, []string{"GO-2"}) {
		t.Errorf("Blocking = %v", got)
	}
	if len(r.Accepted) != 1 || r.Accepted[0].Vuln.ID != "GO-1" || r.Accepted[0].Ignore.Reason != "usa só em teste" {
		t.Errorf("Accepted = %+v", r.Accepted)
	}
	if len(r.Expired) != 1 || r.Expired[0].Vuln.ID != "GO-2" {
		t.Errorf("Expired = %+v", r.Expired)
	}
	if len(r.Unused) != 1 || r.Unused[0].ID != "GO-404" {
		t.Errorf("Unused = %+v", r.Unused)
	}
	if r.ExitCode() != 1 {
		t.Errorf("ExitCode = %d, quer 1 (a exceção vencida não libera)", r.ExitCode())
	}

	// O prazo vale até o fim do dia: no dia do vencimento ainda cobre.
	r = Evaluate(rep, pol, dia("2026-11-12"))
	if len(r.Blocking) != 0 || len(r.Accepted) != 2 {
		t.Errorf("no último dia: Blocking=%v Accepted=%d", ids(r.Blocking), len(r.Accepted))
	}
	// Um instante no meio do dia conta como o mesmo dia.
	r = Evaluate(rep, pol, time.Date(2026, 11, 12, 23, 59, 0, 0, time.UTC))
	if len(r.Blocking) != 0 {
		t.Errorf("às 23:59 do último dia: Blocking=%v", ids(r.Blocking))
	}
}

func TestRender(t *testing.T) {
	var b bytes.Buffer
	Evaluate(&Report{}, politica(t, `{"severity":"obrigatoria","block_on":["chamada"]}`), dia("2026-11-13")).Render(&b)
	if !strings.Contains(b.String(), "OK: nenhuma vulnerabilidade") {
		t.Errorf("sem vulnerabilidades: %q", b.String())
	}
	b.Reset()
	rep := abre(t, "real/vuln-chamada.json")
	Evaluate(rep, politica(t, `{"severity":"obrigatoria","block_on":["chamada"]}`), dia("2026-11-13")).Render(&b)
	for _, tr := range []string{"BLOQUEADO", "GO-2021-0113 (CVE-2021-38561, GHSA-ppp9-7jff-5vj2)", "golang.org/x/text@v0.3.6", "nível chamada (language.ParseAcceptLanguage)", "corrigido em v0.3.7", "REPROVADO"} {
		if !strings.Contains(b.String(), tr) {
			t.Errorf("saída deveria conter %q:\n%s", tr, b.String())
		}
	}
}
