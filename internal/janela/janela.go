// Package janela abre a tela do emulador numa janela nativa do sistema:
// GTK + WebKit no Linux e WebView2 no Windows. Nenhum navegador é usado.
//
// Abrir precisa rodar na thread principal (exigência do GTK e do Windows) e
// só retorna quando a janela é fechada.
package janela

import (
	"errors"
	"os/exec"
	"runtime"
)

const (
	titulo  = "Bobina · emulador de impressoras"
	largura = 1360
	altura  = 880
)

// ErrSemJanelaNativa indica que o sistema não tem o componente de janela
// (WebView2 ausente no Windows, por exemplo). Quem chama pode cair para o
// navegador.
var ErrSemJanelaNativa = errors.New("janela nativa indisponível neste sistema")

// AbrirNoNavegador é o plano B: abre a tela no navegador padrão.
func AbrirNoNavegador(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}
