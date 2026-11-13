// Comando fake-ecs: um ECS simulado (só DescribeServices) com cenários de rollout.
//
// Uso:
//
//	fake-ecs -scenario sucesso -addr 127.0.0.1:8765
//
// Serve para provar a espera de rollout sem conta AWS: aponte o
// `cicd rollout-wait -endpoint` para ele. Cluster "demo", serviço "web".
package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"

	"centralizador-cicd/internal/fakeecs"
)

func main() {
	scenario := flag.String("scenario", "sucesso", "cenário: "+fmt.Sprint(fakeecs.Nomes()))
	addr := flag.String("addr", "127.0.0.1:8765", "endereço para escutar")
	flag.Parse()

	srv, err := fakeecs.New(*scenario)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(2)
	}
	ln, err := net.Listen("tcp", *addr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(2)
	}
	fmt.Printf("fake-ecs: cenário %q em http://%s (cluster demo, serviço web)\n", *scenario, ln.Addr())
	if err := http.Serve(ln, srv.Handler()); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
}
