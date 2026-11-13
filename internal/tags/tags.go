// Package tags confere se os recursos do plano têm as tags obrigatórias.
//
// A conferência é feita no plano, e não no código: as tags efetivas de um
// recurso AWS (`tags_all`) já misturam as do recurso com as do `default_tags`
// do provider, que é o que de fato vai para a nuvem.
package tags

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"centralizador-cicd/internal/plan"
	"centralizador-cicd/internal/policy"
)

// Policy define quais tags são exigidas e o que acontece quando faltam.
type Policy struct {
	Severity     string   `json:"severity"`
	RequiredTags []string `json:"required_tags"`
	IgnoreTypes  []string `json:"ignore_types"`
}

// Load lê e valida a política. Lista de tags vazia é erro: um gate que não
// exige nada passaria sempre, e quem o configurou não quis isso.
func Load(path string) (Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("política de tags: %w", err)
	}
	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		return Policy{}, fmt.Errorf("política de tags %s: %w", path, err)
	}
	if err := p.validate(); err != nil {
		return Policy{}, fmt.Errorf("política de tags %s: %w", path, err)
	}
	return p, nil
}

func (p Policy) validate() error {
	if p.Severity != policy.Aviso && p.Severity != policy.Obrigatoria {
		return fmt.Errorf("severity %q desconhecida (use %q ou %q)", p.Severity, policy.Aviso, policy.Obrigatoria)
	}
	if len(p.RequiredTags) == 0 {
		return fmt.Errorf("required_tags está vazio: o gate não exigiria nada")
	}
	for _, t := range p.RequiredTags {
		if strings.TrimSpace(t) == "" {
			return fmt.Errorf("required_tags tem uma tag com nome vazio")
		}
	}
	return nil
}

func (p Policy) ignored(resourceType string) bool {
	for _, t := range p.IgnoreTypes {
		if t == resourceType {
			return true
		}
	}
	return false
}

// Finding é um recurso fora da política.
type Finding struct {
	Address string
	Type    string
	// Missing lista as tags ausentes (ou com valor vazio).
	Missing []string
	// Unverifiable: as tags só serão conhecidas depois do apply, então o plano
	// não permite conferir.
	Unverifiable bool
}

// Result separa os achados por destino.
type Result struct {
	Blocking []Finding
	Warnings []Finding
	// Checked conta os recursos com suporte a tags que foram conferidos.
	Checked int
}

// Evaluate confere cada recurso gerenciado que será criado ou atualizado.
//
// Regras:
//   - só conta recurso que existirá depois do apply (criar ou atualizar);
//   - recurso sem atributo de tags (`tags_all` ou `tags`) não suporta tags e é pulado;
//   - tag com valor vazio conta como ausente;
//   - tags que só serão conhecidas no apply não podem ser conferidas: é
//     achado, e não aprovação silenciosa;
//   - severidade "obrigatoria" bloqueia; "aviso" só informa.
func Evaluate(p *plan.Plan, pol Policy) Result {
	var r Result
	for _, rc := range p.ResourceChanges {
		if rc.Mode != "managed" || !rc.Change.Writes() || pol.ignored(rc.Type) {
			continue
		}
		field := tagField(rc.Change)
		if field == "" {
			continue
		}
		r.Checked++
		f := Finding{Address: rc.Address, Type: rc.Type}
		if rc.Change.AfterUnknown[field] == true {
			f.Unverifiable = true
		} else {
			f.Missing = missing(rc.Change, field, pol.RequiredTags)
			if len(f.Missing) == 0 {
				continue
			}
		}
		if pol.Severity == policy.Obrigatoria {
			r.Blocking = append(r.Blocking, f)
		} else {
			r.Warnings = append(r.Warnings, f)
		}
	}
	sort.Slice(r.Blocking, func(i, j int) bool { return r.Blocking[i].Address < r.Blocking[j].Address })
	sort.Slice(r.Warnings, func(i, j int) bool { return r.Warnings[i].Address < r.Warnings[j].Address })
	return r
}

// tagField devolve o atributo de tags do recurso: `tags_all` (as tags efetivas,
// com o default_tags do provider) quando existe, senão `tags`. Vazio = o
// recurso não suporta tags.
func tagField(c plan.Change) string {
	for _, f := range []string{"tags_all", "tags"} {
		if _, ok := c.After[f]; ok {
			return f
		}
		if _, ok := c.AfterUnknown[f]; ok {
			return f
		}
	}
	return ""
}

func missing(c plan.Change, field string, required []string) []string {
	known, _ := c.After[field].(map[string]any)
	unknownKeys, _ := c.AfterUnknown[field].(map[string]any)
	var out []string
	for _, tag := range required {
		if v, ok := known[tag].(string); ok && strings.TrimSpace(v) != "" {
			continue
		}
		// O valor ainda não é conhecido, mas a chave existe.
		if unknownKeys[tag] == true {
			continue
		}
		out = append(out, tag)
	}
	return out
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
	if r.Checked == 0 {
		fmt.Fprintln(w, "OK: nenhum recurso com suporte a tags será criado ou atualizado.")
		return
	}
	if len(r.Blocking) == 0 && len(r.Warnings) == 0 {
		fmt.Fprintf(w, "OK: %d recurso(s) com as tags obrigatórias.\n", r.Checked)
		return
	}
	section(w, "BLOQUEADO", "recurso sem as tags obrigatórias", r.Blocking)
	section(w, "AVISO", "recurso sem as tags obrigatórias (sem bloqueio)", r.Warnings)
	if len(r.Blocking) > 0 {
		fmt.Fprintln(w, "\nResultado das tags: REPROVADO. Adicione as tags no recurso ou no default_tags do provider.")
		return
	}
	fmt.Fprintln(w, "\nResultado das tags: liberado.")
}

func section(w io.Writer, tag, desc string, fs []Finding) {
	if len(fs) == 0 {
		return
	}
	fmt.Fprintf(w, "\n[%s] %s (%d)\n", tag, desc, len(fs))
	for _, f := range fs {
		if f.Unverifiable {
			fmt.Fprintf(w, "  - %s (%s): tags só serão conhecidas depois do apply (não verificável)\n", f.Address, f.Type)
			continue
		}
		fmt.Fprintf(w, "  - %s (%s): falta %s\n", f.Address, f.Type, strings.Join(f.Missing, ", "))
	}
}
