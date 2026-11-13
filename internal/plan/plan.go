// Package plan lê a saída de `terraform show -json <plano>`.
//
// O resumo em texto do `terraform plan` não distingue "destruir" de "recriar".
// O JSON distingue: uma recriação aparece como as ações ["delete","create"]
// (ou ["create","delete"] quando o recurso usa create_before_destroy).
package plan

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
)

// Plan é o recorte do JSON do plano que o gate usa.
type Plan struct {
	FormatVersion    string           `json:"format_version"`
	TerraformVersion string           `json:"terraform_version"`
	ResourceChanges  []ResourceChange `json:"resource_changes"`
}

// ResourceChange descreve a mudança prevista para um recurso.
type ResourceChange struct {
	Address string `json:"address"`
	Mode    string `json:"mode"` // "managed" ou "data"
	Type    string `json:"type"`
	Name    string `json:"name"`
	Change  Change `json:"change"`
}

// Change guarda as ações e os atributos que forçam a recriação.
type Change struct {
	Actions []string `json:"actions"`
	// ReplacePaths lista os atributos que não podem ser alterados no lugar.
	// Cada caminho é uma sequência de nomes (string) e índices (número).
	ReplacePaths [][]any `json:"replace_paths"`
	// After é o estado previsto do recurso (nulo quando ele só é destruído).
	After map[string]any `json:"after"`
	// AfterUnknown marca o que só será conhecido depois do apply: true para um
	// atributo inteiro, ou um objeto com true nas chaves ainda desconhecidas.
	AfterUnknown map[string]any `json:"after_unknown"`
}

// Parse lê um plano em JSON. Entrada que não parece um plano é erro:
// um gate que aceita qualquer JSON aprovaria por engano.
func Parse(r io.Reader) (*Plan, error) {
	var p Plan
	if err := json.NewDecoder(r).Decode(&p); err != nil {
		return nil, fmt.Errorf("plano inválido: %w", err)
	}
	if p.FormatVersion == "" {
		return nil, errors.New("plano inválido: falta format_version (use a saída de `terraform show -json <plano>`)")
	}
	return &p, nil
}

// Destroyed lista os endereços dos recursos gerenciados que serão destruídos,
// inclusive os que serão recriados (a recriação destrói o antigo). Sem
// repetição e em ordem alfabética, para a comparação ser estável.
func (p *Plan) Destroyed() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, rc := range p.ResourceChanges {
		if rc.Mode != "managed" || !rc.Change.destroys() || seen[rc.Address] {
			continue
		}
		seen[rc.Address] = true
		out = append(out, rc.Address)
	}
	sort.Strings(out)
	return out
}

func (c Change) destroys() bool {
	for _, a := range c.Actions {
		if a == "delete" {
			return true
		}
	}
	return false
}

// Writes diz se o recurso existirá depois do apply (criado ou atualizado).
func (c Change) Writes() bool {
	for _, a := range c.Actions {
		if a == "create" || a == "update" {
			return true
		}
	}
	return false
}

// IsReplacement diz se o recurso será destruído e recriado.
func (c Change) IsReplacement() bool {
	var del, create bool
	for _, a := range c.Actions {
		switch a {
		case "delete":
			del = true
		case "create":
			create = true
		}
	}
	return del && create
}

// CreateBeforeDestroy diz se a recriação cria o novo antes de apagar o antigo.
func (c Change) CreateBeforeDestroy() bool {
	return c.IsReplacement() && len(c.Actions) > 0 && c.Actions[0] == "create"
}

// ReplaceAttributes devolve os atributos que forçam a recriação, como "ami"
// ou "ingress.0.cidr_blocks".
func (c Change) ReplaceAttributes() []string {
	out := make([]string, 0, len(c.ReplacePaths))
	for _, path := range c.ReplacePaths {
		parts := make([]string, 0, len(path))
		for _, seg := range path {
			switch v := seg.(type) {
			case string:
				parts = append(parts, v)
			case float64:
				parts = append(parts, fmt.Sprintf("%d", int(v)))
			default:
				parts = append(parts, fmt.Sprint(v))
			}
		}
		out = append(out, strings.Join(parts, "."))
	}
	return out
}
