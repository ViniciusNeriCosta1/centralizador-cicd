// Package fakeecs simula o pedaço da API do ECS que a espera de rollout usa
// (DescribeServices), para provar o fluxo inteiro sem uma conta AWS.
//
// Cada cenário é um roteiro: a N-ésima consulta recebe o N-ésimo estado, e
// depois do último ele se repete. Não há relógio: o resultado depende só de
// quantas vezes o cliente perguntou, então os testes são determinísticos.
//
// O que isto prova: a lógica de decisão e a integração com o SDK da AWS (o SDK
// real serializa a chamada e interpreta a resposta). O que NÃO prova: que o ECS
// de verdade se comporta assim. A fidelidade vem da documentação da API, e é
// por isso que existe o limite escrito no README.
package fakeecs

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"sync"

	"centralizador-cicd/internal/rollout"
)

const (
	// TaskDefinitionVelha e TaskDefinitionNova são as versões dos cenários.
	TaskDefinitionVelha = "arn:aws:ecs:us-east-1:123456789012:task-definition/web:1"
	TaskDefinitionNova  = "arn:aws:ecs:us-east-1:123456789012:task-definition/web:2"

	target = "AmazonEC2ContainerServiceV20141113.DescribeServices"
)

// Scenario é um roteiro de estados.
type Scenario struct {
	Descricao string
	Passos    []rollout.Service
	// Inexistente: o serviço não existe (o ECS responde com `failures`, não com erro HTTP).
	Inexistente bool
}

func dep(status, state, reason, td string, desired, running, pending int) rollout.Deployment {
	return rollout.Deployment{ID: "ecs-svc/" + status + td[len(td)-1:], Status: status, RolloutState: state,
		RolloutStateReason: reason, TaskDefinition: td, DesiredCount: desired, RunningCount: running, PendingCount: pending}
}

func svc(running, pending int, deps ...rollout.Deployment) rollout.Service {
	return rollout.Service{Name: "web", Status: "ACTIVE", DesiredCount: 2, RunningCount: running, PendingCount: pending, Deployments: deps}
}

// Scenarios devolve os cenários prontos, por nome.
func Scenarios() map[string]Scenario {
	velhaOK := dep("PRIMARY", "COMPLETED", "ECS deployment ecs-svc/1 completed.", TaskDefinitionVelha, 2, 2, 0)
	return map[string]Scenario{
		"sucesso": {
			Descricao: "o deploy novo sobe aos poucos, o antigo drena e o serviço estabiliza",
			Passos: []rollout.Service{
				svc(2, 2, dep("PRIMARY", "IN_PROGRESS", "ECS deployment ecs-svc/2 in progress.", TaskDefinitionNova, 2, 0, 2), dep("ACTIVE", "COMPLETED", "", TaskDefinitionVelha, 2, 2, 0)),
				svc(3, 1, dep("PRIMARY", "IN_PROGRESS", "ECS deployment ecs-svc/2 in progress.", TaskDefinitionNova, 2, 1, 1), dep("ACTIVE", "COMPLETED", "", TaskDefinitionVelha, 2, 2, 0)),
				svc(3, 0, dep("PRIMARY", "COMPLETED", "ECS deployment ecs-svc/2 completed.", TaskDefinitionNova, 2, 2, 0), dep("ACTIVE", "COMPLETED", "", TaskDefinitionVelha, 2, 1, 0)),
				svc(2, 0, dep("PRIMARY", "COMPLETED", "ECS deployment ecs-svc/2 completed.", TaskDefinitionNova, 2, 2, 0)),
			},
		},
		"falha": {
			Descricao: "as tasks novas não sobem e o circuit breaker marca o deploy como FAILED",
			Passos: []rollout.Service{
				svc(2, 2, dep("PRIMARY", "IN_PROGRESS", "ECS deployment ecs-svc/2 in progress.", TaskDefinitionNova, 2, 0, 2), dep("ACTIVE", "COMPLETED", "", TaskDefinitionVelha, 2, 2, 0)),
				svc(2, 0, dep("PRIMARY", "FAILED", "ECS deployment circuit breaker: tasks failed to start.", TaskDefinitionNova, 2, 0, 0), dep("ACTIVE", "COMPLETED", "", TaskDefinitionVelha, 2, 2, 0)),
			},
		},
		"travado": {
			Descricao: "o deploy nunca converge e nunca falha (sem circuit breaker): só o teto de tempo encerra",
			Passos: []rollout.Service{
				svc(3, 1, dep("PRIMARY", "IN_PROGRESS", "ECS deployment ecs-svc/2 in progress.", TaskDefinitionNova, 2, 1, 1), dep("ACTIVE", "COMPLETED", "", TaskDefinitionVelha, 2, 2, 0)),
			},
		},
		"versao-antiga": {
			Descricao: "logo após o update-service, o serviço ainda mostra a versão ANTIGA estável: não pode valer como pronto",
			Passos: []rollout.Service{
				svc(2, 0, velhaOK),
				svc(2, 0, velhaOK),
				svc(2, 2, dep("PRIMARY", "IN_PROGRESS", "", TaskDefinitionNova, 2, 0, 2), dep("ACTIVE", "COMPLETED", "", TaskDefinitionVelha, 2, 2, 0)),
				svc(2, 0, dep("PRIMARY", "COMPLETED", "ECS deployment ecs-svc/2 completed.", TaskDefinitionNova, 2, 2, 0)),
			},
		},
		"inexistente": {
			Descricao:   "o serviço não existe",
			Inexistente: true,
		},
	}
}

// Nomes lista os cenários em ordem.
func Nomes() []string {
	var out []string
	for n := range Scenarios() {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Server é o ECS simulado.
type Server struct {
	Cluster, Service string
	scenario         Scenario

	mu    sync.Mutex
	calls int
}

// New cria o servidor para um cenário, no cluster "demo" e serviço "web".
func New(nome string) (*Server, error) {
	sc, ok := Scenarios()[nome]
	if !ok {
		return nil, fmt.Errorf("cenário %q desconhecido (use um de %v)", nome, Nomes())
	}
	return &Server{Cluster: "demo", Service: "web", scenario: sc}, nil
}

// Calls diz quantas consultas válidas o servidor atendeu.
func (s *Server) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// Handler atende o protocolo awsJson1.1 do ECS.
func (s *Server) Handler() http.Handler {
	return http.HandlerFunc(s.serve)
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/x-amz-json-1.1")
	if r.Method != http.MethodPost || r.Header.Get("X-Amz-Target") != target {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"__type":"UnknownOperationException","message":"só DescribeServices é simulado"}`)
		return
	}
	var in struct {
		Cluster  string   `json:"cluster"`
		Services []string `json:"services"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"__type":"InvalidParameterException","message":"corpo inválido"}`)
		return
	}
	if s.scenario.Inexistente || in.Cluster != s.Cluster || len(in.Services) != 1 || in.Services[0] != s.Service {
		// O ECS não responde 404: devolve a lista vazia e o motivo em `failures`.
		writeJSON(w, map[string]any{
			"services": []any{},
			"failures": []any{map[string]any{"arn": in.Cluster + "/" + first(in.Services), "reason": "MISSING"}},
		})
		return
	}
	s.mu.Lock()
	i := s.calls
	if i >= len(s.scenario.Passos) {
		i = len(s.scenario.Passos) - 1
	}
	s.calls++
	passo := s.scenario.Passos[i]
	s.mu.Unlock()
	writeJSON(w, map[string]any{"services": []any{wire(passo)}, "failures": []any{}})
}

func first(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return ss[0]
}

func writeJSON(w http.ResponseWriter, v any) {
	if err := json.NewEncoder(w).Encode(v); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

// wire converte para os nomes de campo da API do ECS.
func wire(s rollout.Service) map[string]any {
	deps := make([]any, 0, len(s.Deployments))
	for _, d := range s.Deployments {
		m := map[string]any{
			"id": d.ID, "status": d.Status, "taskDefinition": d.TaskDefinition,
			"desiredCount": d.DesiredCount, "runningCount": d.RunningCount, "pendingCount": d.PendingCount,
		}
		if d.RolloutState != "" {
			m["rolloutState"] = d.RolloutState
			m["rolloutStateReason"] = d.RolloutStateReason
		}
		deps = append(deps, m)
	}
	return map[string]any{
		"serviceName": s.Name, "status": s.Status,
		"desiredCount": s.DesiredCount, "runningCount": s.RunningCount, "pendingCount": s.PendingCount,
		"deployments": deps,
	}
}
