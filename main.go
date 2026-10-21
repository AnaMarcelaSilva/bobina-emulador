// Bobina: emulador de impressoras de cupom (ESC/POS) e etiqueta (ZPL) para
// testar sistemas sem impressora de verdade.
//
//	bobina                       abre o emulador numa janela de desktop
//	bobina -sem-janela           só o servidor (CI, Docker, servidor de testes)
//	bobina enviar HOST:PORTA arq manda um arquivo para uma impressora de rede
//	bobina status HOST:PORTA     pergunta o status ESC/POS (ou ZPL com -zpl)
//	bobina instalar              Linux: instala para o usuário e põe no menu de aplicativos
//	bobina desinstalar           remove o que o "instalar" colocou
package main

import (
	"context"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"bobina/internal/impressora"
	"bobina/internal/janela"
	"bobina/internal/web"
)

//go:embed internal/web/ui/icone.svg
var icone []byte

// A janela nativa (GTK, WebView2) precisa rodar na thread principal.
func init() { runtime.LockOSThread() }

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "enviar":
			sair(comandoEnviar(os.Args[2:]))
		case "status":
			sair(comandoStatus(os.Args[2:]))
		case "instalar":
			sair(comandoInstalar())
		case "desinstalar":
			sair(comandoDesinstalar())
		}
	}

	dirConfig, _ := os.UserConfigDir()
	porta := flag.Int("porta", 8631, "porta da tela e da API")
	host := flag.String("host", "127.0.0.1", "endereço da tela e da API (0.0.0.0 para acessar de outra máquina)")
	arquivo := flag.String("config", filepath.Join(dirConfig, "bobina", "impressoras.json"), "arquivo com as impressoras configuradas")
	semJanela := flag.Bool("sem-janela", false, "não abre a janela; fica só o servidor")
	flag.Parse()

	endereco := net.JoinHostPort(*host, fmt.Sprint(*porta))
	url := "http://" + net.JoinHostPort(enderecoLocal(*host), fmt.Sprint(*porta))

	ln, err := net.Listen("tcp", endereco)
	if err != nil {
		// Já tem um Bobina rodando nessa porta? Então só abre outra janela.
		if jaRodando(url) {
			if !*semJanela {
				abrirJanela(url)
			}
			fmt.Println("O Bobina já está rodando em", url)
			return
		}
		log.Fatalf("a porta %d da tela está ocupada: %v (use -porta)", *porta, err)
	}

	g, err := impressora.Novo(*arquivo)
	if err != nil {
		log.Fatal(err)
	}
	srv := &http.Server{
		Handler:           web.Protegido(web.Rotas(g), web.HostLocal(*host)),
		ReadHeaderTimeout: 10 * time.Second,
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatal(err)
		}
	}()

	fmt.Printf("Bobina rodando em %s\nConfiguração: %s\n", url, *arquivo)
	for _, r := range g.Lista() {
		situacao := r.Endereco
		if r.Erro != "" {
			situacao = "ERRO: " + r.Erro
		}
		fmt.Printf("  • %-20s %-7s %s\n", r.Nome, r.Linguagem, situacao)
	}

	encerrar := make(chan os.Signal, 1)
	signal.Notify(encerrar, os.Interrupt, syscall.SIGTERM)

	if *semJanela {
		<-encerrar
	} else {
		go func() {
			<-encerrar
			fmt.Println("Encerrando…")
			g.Encerrar()
			os.Exit(0)
		}()
		// Fica aqui até a janela fechar; sem janela nativa, até o Ctrl+C.
		if !abrirJanela(url) {
			<-make(chan struct{})
		}
	}

	fmt.Println("Encerrando…")
	ctx, cancelar := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelar()
	_ = srv.Shutdown(ctx)
	g.Encerrar()
}

func sair(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func enderecoLocal(host string) string {
	if host == "0.0.0.0" || host == "" {
		return "127.0.0.1"
	}
	return host
}

func jaRodando(url string) bool {
	c := http.Client{Timeout: time.Second}
	r, err := c.Get(url + "/api/impressoras")
	if err != nil {
		return false
	}
	r.Body.Close()
	return r.StatusCode == http.StatusOK
}

// abrirJanela abre a janela nativa e espera ela fechar. Devolve false quando
// o sistema não tem o componente de janela e a tela foi para o navegador.
func abrirJanela(url string) bool {
	if err := janela.Abrir(url, icone); err != nil {
		fmt.Println("Sem janela nativa (" + err.Error() + "); abrindo no navegador: " + url)
		janela.AbrirNoNavegador(url)
		return false
	}
	return true
}
