package ecs

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"centralizador-cicd/internal/fakeecs"
	"centralizador-cicd/internal/rollout"
)

// Credenciais falsas: o SDK exige alguma para assinar a chamada; o simulador ignora.
func ambiente(t *testing.T) {
	t.Helper()
	t.Setenv("AWS_ACCESS_KEY_ID", "falso")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "falso")
	t.Setenv("AWS_SESSION_TOKEN", "")
	t.Setenv("AWS_PROFILE", "")
	t.Setenv("AWS_CONFIG_FILE", "/dev/null")
	t.Setenv("AWS_SHARED_CREDENTIALS_FILE", "/dev/null")
}

func cliente(t *testing.T, cenario, cluster, servico string) (*Client, *fakeecs.Server) {
	t.Helper()
	ambiente(t)
	srv, err := fakeecs.New(cenario)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	c, err := New(context.Background(), "us-east-1", ts.URL, cluster, servico)
	if err != nil {
		t.Fatal(err)
	}
	return c, srv
}

// O SDK real serializa a chamada e interpreta a resposta: se os nomes de campo
// do simulador não batessem com o modelo da API, os valores não chegariam aqui.
func TestDescribeInterpretaARespostaPeloSDK(t *testing.T) {
	c, _ := cliente(t, "sucesso", "demo", "web")
	s, err := c.Describe(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if s.Name != "web" || s.Status != "ACTIVE" || s.DesiredCount != 2 || s.RunningCount != 2 || s.PendingCount != 2 {
		t.Errorf("serviço = %+v", s)
	}
	if len(s.Deployments) != 2 {
		t.Fatalf("deployments = %+v", s.Deployments)
	}
	p := s.Deployments[0]
	if p.Status != "PRIMARY" || p.RolloutState != "IN_PROGRESS" || p.TaskDefinition != fakeecs.TaskDefinitionNova ||
		p.PendingCount != 2 || p.RolloutStateReason == "" {
		t.Errorf("PRIMARY = %+v", p)
	}
}

func TestServicoInexistenteViraErrNotFound(t *testing.T) {
	for _, c := range []struct{ nome, cenario, cluster, servico string }{
		{"cenário inexistente", "inexistente", "demo", "web"},
		{"nome de serviço errado", "sucesso", "demo", "outro"},
		{"cluster errado", "sucesso", "outro", "web"},
	} {
		t.Run(c.nome, func(t *testing.T) {
			cl, _ := cliente(t, c.cenario, c.cluster, c.servico)
			_, err := cl.Describe(context.Background())
			if !errors.Is(err, rollout.ErrNotFound) {
				t.Fatalf("err = %v, quer ErrNotFound", err)
			}
		})
	}
}

func TestSemRegiaoEErro(t *testing.T) {
	ambiente(t)
	t.Setenv("AWS_REGION", "")
	t.Setenv("AWS_DEFAULT_REGION", "")
	if _, err := New(context.Background(), "", "", "demo", "web"); err == nil {
		t.Fatal("esperava erro: sem região o SDK usaria um padrão que ninguém escolheu")
	}
}

// A espera inteira: waiter + SDK + simulador, com intervalo curto.
func TestEsperaDeRolloutContraOSimulador(t *testing.T) {
	casos := []struct {
		nome     string
		cenario  string
		want     string
		timeout  time.Duration
		ok       bool
		timedOut bool
		falhou   bool
		consulta int // quantas consultas o waiter fez (0 = não confere)
	}{
		{"sucesso", "sucesso", fakeecs.TaskDefinitionNova, 5 * time.Second, true, false, false, 4},
		{"falha rápida do circuit breaker", "falha", fakeecs.TaskDefinitionNova, 5 * time.Second, false, false, true, 2},
		{"versão antiga não vale", "versao-antiga", fakeecs.TaskDefinitionNova, 5 * time.Second, true, false, false, 4},
		{"travado estoura o teto", "travado", fakeecs.TaskDefinitionNova, 60 * time.Millisecond, false, true, false, 0},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			cl, srv := cliente(t, c.cenario, "demo", "web")
			res, err := rollout.Wait(context.Background(), cl, rollout.Options{
				Want: c.want, Timeout: c.timeout, Interval: 10 * time.Millisecond,
			})
			if err != nil {
				t.Fatal(err)
			}
			if res.OK() != c.ok || res.TimedOut != c.timedOut || (res.Verdict.State == rollout.Failed) != c.falhou {
				t.Fatalf("res = %+v", res)
			}
			if c.consulta > 0 && (res.Polls != c.consulta || srv.Calls() != c.consulta) {
				t.Errorf("consultas: waiter=%d servidor=%d, quer %d (o estado decide, e não o relógio)", res.Polls, srv.Calls(), c.consulta)
			}
		})
	}
}
