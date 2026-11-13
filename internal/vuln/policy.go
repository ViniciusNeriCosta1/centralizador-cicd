package vuln

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"centralizador-cicd/internal/policy"
)

// Policy define o que reprova e quais vulnerabilidades têm exceção.
type Policy struct {
	Severity string `json:"severity"`
	// BlockOn lista os níveis que reprovam. O padrão sensato é só "chamada".
	BlockOn []string `json:"block_on"`
	Ignore  []Ignore `json:"ignore"`

	levels []Level
}

// Ignore é uma exceção. Tem motivo e prazo: exceção sem prazo vira dívida que
// ninguém revisita.
type Ignore struct {
	ID      string `json:"id"` // GO-2021-0113, CVE-... ou GHSA-...
	Reason  string `json:"reason"`
	Expires string `json:"expires"` // AAAA-MM-DD, vale até o fim desse dia

	until time.Time
}

const dateLayout = "2006-01-02"

// Load lê e valida a política.
func Load(path string) (Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("política de dependências: %w", err)
	}
	var p Policy
	if err := json.Unmarshal(b, &p); err != nil {
		return Policy{}, fmt.Errorf("política de dependências %s: %w", path, err)
	}
	if err := p.validate(); err != nil {
		return Policy{}, fmt.Errorf("política de dependências %s: %w", path, err)
	}
	return p, nil
}

func (p *Policy) validate() error {
	if p.Severity != policy.Aviso && p.Severity != policy.Obrigatoria {
		return fmt.Errorf("severity %q desconhecida (use %q ou %q)", p.Severity, policy.Aviso, policy.Obrigatoria)
	}
	if len(p.BlockOn) == 0 {
		return fmt.Errorf("block_on está vazio: o gate não bloquearia nada")
	}
	for _, n := range p.BlockOn {
		l, err := ParseLevel(n)
		if err != nil {
			return fmt.Errorf("block_on: %w", err)
		}
		p.levels = append(p.levels, l)
	}
	seen := map[string]bool{}
	for i := range p.Ignore {
		ig := &p.Ignore[i]
		if strings.TrimSpace(ig.ID) == "" {
			return fmt.Errorf("ignore[%d]: falta o id", i)
		}
		if strings.TrimSpace(ig.Reason) == "" {
			return fmt.Errorf("ignore %s: falta o motivo (reason)", ig.ID)
		}
		t, err := time.Parse(dateLayout, ig.Expires)
		if err != nil {
			return fmt.Errorf("ignore %s: expires %q precisa ser AAAA-MM-DD", ig.ID, ig.Expires)
		}
		if seen[ig.ID] {
			return fmt.Errorf("ignore %s: id repetido", ig.ID)
		}
		seen[ig.ID] = true
		ig.until = t
	}
	return nil
}

func (p Policy) blocks(l Level) bool {
	for _, b := range p.levels {
		if b == l {
			return true
		}
	}
	return false
}

// match diz se a exceção cobre a vulnerabilidade, pelo id do Go ou por um alias.
func (ig Ignore) match(v Vuln) bool {
	if ig.ID == v.ID {
		return true
	}
	for _, a := range v.Aliases {
		if ig.ID == a {
			return true
		}
	}
	return false
}
