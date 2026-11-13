// Package gate decide o que fazer com as recriações encontradas em um plano.
package gate

import (
	"fmt"
	"io"
	"strings"

	"centralizador-cicd/internal/plan"
	"centralizador-cicd/internal/policy"
)

// Finding é um recurso que será recriado.
type Finding struct {
	Address             string
	Type                string
	Attributes          []string // atributos que forçam a recriação
	CreateBeforeDestroy bool
}

// Result separa as recriações por destino.
type Result struct {
	Blocking []Finding // reprovam o gate
	Approved []Finding // sensíveis, liberados por aprovação explícita
	Warnings []Finding // recriações sem bloqueio (tipo comum ou severidade "aviso")
	Ignored  []Finding // tipos recriados por natureza (ex.: task definition do ECS)
}

// Evaluate aplica a política ao plano.
//
// Regras:
//   - só recursos gerenciados (mode "managed") contam; data sources não;
//   - destruir sem recriar não é recriação (é outro gate);
//   - tipo ignorado é registrado, mas nunca avisa nem bloqueia;
//   - tipo sensível bloqueia quando a severidade é "obrigatoria" e não há aprovação.
func Evaluate(p *plan.Plan, pol policy.Policy, approved bool) Result {
	var r Result
	for _, rc := range p.ResourceChanges {
		if rc.Mode != "managed" || !rc.Change.IsReplacement() {
			continue
		}
		f := Finding{
			Address:             rc.Address,
			Type:                rc.Type,
			Attributes:          rc.Change.ReplaceAttributes(),
			CreateBeforeDestroy: rc.Change.CreateBeforeDestroy(),
		}
		switch {
		case pol.IsIgnored(rc.Type):
			r.Ignored = append(r.Ignored, f)
		case pol.IsSensitive(rc.Type) && pol.Severity == policy.Obrigatoria:
			if approved {
				r.Approved = append(r.Approved, f)
			} else {
				r.Blocking = append(r.Blocking, f)
			}
		default:
			r.Warnings = append(r.Warnings, f)
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
	total := len(r.Blocking) + len(r.Approved) + len(r.Warnings) + len(r.Ignored)
	if total == 0 {
		fmt.Fprintln(w, "OK: nenhum recurso será recriado.")
		return
	}
	section(w, "BLOQUEADO", "recriação de recurso sensível sem aprovação", r.Blocking)
	section(w, "APROVADO", "recriação de recurso sensível com aprovação explícita", r.Approved)
	section(w, "AVISO", "recriação sem bloqueio", r.Warnings)
	section(w, "IGNORADO", "tipo recriado por natureza", r.Ignored)
	if len(r.Blocking) > 0 {
		fmt.Fprintln(w, "\nResultado: REPROVADO. Para seguir, aprove a destruição de forma explícita (flag -approved).")
		return
	}
	fmt.Fprintln(w, "\nResultado: liberado.")
}

func section(w io.Writer, tag, desc string, fs []Finding) {
	if len(fs) == 0 {
		return
	}
	fmt.Fprintf(w, "\n[%s] %s (%d)\n", tag, desc, len(fs))
	for _, f := range fs {
		line := fmt.Sprintf("  - %s (%s)", f.Address, f.Type)
		if len(f.Attributes) > 0 {
			line += " por mudança em: " + strings.Join(f.Attributes, ", ")
		}
		if f.CreateBeforeDestroy {
			line += " [create_before_destroy]"
		}
		fmt.Fprintln(w, line)
	}
}
