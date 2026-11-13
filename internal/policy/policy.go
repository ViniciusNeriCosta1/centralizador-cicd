// Package policy carrega a política do gate de recriação.
//
// A política vive em um arquivo versionado, e não em flags por repositório:
// todo consumidor recebe a mesma regra, e mudar a regra é um PR neste repo.
package policy

import (
	"encoding/json"
	"fmt"
	"os"
)

const (
	// Aviso nunca reprova: só informa.
	Aviso = "aviso"
	// Obrigatoria reprova quando um recurso sensível é recriado sem aprovação.
	Obrigatoria = "obrigatoria"
)

// Policy define o que o gate faz com cada recriação.
type Policy struct {
	Severity       string   `json:"severity"`
	SensitiveTypes []string `json:"sensitive_types"`
	IgnoreTypes    []string `json:"ignore_types"`
}

// Load lê e valida o arquivo. Severidade desconhecida é erro: cair em um
// padrão silencioso tornaria o gate mais frouxo do que quem o configurou quis.
func Load(path string) (Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("política: %w", err)
	}
	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		return Policy{}, fmt.Errorf("política %s: %w", path, err)
	}
	if err := p.Validate(); err != nil {
		return Policy{}, fmt.Errorf("política %s: %w", path, err)
	}
	return p, nil
}

// Validate confere a severidade.
func (p Policy) Validate() error {
	switch p.Severity {
	case Aviso, Obrigatoria:
		return nil
	default:
		return fmt.Errorf("severity %q desconhecida (use %q ou %q)", p.Severity, Aviso, Obrigatoria)
	}
}

// IsSensitive diz se recriar este tipo de recurso deve bloquear.
func (p Policy) IsSensitive(resourceType string) bool {
	return contains(p.SensitiveTypes, resourceType)
}

// IsIgnored diz se este tipo é recriado por natureza e deve ser ignorado.
func (p Policy) IsIgnored(resourceType string) bool { return contains(p.IgnoreTypes, resourceType) }

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
