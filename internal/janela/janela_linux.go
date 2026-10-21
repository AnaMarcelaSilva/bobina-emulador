//go:build linux

package janela

/*
#cgo pkg-config: gtk+-3.0 webkit2gtk-4.1
#include <stdlib.h>
#include <signal.h>
#include <gtk/gtk.h>
#include <webkit2/webkit2.h>

// O WebKit instala tratadores de sinal sem SA_ONSTACK, e o runtime do Go
// derruba o programa quando um sinal chega por eles. Acrescentar a flag
// resolve (é o mesmo conserto que o Wails usa).
static void consertar_sinal(int sinal) {
	struct sigaction sa;
	if (sigaction(sinal, NULL, &sa) == 0 && sa.sa_handler != SIG_DFL && sa.sa_handler != SIG_IGN) {
		sa.sa_flags |= SA_ONSTACK;
		sigaction(sinal, &sa, NULL);
	}
}

static void consertar_sinais(void) {
	int sinais[] = {SIGCHLD, SIGHUP, SIGINT, SIGQUIT, SIGABRT, SIGFPE, SIGTERM, SIGBUS, SIGSEGV, SIGXCPU, SIGXFSZ, SIGUSR1, SIGUSR2, SIGPIPE};
	for (unsigned i = 0; i < sizeof(sinais) / sizeof(sinais[0]); i++) {
		consertar_sinal(sinais[i]);
	}
}

// O título da janela acompanha o da página ("Cupom (rede) · Bobina").
static void ao_mudar_titulo(WebKitWebView *web, GParamSpec *p, gpointer janela) {
	const char *t = webkit_web_view_get_title(web);
	if (t != NULL && t[0] != 0) {
		gtk_window_set_title(GTK_WINDOW(janela), t);
	}
}

// Uma janela GTK com um WebKitWebView ocupando tudo. Fechar a janela
// encerra o laço do GTK e devolve o controle para o Go.
static int abrir_janela(const char *url, const char *titulo, int largura, int altura, const char *icone) {
	if (!gtk_init_check(NULL, NULL)) {
		return 0;
	}
	GtkWidget *janela = gtk_window_new(GTK_WINDOW_TOPLEVEL);
	gtk_window_set_title(GTK_WINDOW(janela), titulo);
	gtk_window_set_default_size(GTK_WINDOW(janela), largura, altura);
	gtk_window_set_position(GTK_WINDOW(janela), GTK_WIN_POS_CENTER);
	if (icone != NULL && icone[0] != 0) {
		gtk_window_set_icon_from_file(GTK_WINDOW(janela), icone, NULL);
	}

	WebKitWebView *web = WEBKIT_WEB_VIEW(webkit_web_view_new());
	WebKitSettings *ajustes = webkit_web_view_get_settings(web);
	webkit_settings_set_javascript_can_access_clipboard(ajustes, TRUE);
	webkit_settings_set_enable_developer_extras(ajustes, TRUE);

	gtk_container_add(GTK_CONTAINER(janela), GTK_WIDGET(web));
	g_signal_connect(janela, "destroy", G_CALLBACK(gtk_main_quit), NULL);
	g_signal_connect(web, "notify::title", G_CALLBACK(ao_mudar_titulo), janela);
	webkit_web_view_load_uri(web, url);
	gtk_widget_show_all(janela);
	consertar_sinais();
	gtk_main();
	return 1;
}
*/
import "C"

import (
	"os"
	"path/filepath"
	"unsafe"
)

// Abrir mostra a tela numa janela GTK e espera ela fechar. icone é o
// conteúdo de um SVG ou PNG para a barra de tarefas (opcional).
func Abrir(url string, icone []byte) error {
	// O renderizador DMABUF do WebKitGTK deixa áreas sem pintar em alguns
	// drivers de vídeo (retângulos com o fundo errado). Sem ele a tela fica
	// correta e o desempenho não muda para uma interface como esta.
	if os.Getenv("WEBKIT_DISABLE_DMABUF_RENDERER") == "" {
		os.Setenv("WEBKIT_DISABLE_DMABUF_RENDERER", "1")
	}
	caminhoIcone := ""
	if len(icone) > 0 {
		// Na pasta de cache do usuário, não no /tmp: lá outro usuário poderia
		// deixar um atalho com esse nome apontando para um arquivo nosso.
		if cache, err := os.UserCacheDir(); err == nil {
			arq := filepath.Join(cache, "bobina", "icone.svg")
			if os.MkdirAll(filepath.Dir(arq), 0o700) == nil && os.WriteFile(arq, icone, 0o600) == nil {
				caminhoIcone = arq
			}
		}
	}
	cURL, cTitulo, cIcone := C.CString(url), C.CString(titulo), C.CString(caminhoIcone)
	defer C.free(unsafe.Pointer(cURL))
	defer C.free(unsafe.Pointer(cTitulo))
	defer C.free(unsafe.Pointer(cIcone))
	if C.abrir_janela(cURL, cTitulo, largura, altura, cIcone) == 0 {
		return ErrSemJanelaNativa
	}
	return nil
}
