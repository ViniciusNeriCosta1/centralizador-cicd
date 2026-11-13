package delta

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// BotLogin é o autor dos comentários que o CI publica com o GITHUB_TOKEN.
// Só comentários dele valem como referência: qualquer pessoa pode comentar em
// um pull request, e um comentário forjado com o conjunto "tudo revisado"
// liberaria qualquer destruição.
const BotLogin = "github-actions[bot]"

const (
	markerStart = "<!-- cicd:revisado "
	markerEnd   = " -->"
)

// Comment é o recorte de um comentário da API do GitHub que interessa aqui.
type Comment struct {
	ID   int64 `json:"id"`
	User struct {
		Login string `json:"login"`
	} `json:"user"`
	Body string `json:"body"`
}

// EncodeComment monta o comentário do pull request: um resumo legível e o
// conjunto revisado em um marcador invisível.
//
// json.Marshal escapa "<", ">" e "&", então o payload nunca contém "-->" e não
// consegue fechar o comentário HTML antes da hora.
func EncodeComment(r Reviewed) (string, error) {
	if r.Destroy == nil {
		r.Destroy = []string{}
	}
	payload, err := json.Marshal(r)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	fmt.Fprintf(&b, "### Plano revisado (`%s`)\n\n", r.Dir)
	if len(r.Destroy) == 0 {
		b.WriteString("Nenhum recurso será destruído.\n")
	} else {
		fmt.Fprintf(&b, "Recursos que serão destruídos (%d):\n\n", len(r.Destroy))
		for _, a := range r.Destroy {
			fmt.Fprintf(&b, "- `%s`\n", a)
		}
	}
	b.WriteString("\nO apply compara a sua destruição com esta lista; o que não estiver aqui é bloqueado.\n\n")
	b.WriteString(markerStart + string(payload) + markerEnd + "\n")
	return b.String(), nil
}

// decodeMarker extrai o conjunto revisado de um comentário. ok é false quando
// o comentário não tem marcador.
func decodeMarker(body string) (r Reviewed, ok bool, err error) {
	i := strings.Index(body, markerStart)
	if i < 0 {
		return r, false, nil
	}
	rest := body[i+len(markerStart):]
	j := strings.Index(rest, markerEnd)
	if j < 0 {
		return r, true, fmt.Errorf("marcador sem fechamento")
	}
	if err := json.Unmarshal([]byte(rest[:j]), &r); err != nil {
		return r, true, fmt.Errorf("marcador ilegível: %w", err)
	}
	return r, true, nil
}

// Find devolve o conjunto revisado mais recente publicado pelo bot para o
// diretório dir, e o id do comentário (para atualizá-lo em vez de criar outro).
//
// Devolve nil quando não há referência. Comentário do bot com marcador
// ilegível é erro, e não "sem referência": cair para um comentário mais antigo
// compararia contra um plano que já não vale.
func Find(comments []Comment, dir string) (*Reviewed, int64, error) {
	var found *Reviewed
	var id int64
	for _, c := range comments {
		if c.User.Login != BotLogin {
			continue
		}
		r, ok, err := decodeMarker(c.Body)
		if !ok {
			continue
		}
		if err != nil {
			return nil, 0, fmt.Errorf("comentário %d: %w", c.ID, err)
		}
		if r.Dir != dir {
			continue
		}
		rr := r
		found, id = &rr, c.ID
	}
	return found, id, nil
}

// LoadComments lê comentários em JSON. Aceita um array ou vários arrays em
// sequência, que é o que `gh api --paginate` escreve quando há mais de uma página.
func LoadComments(r io.Reader) ([]Comment, error) {
	dec := json.NewDecoder(r)
	var all []Comment
	for {
		var page []Comment
		err := dec.Decode(&page)
		if err == io.EOF {
			return all, nil
		}
		if err != nil {
			return nil, fmt.Errorf("comentários inválidos: %w", err)
		}
		all = append(all, page...)
	}
}
