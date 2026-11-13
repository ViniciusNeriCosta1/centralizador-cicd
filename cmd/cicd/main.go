// Comando cicd: verificações de pipeline reutilizáveis.
//
// Uso:
//
//	cicd plan-gate     -plan plano.json -policy politica.json [-approved]
//	cicd review-comment -plan plano.json [-dir .]
//	cicd reviewed-id   -comments comentarios.json [-dir .]
//	cicd tag-check     -plan plano.json -policy politica-de-tags.json
//	cicd dep-audit     -report govulncheck.json -policy politica-de-dependencias.json
//	cicd apply-gate    -plan plano.json -policy politica.json [-comments comentarios.json] [-dir .] [-approved]
//
// Códigos de saída: 0 liberado, 1 reprovado pelo gate, 2 erro de uso ou de leitura.
// Erro nunca vira "liberado": se o plano, a política ou os comentários não
// puderem ser lidos, sai 2.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"time"

	"centralizador-cicd/internal/delta"
	"centralizador-cicd/internal/gate"
	"centralizador-cicd/internal/plan"
	"centralizador-cicd/internal/policy"
	"centralizador-cicd/internal/tags"
	"centralizador-cicd/internal/vuln"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

const uso = `uso:
  cicd plan-gate      -plan <plano.json> -policy <politica.json> [-approved]
  cicd review-comment -plan <plano.json> [-dir .]
  cicd reviewed-id    -comments <comentarios.json> [-dir .]
  cicd tag-check      -plan <plano.json> -policy <politica-de-tags.json>
  cicd dep-audit      -report <govulncheck.json> -policy <politica-de-dependencias.json> [-today AAAA-MM-DD]
  cicd apply-gate     -plan <plano.json> -policy <politica.json> [-comments <comentarios.json>] [-dir .] [-approved]`

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" {
		fmt.Fprintln(stderr, uso)
		return 2
	}
	switch args[0] {
	case "plan-gate":
		return planGate(args[1:], stdout, stderr)
	case "review-comment":
		return reviewComment(args[1:], stdout, stderr)
	case "reviewed-id":
		return reviewedID(args[1:], stdout, stderr)
	case "apply-gate":
		return applyGate(args[1:], stdout, stderr)
	case "tag-check":
		return tagCheck(args[1:], stdout, stderr)
	case "dep-audit":
		return depAudit(args[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "subcomando desconhecido: %q\n", args[0])
		return 2
	}
}

func loadPlan(path string) (*plan.Plan, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return plan.Parse(f)
}

func loadComments(path string) ([]delta.Comment, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return delta.LoadComments(f)
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
	p, err := loadPlan(*planPath)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}

	res := gate.Evaluate(p, pol, *approved)
	res.Render(stdout)
	return res.ExitCode()
}

// review-comment escreve o corpo do comentário que o pull request recebe com o
// conjunto de recursos que serão destruídos.
func reviewComment(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("review-comment", flag.ContinueOnError)
	fs.SetOutput(stderr)
	planPath := fs.String("plan", "", "plano em JSON")
	dir := fs.String("dir", ".", "diretório do módulo (distingue vários módulos no mesmo PR)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *planPath == "" {
		fmt.Fprintln(stderr, "erro: -plan é obrigatório")
		return 2
	}
	p, err := loadPlan(*planPath)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}
	body, err := delta.EncodeComment(delta.Reviewed{Dir: *dir, Destroy: p.Destroyed()})
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}
	fmt.Fprint(stdout, body)
	return 0
}

// reviewed-id escreve o id do comentário do bot para o diretório, para o CI
// atualizar esse comentário em vez de criar outro. Sem comentário, escreve nada.
func reviewedID(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("reviewed-id", flag.ContinueOnError)
	fs.SetOutput(stderr)
	commentsPath := fs.String("comments", "", "comentários do PR em JSON")
	dir := fs.String("dir", ".", "diretório do módulo")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *commentsPath == "" {
		fmt.Fprintln(stderr, "erro: -comments é obrigatório")
		return 2
	}
	comments, err := loadComments(*commentsPath)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}
	_, id, err := delta.Find(comments, *dir)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}
	if id != 0 {
		fmt.Fprintln(stdout, id)
	}
	return 0
}

// apply-gate roda os dois gates do apply: a recriação de recursos sensíveis e o
// delta contra o plano revisado no pull request.
func applyGate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("apply-gate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	planPath := fs.String("plan", "", "plano do apply em JSON")
	policyPath := fs.String("policy", "", "arquivo de política")
	commentsPath := fs.String("comments", "", "comentários do PR em JSON (sem isso, não há referência)")
	dir := fs.String("dir", ".", "diretório do módulo")
	approved := fs.Bool("approved", false, "um humano aprovou a destruição de forma explícita")
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
	p, err := loadPlan(*planPath)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}

	var reviewed *delta.Reviewed
	if *commentsPath != "" {
		comments, err := loadComments(*commentsPath)
		if err != nil {
			fmt.Fprintln(stderr, "erro:", err)
			return 2
		}
		if reviewed, _, err = delta.Find(comments, *dir); err != nil {
			fmt.Fprintln(stderr, "erro:", err)
			return 2
		}
	}

	rep := gate.Evaluate(p, pol, *approved)
	rep.Render(stdout)
	d := delta.Compare(p.Destroyed(), reviewed)
	fmt.Fprintln(stdout)
	d.Render(stdout, *approved)

	if rep.ExitCode() != 0 || (d.Blocking() && !*approved) {
		fmt.Fprintln(stdout, "\nResultado do apply-gate: REPROVADO. O apply não deve rodar.")
		return 1
	}
	fmt.Fprintln(stdout, "\nResultado do apply-gate: liberado.")
	return 0
}

// tag-check confere as tags obrigatórias dos recursos que o plano cria ou atualiza.
func tagCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("tag-check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	planPath := fs.String("plan", "", "plano em JSON (saída de `terraform show -json`)")
	policyPath := fs.String("policy", "", "política de tags")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *planPath == "" || *policyPath == "" {
		fmt.Fprintln(stderr, "erro: -plan e -policy são obrigatórios")
		return 2
	}
	pol, err := tags.Load(*policyPath)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}
	p, err := loadPlan(*planPath)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}
	res := tags.Evaluate(p, pol)
	res.Render(stdout)
	return res.ExitCode()
}

// dep-audit aplica a política de dependências ao relatório do govulncheck.
func depAudit(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("dep-audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	reportPath := fs.String("report", "", "relatório de `govulncheck -format json`")
	policyPath := fs.String("policy", "", "política de dependências")
	today := fs.String("today", "", "data de hoje (AAAA-MM-DD), para as exceções com prazo; vazio = hoje (UTC)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *reportPath == "" || *policyPath == "" {
		fmt.Fprintln(stderr, "erro: -report e -policy são obrigatórios")
		return 2
	}
	now := time.Now().UTC()
	if *today != "" {
		t, err := time.Parse("2006-01-02", *today)
		if err != nil {
			fmt.Fprintf(stderr, "erro: -today %q precisa ser AAAA-MM-DD\n", *today)
			return 2
		}
		now = t
	}
	pol, err := vuln.Load(*policyPath)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}
	f, err := os.Open(*reportPath)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}
	defer f.Close()
	rep, err := vuln.Parse(f)
	if err != nil {
		fmt.Fprintln(stderr, "erro:", err)
		return 2
	}
	res := vuln.Evaluate(rep, pol, now)
	res.Render(stdout)
	return res.ExitCode()
}
