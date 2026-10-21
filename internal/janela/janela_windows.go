//go:build windows

package janela

import (
	"os"
	"path/filepath"

	webview2 "github.com/jchv/go-webview2"
)

// Abrir mostra a tela numa janela com o WebView2 (já vem no Windows 10 e 11)
// e espera ela fechar. O ícone vem do recurso embutido no .exe.
func Abrir(url string, _ []byte) error {
	dados, _ := os.UserConfigDir()
	w := webview2.NewWithOptions(webview2.WebViewOptions{
		AutoFocus: true,
		DataPath:  filepath.Join(dados, "bobina", "webview2"),
		WindowOptions: webview2.WindowOptions{
			Title:  titulo,
			Width:  largura,
			Height: altura,
			IconId: 1,
			Center: true,
		},
	})
	if w == nil {
		return ErrSemJanelaNativa
	}
	defer w.Destroy()
	w.Navigate(url)
	w.Run()
	return nil
}
