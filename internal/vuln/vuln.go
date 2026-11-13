// Package vuln lê o relatório do `govulncheck -format json` e aplica a política
// de dependências.
//
// O govulncheck emite VÁRIOS findings por vulnerabilidade, um por nível de
// alcançabilidade: só o módulo está no build, o pacote vulnerável é importado,
// ou uma função vulnerável é de fato chamada. O que importa para decidir é o
// nível mais profundo: uma vulnerabilidade em módulo que o código nem importa
// não deveria reprovar o pipeline do mesmo jeito que uma função chamada.
package vuln

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Level é a profundidade até onde o código alcança a vulnerabilidade.
type Level int

const (
	// Modulo: o módulo vulnerável está no build, mas nenhum pacote afetado é importado.
	Modulo Level = iota + 1
	// Pacote: um pacote afetado é importado, mas a função vulnerável não é chamada.
	Pacote
	// Chamada: uma função vulnerável é alcançada a partir do código.
	Chamada
)

// Nomes usados na política e no relatório.
func (l Level) String() string {
	switch l {
	case Modulo:
		return "modulo"
	case Pacote:
		return "pacote"
	case Chamada:
		return "chamada"
	}
	return "desconhecido"
}

// ParseLevel converte o nome usado na política.
func ParseLevel(s string) (Level, error) {
	for _, l := range []Level{Modulo, Pacote, Chamada} {
		if l.String() == s {
			return l, nil
		}
	}
	return 0, fmt.Errorf("nível %q desconhecido (use modulo, pacote ou chamada)", s)
}

// Vuln é uma vulnerabilidade, já com o nível mais profundo dos seus findings.
type Vuln struct {
	ID      string   // identificador do banco do Go, como GO-2021-0113
	Aliases []string // CVE, GHSA...
	Summary string
	Module  string
	Version string
	Fixed   string // versão que corrige; vazio se não há
	Level   Level
	// Symbol é a função vulnerável alcançada (só no nível Chamada).
	Symbol string
}

// Report é o resultado do scanner.
type Report struct {
	Scanner string
	Vulns   []Vuln // ordenado por ID
}

type message struct {
	Config  *config  `json:"config"`
	OSV     *osv     `json:"osv"`
	Finding *finding `json:"finding"`
}

type config struct {
	ProtocolVersion string `json:"protocol_version"`
	ScannerName     string `json:"scanner_name"`
	ScannerVersion  string `json:"scanner_version"`
}

type osv struct {
	ID      string   `json:"id"`
	Aliases []string `json:"aliases"`
	Summary string   `json:"summary"`
}

type finding struct {
	OSV          string  `json:"osv"`
	FixedVersion string  `json:"fixed_version"`
	Trace        []frame `json:"trace"`
}

type frame struct {
	Module   string `json:"module"`
	Version  string `json:"version"`
	Package  string `json:"package"`
	Function string `json:"function"`
	Receiver string `json:"receiver"`
}

// Parse lê o fluxo de objetos JSON do govulncheck. Entrada sem a mensagem
// `config` de um govulncheck v1 é erro: um gate que aceita qualquer JSON
// aprovaria por engano, e uma saída vazia de um scanner que falhou não pode
// virar "nenhuma vulnerabilidade".
func Parse(r io.Reader) (*Report, error) {
	dec := json.NewDecoder(r)
	var (
		rep     Report
		saw     bool
		osvs    = map[string]osv{}
		byID    = map[string]*Vuln{}
		scanner string
	)
	for {
		var m message
		err := dec.Decode(&m)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("relatório inválido: %w", err)
		}
		switch {
		case m.Config != nil:
			if m.Config.ScannerName != "govulncheck" || !strings.HasPrefix(m.Config.ProtocolVersion, "v1.") {
				return nil, fmt.Errorf("relatório inválido: scanner %q, protocolo %q (esperado govulncheck v1)",
					m.Config.ScannerName, m.Config.ProtocolVersion)
			}
			saw = true
			scanner = "govulncheck " + m.Config.ScannerVersion
		case m.OSV != nil:
			osvs[m.OSV.ID] = *m.OSV
		case m.Finding != nil:
			f := m.Finding
			if f.OSV == "" || len(f.Trace) == 0 {
				return nil, errors.New("relatório inválido: finding sem osv ou sem trace")
			}
			lvl, sym := levelOf(f.Trace)
			v, ok := byID[f.OSV]
			if !ok {
				v = &Vuln{ID: f.OSV, Module: f.Trace[0].Module, Version: f.Trace[0].Version}
				byID[f.OSV] = v
			}
			if f.FixedVersion != "" {
				v.Fixed = f.FixedVersion
			}
			if lvl > v.Level {
				v.Level = lvl
				v.Symbol = sym
			}
		}
	}
	if !saw {
		return nil, errors.New("relatório inválido: falta a mensagem `config` (use a saída de `govulncheck -format json`)")
	}
	rep.Scanner = scanner
	for id, v := range byID {
		if o, ok := osvs[id]; ok {
			v.Aliases = o.Aliases
			v.Summary = o.Summary
		}
		rep.Vulns = append(rep.Vulns, *v)
	}
	sort.Slice(rep.Vulns, func(i, j int) bool { return rep.Vulns[i].ID < rep.Vulns[j].ID })
	return &rep, nil
}

// levelOf lê o nível de um finding pelo primeiro frame do trace: com função, a
// vulnerabilidade é chamada; só com pacote, importada; só com módulo, presente.
func levelOf(trace []frame) (Level, string) {
	f := trace[0]
	switch {
	case f.Function != "":
		return Chamada, qualified(f)
	case f.Package != "":
		return Pacote, ""
	default:
		return Modulo, ""
	}
}

func qualified(f frame) string {
	pkg := f.Package
	if i := strings.LastIndex(pkg, "/"); i >= 0 {
		pkg = pkg[i+1:]
	}
	name := f.Function
	if f.Receiver != "" {
		name = f.Receiver + "." + f.Function
	}
	if pkg == "" {
		return name
	}
	return pkg + "." + name
}
