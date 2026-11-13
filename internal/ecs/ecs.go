// Package ecs consulta o estado de um serviço no Amazon ECS (DescribeServices)
// e o entrega no formato que o pacote rollout entende.
package ecs

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	awsecs "github.com/aws/aws-sdk-go-v2/service/ecs"
	"github.com/aws/aws-sdk-go-v2/service/ecs/types"

	"centralizador-cicd/internal/rollout"
)

type describeAPI interface {
	DescribeServices(ctx context.Context, in *awsecs.DescribeServicesInput, opts ...func(*awsecs.Options)) (*awsecs.DescribeServicesOutput, error)
}

// Client consulta um serviço. Implementa rollout.Describer.
type Client struct {
	api              describeAPI
	Cluster, Service string
}

// New usa a cadeia padrão de credenciais da AWS (variáveis de ambiente, perfil,
// OIDC já assumido pelo workflow...). `endpoint` vazio = o ECS de verdade; um
// valor aponta para um simulador.
func New(ctx context.Context, region, endpoint, cluster, service string) (*Client, error) {
	var opts []func(*config.LoadOptions) error
	if region != "" {
		opts = append(opts, config.WithRegion(region))
	}
	cfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("configuração da AWS: %w", err)
	}
	if cfg.Region == "" {
		return nil, fmt.Errorf("região da AWS não definida (use -region ou AWS_REGION)")
	}
	api := awsecs.NewFromConfig(cfg, func(o *awsecs.Options) {
		if endpoint != "" {
			o.BaseEndpoint = aws.String(endpoint)
		}
	})
	return &Client{api: api, Cluster: cluster, Service: service}, nil
}

// Describe devolve o estado atual do serviço.
func (c *Client) Describe(ctx context.Context) (rollout.Service, error) {
	out, err := c.api.DescribeServices(ctx, &awsecs.DescribeServicesInput{
		Cluster:  aws.String(c.Cluster),
		Services: []string{c.Service},
	})
	if err != nil {
		return rollout.Service{}, fmt.Errorf("DescribeServices: %w", err)
	}
	if len(out.Services) == 0 {
		var motivos []string
		for _, f := range out.Failures {
			motivos = append(motivos, aws.ToString(f.Reason))
		}
		return rollout.Service{}, fmt.Errorf("%w: %s/%s (%s)", rollout.ErrNotFound, c.Cluster, c.Service, strings.Join(motivos, ", "))
	}
	return convert(out.Services[0]), nil
}

func convert(s types.Service) rollout.Service {
	svc := rollout.Service{
		Name:         aws.ToString(s.ServiceName),
		Status:       aws.ToString(s.Status),
		DesiredCount: int(s.DesiredCount),
		RunningCount: int(s.RunningCount),
		PendingCount: int(s.PendingCount),
	}
	for _, d := range s.Deployments {
		svc.Deployments = append(svc.Deployments, rollout.Deployment{
			ID:                 aws.ToString(d.Id),
			Status:             aws.ToString(d.Status),
			RolloutState:       string(d.RolloutState),
			RolloutStateReason: aws.ToString(d.RolloutStateReason),
			TaskDefinition:     aws.ToString(d.TaskDefinition),
			DesiredCount:       int(d.DesiredCount),
			RunningCount:       int(d.RunningCount),
			PendingCount:       int(d.PendingCount),
		})
	}
	return svc
}
