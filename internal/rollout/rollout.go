// Package rollout decide se um deploy de serviço ECS terminou, olhando o ESTADO
// do serviço, e não um tempo fixo.
//
// Esperar um tempo fixo erra nos dois sentidos: fica vermelho quando o deploy
// deu certo mas demorou mais que o limite, e gasta o limite inteiro quando o
// deploy falhou logo no começo. Aqui o tempo é só o teto: a espera termina
// assim que o estado é estável ou falho.
package rollout

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

// Deployment é um deployment do serviço (o recorte que a decisão usa).
type Deployment struct {
	ID                 string
	Status             string // PRIMARY, ACTIVE ou INACTIVE
	RolloutState       string // IN_PROGRESS, COMPLETED ou FAILED (vazio sem circuit breaker/ECS controller)
	RolloutStateReason string
	TaskDefinition     string
	DesiredCount       int
	RunningCount       int
	PendingCount       int
}

// Service é o estado do serviço num instante.
type Service struct {
	Name         string
	Status       string // ACTIVE, DRAINING ou INACTIVE
	DesiredCount int
	RunningCount int
	PendingCount int
	Deployments  []Deployment
}

// State é o veredito de uma consulta.
type State int

const (
	// Progress: ainda não terminou.
	Progress State = iota
	// Stable: o deploy terminou e o serviço está no estado desejado.
	Stable
	// Failed: o deploy falhou; esperar mais não vai ajudar.
	Failed
)

func (s State) String() string {
	switch s {
	case Stable:
		return "estável"
	case Failed:
		return "falhou"
	}
	return "em andamento"
}

// Verdict é o estado e o motivo, em texto, para o relatório.
type Verdict struct {
	State  State
	Reason string
}

// Evaluate decide o estado do serviço. `want` é a task definition que o deploy
// deveria colocar no ar; vazio = não conferir.
//
// Por que conferir `want`: logo depois de um update-service, o serviço ainda
// pode mostrar o deployment ANTIGO como estável. Sem conferir qual task
// definition é a PRIMARY, a espera acabaria na hora, declarando estável a
// versão velha.
func Evaluate(s Service, want string) Verdict {
	if s.Status != "" && s.Status != "ACTIVE" {
		return Verdict{Failed, fmt.Sprintf("o serviço está %s, não ACTIVE", s.Status)}
	}
	var primary *Deployment
	for i := range s.Deployments {
		if s.Deployments[i].Status == "PRIMARY" {
			primary = &s.Deployments[i]
			break
		}
	}
	if primary == nil {
		return Verdict{Progress, "sem deployment PRIMARY ainda"}
	}
	if want != "" && primary.TaskDefinition != want {
		return Verdict{Progress, fmt.Sprintf("o deployment PRIMARY ainda usa %s; esperado %s (o deploy novo ainda não apareceu)",
			primary.TaskDefinition, want)}
	}
	if primary.RolloutState == "FAILED" {
		reason := primary.RolloutStateReason
		if reason == "" {
			reason = "sem motivo informado"
		}
		return Verdict{Failed, "rollout FAILED: " + reason}
	}
	// Com o circuit breaker, COMPLETED é o sinal do ECS. Sem rolloutState
	// (deploy fora do controller ECS), só as contagens decidem.
	if primary.RolloutState != "" && primary.RolloutState != "COMPLETED" {
		return Verdict{Progress, fmt.Sprintf("rolloutState=%s, rodando %d/%d",
			primary.RolloutState, primary.RunningCount, primary.DesiredCount)}
	}
	if len(s.Deployments) > 1 {
		return Verdict{Progress, fmt.Sprintf("%d deployments ativos: aguardando os antigos drenarem", len(s.Deployments))}
	}
	if s.RunningCount != s.DesiredCount || s.PendingCount != 0 {
		return Verdict{Progress, fmt.Sprintf("rodando %d/%d, %d pendente(s)", s.RunningCount, s.DesiredCount, s.PendingCount)}
	}
	return Verdict{Stable, fmt.Sprintf("%d/%d tasks rodando", s.RunningCount, s.DesiredCount)}
}

// ErrNotFound: o serviço não existe. Não adianta tentar de novo.
var ErrNotFound = errors.New("serviço não encontrado")

// Describer consulta o estado do serviço.
type Describer interface {
	Describe(ctx context.Context) (Service, error)
}

// Options controla a espera.
type Options struct {
	Want     string        // task definition esperada (opcional)
	Timeout  time.Duration // teto da espera, não a duração normal
	Interval time.Duration // pausa entre as consultas
	// MaxErrors é quantas consultas seguidas podem falhar (limite de taxa,
	// rede) antes de desistir. Padrão 3.
	MaxErrors int
	Log       io.Writer
	// Now e Sleep existem para os testes não esperarem de verdade.
	Now   func() time.Time
	Sleep func(ctx context.Context, d time.Duration) error
}

// Result é o desfecho da espera.
type Result struct {
	Verdict  Verdict
	TimedOut bool
	Polls    int
	Elapsed  time.Duration
}

// OK diz se o deploy terminou bem.
func (r Result) OK() bool { return r.Verdict.State == Stable && !r.TimedOut }

// Wait consulta o serviço até o estado ficar estável ou falho, ou até o teto de
// tempo. Erro de consulta é erro (não vira "estável" nem "falhou"): o chamador
// decide. O serviço inexistente falha na hora; erros transitórios são
// repetidos até MaxErrors vezes seguidas.
func Wait(ctx context.Context, d Describer, o Options) (Result, error) {
	if o.Timeout <= 0 || o.Interval <= 0 {
		return Result{}, errors.New("timeout e intervalo precisam ser positivos")
	}
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Sleep == nil {
		o.Sleep = sleep
	}
	if o.MaxErrors <= 0 {
		o.MaxErrors = 3
	}
	start := o.Now()
	var res Result
	failures := 0
	for {
		res.Polls++
		svc, err := d.Describe(ctx)
		res.Elapsed = o.Now().Sub(start)
		if err != nil {
			if errors.Is(err, ErrNotFound) || ctx.Err() != nil {
				return res, err
			}
			failures++
			logf(o.Log, "[%s] consulta falhou (%d/%d): %v", dur(res.Elapsed), failures, o.MaxErrors, err)
			if failures >= o.MaxErrors {
				return res, fmt.Errorf("%d consultas seguidas falharam: %w", failures, err)
			}
		} else {
			failures = 0
			res.Verdict = Evaluate(svc, o.Want)
			logf(o.Log, "[%s] %s: %s", dur(res.Elapsed), res.Verdict.State, res.Verdict.Reason)
			if res.Verdict.State != Progress {
				return res, nil
			}
		}
		if res.Elapsed+o.Interval > o.Timeout {
			res.TimedOut = true
			if err != nil && res.Verdict.Reason == "" {
				res.Verdict = Verdict{Progress, "sem nenhuma consulta bem-sucedida"}
			}
			return res, nil
		}
		if err := o.Sleep(ctx, o.Interval); err != nil {
			return res, err
		}
	}
}

func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func logf(w io.Writer, format string, a ...any) {
	if w != nil {
		fmt.Fprintf(w, format+"\n", a...)
	}
}

func dur(d time.Duration) string { return d.Round(time.Millisecond).String() }
