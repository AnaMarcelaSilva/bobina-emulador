package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"bobina/internal/impressora"
)

func TestAPI(t *testing.T) {
	// Começa sem impressoras, para não ocupar as portas 9100 e 9101 da máquina.
	arquivo := filepath.Join(t.TempDir(), "impressoras.json")
	if err := os.WriteFile(arquivo, []byte("[]"), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := impressora.Novo(arquivo)
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(Rotas(g))
	defer srv.Close()
	defer g.Encerrar()

	pedir := func(metodo, caminho, corpo string, alvo any) int {
		t.Helper()
		req, _ := http.NewRequest(metodo, srv.URL+caminho, strings.NewReader(corpo))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if alvo != nil {
			_ = json.NewDecoder(resp.Body).Decode(alvo)
		}
		return resp.StatusCode
	}

	var criada impressora.Resumo
	if s := pedir("POST", "/api/impressoras", `{"nome":"Caixa 2","linguagem":"escpos","conexao":"pasta","pasta":"`+filepath.ToSlash(t.TempDir())+`"}`, &criada); s != http.StatusCreated {
		t.Fatalf("criar devolveu %d", s)
	}
	if criada.ID != "caixa-2" || !criada.Estado.Ligada {
		t.Fatalf("impressora criada errada: %+v", criada)
	}

	var resumo impressora.Resumo
	pedir("PATCH", "/api/impressoras/caixa-2/estado", `{"tampaAberta":true}`, &resumo)
	if !resumo.Estado.TampaAberta || !resumo.Estado.Ligada {
		t.Fatalf("o PATCH deveria mudar só a tampa: %+v", resumo.Estado)
	}

	// Um teste automatizado espera o trabalho enquanto o sistema imprime.
	chegou := make(chan []impressora.Trabalho, 1)
	go func() {
		var lista []impressora.Trabalho
		pedir("GET", "/api/impressoras/caixa-2/trabalhos?desde=0&aguardar=5s", "", &lista)
		chegou <- lista
	}()
	time.Sleep(200 * time.Millisecond)
	pedir("POST", "/api/impressoras/caixa-2/imprimir", "\x1b@TOTAL 10,00\n\x1dV\x00", nil)
	select {
	case lista := <-chegou:
		if len(lista) != 1 || lista[0].Texto != "TOTAL 10,00" || lista[0].Impresso || lista[0].Motivo != "tampa aberta" {
			t.Fatalf("trabalho esperado errado: %+v", lista)
		}
	case <-time.After(6 * time.Second):
		t.Fatal("o ?aguardar não devolveu o trabalho")
	}

	if s := pedir("GET", "/api/impressoras/nao-existe/trabalhos", "", nil); s != http.StatusNotFound {
		t.Errorf("impressora inexistente devolveu %d", s)
	}
}

// Um site aberto no navegador não pode mexer na API local, nem por um POST
// "simples" de outro site nem por DNS rebinding.
func TestProtecaoContraOutrosSites(t *testing.T) {
	alvo := Protegido(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}), true)
	casos := []struct {
		nome, host, origem string
		esperado           int
	}{
		{"a própria tela", "127.0.0.1:8631", "http://127.0.0.1:8631", http.StatusNoContent},
		{"curl ou teste (sem Origin)", "127.0.0.1:8631", "", http.StatusNoContent},
		{"localhost", "localhost:8631", "", http.StatusNoContent},
		{"outro site", "127.0.0.1:8631", "https://site-malicioso.com", http.StatusForbidden},
		{"página sem origem (null)", "127.0.0.1:8631", "null", http.StatusForbidden},
		{"DNS rebinding", "site-malicioso.com:8631", "http://site-malicioso.com:8631", http.StatusForbidden},
	}
	for _, c := range casos {
		req := httptest.NewRequest("POST", "/api/impressoras", strings.NewReader(`{"nome":"x","conexao":"pasta","pasta":"~/.ssh"}`))
		req.Host = c.host
		if c.origem != "" {
			req.Header.Set("Origin", c.origem)
		}
		rec := httptest.NewRecorder()
		alvo.ServeHTTP(rec, req)
		if rec.Code != c.esperado {
			t.Errorf("%s: esperava %d, veio %d", c.nome, c.esperado, rec.Code)
		}
	}
}
