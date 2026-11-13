package vuln

import (
	"fmt"
	"io"
	"strings"
	"time"

	"centralizador-cicd/internal/policy"
)

// Accepted é uma vulnerabilidade coberta por uma exceção em vigor.
type Accepted struct {
	Vuln   Vuln
	Ignore Ignore
}

// Result separa as vulnerabilidades por destino.
type Result struct {
	Blocking []Vuln
	Warnings []Vuln
	Accepted []Accepted
	// Expired são as exceções vencidas que ainda casam com uma vulnerabilidade:
	// a vulnerabilidade volta a valer, e a exceção precisa de revisão.
	Expired []Accepted
	// Unused são as exceções que não casam com nada no relatório: podem ser removidas.
	Unused []Ignore
	Total  int
}

// Evaluate aplica a política. `hoje` é um argumento para a conta ser
// reproduzível: a mesma política e o mesmo relatório dão o mesmo resultado.
//
// Regras:
//   - o nível da vulnerabilidade (módulo, pacote, chamada) decide se bloqueia;
//   - exceção em vigor tira a vulnerabilidade do bloqueio, mas ela continua no relatório;
//   - exceção vencida não vale: a vulnerabilidade é avaliada como qualquer outra;
//   - severidade "aviso" nunca bloqueia.
func Evaluate(rep *Report, pol Policy, hoje time.Time) Result {
	r := Result{Total: len(rep.Vulns)}
	used := map[string]bool{}
	hoje = time.Date(hoje.Year(), hoje.Month(), hoje.Day(), 0, 0, 0, 0, time.UTC)
	for _, v := range rep.Vulns {
		var covered *Ignore
		for i := range pol.Ignore {
			ig := pol.Ignore[i]
			if !ig.match(v) {
				continue
			}
			used[ig.ID] = true
			if hoje.After(ig.until) {
				r.Expired = append(r.Expired, Accepted{Vuln: v, Ignore: ig})
				continue
			}
			covered = &ig
			break
		}
		switch {
		case covered != nil:
			r.Accepted = append(r.Accepted, Accepted{Vuln: v, Ignore: *covered})
		case pol.Severity == policy.Obrigatoria && pol.blocks(v.Level):
			r.Blocking = append(r.Blocking, v)
		default:
			r.Warnings = append(r.Warnings, v)
		}
	}
	for _, ig := range pol.Ignore {
		if !used[ig.ID] {
			r.Unused = append(r.Unused, ig)
		}
	}
	return r
}

// ExitCode traduz o resultado: 0 libera, 1 reprova.
func (r Result) ExitCode() int {
	if len(r.Blocking) > 0 {
		return 1
	}
	return 0
}

// Render escreve o relatório em texto.
func (r Result) Render(w io.Writer) {
	if r.Total == 0 {
		fmt.Fprintln(w, "OK: nenhuma vulnerabilidade conhecida nas dependências.")
		return
	}
	vulns(w, "BLOQUEADO", "vulnerabilidade alcançada pelo código", r.Blocking)
	vulns(w, "AVISO", "vulnerabilidade sem bloqueio", r.Warnings)
	if len(r.Accepted) > 0 {
		fmt.Fprintf(w, "\n[IGNORADO] exceção em vigor (%d)\n", len(r.Accepted))
		for _, a := range r.Accepted {
			fmt.Fprintf(w, "  - %s até %s: %s\n", a.Vuln.ID, a.Ignore.Expires, a.Ignore.Reason)
		}
	}
	if len(r.Expired) > 0 {
		fmt.Fprintf(w, "\n[EXCEÇÃO VENCIDA] a exceção não vale mais; revise (%d)\n", len(r.Expired))
		for _, a := range r.Expired {
			fmt.Fprintf(w, "  - %s venceu em %s (%s)\n", a.Vuln.ID, a.Ignore.Expires, a.Ignore.Reason)
		}
	}
	if len(r.Unused) > 0 {
		fmt.Fprintf(w, "\n[EXCEÇÃO SEM USO] não casa com nada no relatório; pode sair da política (%d)\n", len(r.Unused))
		for _, ig := range r.Unused {
			fmt.Fprintf(w, "  - %s\n", ig.ID)
		}
	}
	if len(r.Blocking) > 0 {
		fmt.Fprintln(w, "\nResultado das dependências: REPROVADO. Atualize para a versão corrigida ou registre uma exceção com motivo e prazo.")
		return
	}
	fmt.Fprintln(w, "\nResultado das dependências: liberado.")
}

func vulns(w io.Writer, tag, desc string, vs []Vuln) {
	if len(vs) == 0 {
		return
	}
	fmt.Fprintf(w, "\n[%s] %s (%d)\n", tag, desc, len(vs))
	for _, v := range vs {
		id := v.ID
		if len(v.Aliases) > 0 {
			id += " (" + strings.Join(v.Aliases, ", ") + ")"
		}
		line := fmt.Sprintf("  - %s: %s@%s, nível %s", id, v.Module, v.Version, v.Level)
		if v.Symbol != "" {
			line += " (" + v.Symbol + ")"
		}
		if v.Fixed != "" {
			line += "; corrigido em " + v.Fixed
		}
		fmt.Fprintln(w, line)
		if v.Summary != "" {
			fmt.Fprintln(w, "      "+v.Summary)
		}
	}
}
