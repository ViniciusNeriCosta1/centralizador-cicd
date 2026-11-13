// Package delta compara o que o apply vai destruir com o que foi revisado no
// pull request.
//
// Revisar um plano no PR só vale se o apply executar o que foi revisado. Entre
// a revisão e o merge o estado da infraestrutura pode mudar, e o plano do apply
// pode destruir algo que ninguém viu. O delta é essa diferença.
package delta

import (
	"fmt"
	"io"
	"sort"
)

// Status resume a comparação.
type Status string

const (
	// NadaADestruir: o apply não destrói nada. Libera sem precisar de referência.
	NadaADestruir Status = "nada-a-destruir"
	// DentroDoRevisado: tudo que será destruído já estava no plano revisado.
	DentroDoRevisado Status = "dentro-do-revisado"
	// ForaDoRevisado: o apply destrói algo que o plano revisado não previa.
	ForaDoRevisado Status = "fora-do-revisado"
	// SemReferencia: há destruição, mas não existe plano revisado para comparar.
	// Sem referência não há como provar que foi revisado, então bloqueia.
	SemReferencia Status = "sem-referencia"
)

// Reviewed é o conjunto de destruições que o revisor viu no pull request.
type Reviewed struct {
	Dir     string   `json:"dir"`
	Destroy []string `json:"destroy"`
}

// Result é o resultado da comparação.
type Result struct {
	Status     Status
	Unreviewed []string // destruições que o plano revisado não previa
}

// Blocking diz se o status deve reprovar o apply.
func (r Result) Blocking() bool {
	return r.Status == ForaDoRevisado || r.Status == SemReferencia
}

// Compare compara as destruições do apply com as revisadas. reviewed nil
// significa que não há referência.
//
// Destruir MENOS do que foi revisado é seguro e libera.
func Compare(destroying []string, reviewed *Reviewed) Result {
	if len(destroying) == 0 {
		return Result{Status: NadaADestruir}
	}
	if reviewed == nil {
		return Result{Status: SemReferencia, Unreviewed: sorted(destroying)}
	}
	known := make(map[string]bool, len(reviewed.Destroy))
	for _, a := range reviewed.Destroy {
		known[a] = true
	}
	var un []string
	for _, a := range destroying {
		if !known[a] {
			un = append(un, a)
		}
	}
	if len(un) > 0 {
		return Result{Status: ForaDoRevisado, Unreviewed: sorted(un)}
	}
	return Result{Status: DentroDoRevisado}
}

func sorted(in []string) []string {
	out := append([]string(nil), in...)
	sort.Strings(out)
	return out
}

// Render escreve o resultado em texto. approved indica que um humano liberou
// explicitamente, mesmo com o bloqueio.
func (r Result) Render(w io.Writer, approved bool) {
	switch r.Status {
	case NadaADestruir:
		fmt.Fprintln(w, "[DELTA] nada-a-destruir: o apply não destrói nenhum recurso.")
	case DentroDoRevisado:
		fmt.Fprintln(w, "[DELTA] dentro-do-revisado: toda destruição estava no plano revisado.")
	case ForaDoRevisado, SemReferencia:
		tag := "BLOQUEADO"
		if approved {
			tag = "APROVADO"
		}
		motivo := "o apply destrói recursos que o plano revisado não previa"
		if r.Status == SemReferencia {
			motivo = "há destruição e não existe plano revisado para comparar"
		}
		fmt.Fprintf(w, "[DELTA] %s: %s (%s)\n", tag, r.Status, motivo)
		for _, a := range r.Unreviewed {
			fmt.Fprintf(w, "  - %s\n", a)
		}
	}
}
