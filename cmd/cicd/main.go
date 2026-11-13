// Comando cicd: verificações de pipeline reutilizáveis.
//
// Uso:
//
//	cicd plan-gate -plan plano.json -policy config/plan-policy.json [-approved]
//
// Códigos de saída: 0 liberado, 1 reprovado pelo gate, 2 erro de uso ou de leitura.
// Erro nunca vira "liberado": se o plano ou a política não puderem ser lidos, sai 2.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"centralizador-cicd/internal/gate"
	"centralizador-cicd/internal/plan"
	"centralizador-cicd/internal/policy"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(stderr, "uso: cicd plan-gate -plan <plano.json> -policy <politica.json> [-approved]")
		return 2
	}
	switch args[0] {
	case "plan-gate":
		return planGate(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "subcomando desconhecido: %q\n", args[0])
		return 2
	}
}

func planGate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("plan-gate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	planPath := fs.String("plan", "", "plano em JSON (saída de `terraform show -json`)")
	policyPath := fs.String("policy", "", "arquivo de política")
	approved := fs.Bool("approved", false, "a recriação de recursos sensíveis foi aprovada de forma explícita")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *planPath == "" || *policyPath == "" {
		fmt.Fprintln(stderr, "erro: -plan e -policy são obrigatórios")
		return 2
	}

	pol, err := policy.Load(*policyPath)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}
	f, err := os.Open(*planPath)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}
	defer f.Close()
	p, err := plan.Parse(f)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}

	res := gate.Evaluate(p, pol, *approved)
	res.Render(stdout)
	return res.ExitCode()
}
