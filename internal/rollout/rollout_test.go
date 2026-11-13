package rollout

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const (
	tdVelha = "arn:aws:ecs:us-east-1:123456789012:task-definition/web:1"
	tdNova  = "arn:aws:ecs:us-east-1:123456789012:task-definition/web:2"
)

func dep(status, state, td string, desired, running, pending int) Deployment {
	return Deployment{ID: "ecs-svc/" + status, Status: status, RolloutState: state, TaskDefinition: td,
		DesiredCount: desired, RunningCount: running, PendingCount: pending}
}

func svc(running, pending int, deps ...Deployment) Service {
	return Service{Name: "web", Status: "ACTIVE", DesiredCount: 2, RunningCount: running, PendingCount: pending, Deployments: deps}
}

func TestEvaluate(t *testing.T) {
	falho := dep("PRIMARY", "FAILED", tdNova, 2, 0, 0)
	falho.RolloutStateReason = "tasks falharam ao iniciar"
	casos := []struct {
		nome   string
		s      Service
		want   string
		estado State
		trecho string
	}{
		{"estável", svc(2, 0, dep("PRIMARY", "COMPLETED", tdNova, 2, 2, 0)), tdNova, Stable, "2/2"},
		{"estável sem conferir a task definition", svc(2, 0, dep("PRIMARY", "COMPLETED", tdVelha, 2, 2, 0)), "", Stable, ""},
		{"sem rolloutState, só as contagens", svc(2, 0, dep("PRIMARY", "", tdNova, 2, 2, 0)), tdNova, Stable, ""},
		{"versão antiga estável: o deploy novo ainda não apareceu", svc(2, 0, dep("PRIMARY", "COMPLETED", tdVelha, 2, 2, 0)), tdNova, Progress, "esperado " + tdNova},
		{"em andamento", svc(1, 1, dep("PRIMARY", "IN_PROGRESS", tdNova, 2, 1, 1), dep("ACTIVE", "COMPLETED", tdVelha, 2, 2, 0)), tdNova, Progress, "IN_PROGRESS"},
		{"completou mas o antigo ainda drena", svc(2, 0, dep("PRIMARY", "COMPLETED", tdNova, 2, 2, 0), dep("ACTIVE", "COMPLETED", tdVelha, 2, 1, 0)), tdNova, Progress, "drenarem"},
		{"completou mas faltam tasks", svc(1, 0, dep("PRIMARY", "COMPLETED", tdNova, 2, 1, 0)), tdNova, Progress, "rodando 1/2"},
		{"completou mas há task pendente", svc(2, 1, dep("PRIMARY", "COMPLETED", tdNova, 2, 2, 1)), tdNova, Progress, "1 pendente"},
		{"circuit breaker disparou", svc(0, 0, falho), tdNova, Failed, "tasks falharam ao iniciar"},
		{"falha vale mesmo sem task definition esperada", svc(0, 0, falho), "", Failed, "FAILED"},
		{"falha sem motivo", svc(0, 0, dep("PRIMARY", "FAILED", tdNova, 2, 0, 0)), "", Failed, "sem motivo informado"},
		{"serviço drenando", Service{Name: "web", Status: "DRAINING"}, "", Failed, "DRAINING"},
		{"sem deployment PRIMARY", svc(2, 0, dep("ACTIVE", "COMPLETED", tdVelha, 2, 2, 0)), "", Progress, "PRIMARY"},
		{"sem deployments", svc(0, 0), "", Progress, "PRIMARY"},
	}
	for _, c := range casos {
		t.Run(c.nome, func(t *testing.T) {
			v := Evaluate(c.s, c.want)
			if v.State != c.estado {
				t.Fatalf("State = %s (%s), quer %s", v.State, v.Reason, c.estado)
			}
			if !strings.Contains(v.Reason, c.trecho) {
				t.Errorf("Reason = %q, deveria conter %q", v.Reason, c.trecho)
			}
		})
	}
}

// relógio e pausa falsos: nada espera de verdade.
type relogio struct{ agora time.Time }

func (r *relogio) Now() time.Time { return r.agora }
func (r *relogio) Sleep(_ context.Context, d time.Duration) error {
	r.agora = r.agora.Add(d)
	return nil
}

// roteiro devolve uma resposta por consulta; depois da última, repete a última.
type roteiro struct {
	passos []any // Service ou error
	n      int
}

func (r *roteiro) Describe(context.Context) (Service, error) {
	i := r.n
	if i >= len(r.passos) {
		i = len(r.passos) - 1
	}
	r.n++
	switch p := r.passos[i].(type) {
	case error:
		return Service{}, p
	default:
		return p.(Service), nil
	}
}

func opcoes(r *relogio, want string) Options {
	return Options{Want: want, Timeout: 10 * time.Minute, Interval: 10 * time.Second, Now: r.Now, Sleep: r.Sleep}
}

func TestWaitTerminaAssimQueEstavel(t *testing.T) {
	r := &relogio{agora: time.Unix(0, 0)}
	d := &roteiro{passos: []any{
		svc(1, 1, dep("PRIMARY", "IN_PROGRESS", tdNova, 2, 1, 1), dep("ACTIVE", "COMPLETED", tdVelha, 2, 2, 0)),
		svc(2, 0, dep("PRIMARY", "COMPLETED", tdNova, 2, 2, 0), dep("ACTIVE", "COMPLETED", tdVelha, 2, 1, 0)),
		svc(2, 0, dep("PRIMARY", "COMPLETED", tdNova, 2, 2, 0)),
	}}
	var log bytes.Buffer
	o := opcoes(r, tdNova)
	o.Log = &log
	res, err := Wait(context.Background(), d, o)
	if err != nil || !res.OK() {
		t.Fatalf("Wait = %+v, %v", res, err)
	}
	if res.Polls != 3 || res.Elapsed != 20*time.Second {
		t.Errorf("Polls=%d Elapsed=%s: devia parar na 3ª consulta, aos 20s, sem usar o teto de 10min", res.Polls, res.Elapsed)
	}
	if !strings.Contains(log.String(), "em andamento") || !strings.Contains(log.String(), "estável") {
		t.Errorf("log:\n%s", log.String())
	}
}

// O caso que um tempo fixo erra: o deploy falha logo, e não faz sentido gastar
// os 10 minutos.
func TestWaitFalhaNaHoraSemGastarOTeto(t *testing.T) {
	r := &relogio{agora: time.Unix(0, 0)}
	falho := dep("PRIMARY", "FAILED", tdNova, 2, 0, 0)
	falho.RolloutStateReason = "circuit breaker"
	d := &roteiro{passos: []any{
		svc(0, 2, dep("PRIMARY", "IN_PROGRESS", tdNova, 2, 0, 2)),
		svc(0, 0, falho),
	}}
	res, err := Wait(context.Background(), d, opcoes(r, tdNova))
	if err != nil {
		t.Fatal(err)
	}
	if res.Verdict.State != Failed || res.TimedOut || res.Polls != 2 || res.Elapsed != 10*time.Second {
		t.Fatalf("res = %+v", res)
	}
	if res.OK() {
		t.Error("deploy falho não pode ser OK")
	}
}

func TestWaitEsperaAVersaoNovaAparecer(t *testing.T) {
	r := &relogio{agora: time.Unix(0, 0)}
	velha := svc(2, 0, dep("PRIMARY", "COMPLETED", tdVelha, 2, 2, 0))
	d := &roteiro{passos: []any{
		velha, velha, // o update-service ainda não refletiu
		svc(2, 0, dep("PRIMARY", "COMPLETED", tdNova, 2, 2, 0)),
	}}
	res, err := Wait(context.Background(), d, opcoes(r, tdNova))
	if err != nil || !res.OK() || res.Polls != 3 {
		t.Fatalf("res = %+v, err = %v: a versão antiga estável não pode encerrar a espera", res, err)
	}
	// Sem conferir a task definition, a versão antiga encerraria na 1ª consulta.
	r2 := &relogio{agora: time.Unix(0, 0)}
	res, _ = Wait(context.Background(), &roteiro{passos: []any{velha}}, opcoes(r2, ""))
	if res.Polls != 1 {
		t.Errorf("sem conferir: Polls = %d", res.Polls)
	}
}

func TestWaitTravadoEstouraOTeto(t *testing.T) {
	r := &relogio{agora: time.Unix(0, 0)}
	d := &roteiro{passos: []any{svc(1, 1, dep("PRIMARY", "IN_PROGRESS", tdNova, 2, 1, 1))}}
	o := opcoes(r, tdNova)
	o.Timeout = 60 * time.Second
	res, err := Wait(context.Background(), d, o)
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut || res.OK() || res.Verdict.State != Progress {
		t.Fatalf("res = %+v", res)
	}
	if res.Elapsed > o.Timeout {
		t.Errorf("Elapsed %s passou do teto %s", res.Elapsed, o.Timeout)
	}
}

func TestWaitServicoInexistenteFalhaNaHora(t *testing.T) {
	r := &relogio{agora: time.Unix(0, 0)}
	d := &roteiro{passos: []any{ErrNotFound}}
	res, err := Wait(context.Background(), d, opcoes(r, ""))
	if !errors.Is(err, ErrNotFound) || res.Polls != 1 {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
}

func TestWaitToleraErroTransitorioMasNaoParaSempre(t *testing.T) {
	boom := errors.New("ThrottlingException")
	r := &relogio{agora: time.Unix(0, 0)}
	estavel := svc(2, 0, dep("PRIMARY", "COMPLETED", tdNova, 2, 2, 0))
	res, err := Wait(context.Background(), &roteiro{passos: []any{boom, boom, estavel}}, opcoes(r, tdNova))
	if err != nil || !res.OK() {
		t.Fatalf("dois erros seguidos seguidos de sucesso deveriam passar: %+v, %v", res, err)
	}

	r = &relogio{agora: time.Unix(0, 0)}
	_, err = Wait(context.Background(), &roteiro{passos: []any{boom}}, opcoes(r, tdNova))
	if err == nil || !errors.Is(err, boom) {
		t.Fatalf("três erros seguidos deveriam desistir com o erro: %v", err)
	}

	// Erro que não é seguido não acumula.
	r = &relogio{agora: time.Unix(0, 0)}
	emAndamento := svc(1, 1, dep("PRIMARY", "IN_PROGRESS", tdNova, 2, 1, 1))
	res, err = Wait(context.Background(), &roteiro{passos: []any{boom, emAndamento, boom, emAndamento, boom, estavel}}, opcoes(r, tdNova))
	if err != nil || !res.OK() {
		t.Fatalf("erros intercalados não deveriam acumular: %+v, %v", res, err)
	}
}

func TestWaitSoComErrosAteOTeto(t *testing.T) {
	boom := errors.New("timeout de rede")
	r := &relogio{agora: time.Unix(0, 0)}
	o := opcoes(r, "")
	o.MaxErrors = 100
	o.Timeout = 30 * time.Second
	res, err := Wait(context.Background(), &roteiro{passos: []any{boom}}, o)
	if err != nil || !res.TimedOut || res.OK() {
		t.Fatalf("res = %+v, err = %v", res, err)
	}
	if res.Verdict.Reason == "" {
		t.Error("o motivo precisa dizer que nenhuma consulta funcionou")
	}
}

func TestWaitContextoCancelado(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &roteiro{passos: []any{svc(1, 1, dep("PRIMARY", "IN_PROGRESS", tdNova, 2, 1, 1))}}
	o := Options{Timeout: time.Minute, Interval: time.Second, Sleep: func(ctx context.Context, _ time.Duration) error { return ctx.Err() }}
	if _, err := Wait(ctx, d, o); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}

func TestWaitExigeTempoPositivo(t *testing.T) {
	if _, err := Wait(context.Background(), &roteiro{passos: []any{Service{}}}, Options{}); err == nil {
		t.Fatal("esperava erro")
	}
}
