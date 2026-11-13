package fakeecs

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func post(t *testing.T, srv *Server, target, body string) (int, string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
	if target != "" {
		req.Header.Set("X-Amz-Target", target)
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

func TestCenarioAvancaPorConsultaERepeteOUltimo(t *testing.T) {
	srv, _ := New("falha")
	for i := 0; i < 4; i++ {
		code, body := post(t, srv, target, `{"cluster":"demo","services":["web"]}`)
		if code != 200 {
			t.Fatalf("consulta %d: %d %s", i, code, body)
		}
		falhou := strings.Contains(body, `"FAILED"`)
		if falhou != (i >= 1) {
			t.Errorf("consulta %d: FAILED=%v", i, falhou)
		}
	}
	if srv.Calls() != 4 {
		t.Errorf("Calls = %d", srv.Calls())
	}
}

func TestSoDescribeServicesEPedidosValidos(t *testing.T) {
	srv, _ := New("sucesso")
	if code, _ := post(t, srv, "", `{}`); code != http.StatusBadRequest {
		t.Errorf("sem X-Amz-Target: %d", code)
	}
	if code, _ := post(t, srv, "AmazonEC2ContainerServiceV20141113.ListServices", `{}`); code != http.StatusBadRequest {
		t.Errorf("outra operação: %d", code)
	}
	if code, _ := post(t, srv, target, `não é json`); code != http.StatusBadRequest {
		t.Errorf("corpo inválido: %d", code)
	}
	if srv.Calls() != 0 {
		t.Errorf("pedido inválido não conta como consulta: %d", srv.Calls())
	}
	// Serviço que não existe: 200 com `failures`, como o ECS de verdade.
	code, body := post(t, srv, target, `{"cluster":"demo","services":["outro"]}`)
	if code != 200 || !strings.Contains(body, `"MISSING"`) || !strings.Contains(body, `"services":[]`) {
		t.Errorf("serviço inexistente: %d %s", code, body)
	}
}

func TestCenarioDesconhecido(t *testing.T) {
	if _, err := New("nao-existe"); err == nil {
		t.Fatal("esperava erro")
	}
	if got := Nomes(); len(got) != 5 {
		t.Errorf("Nomes = %v", got)
	}
}
