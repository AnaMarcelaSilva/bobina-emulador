// Package web serve a tela do emulador e a API que ela (e os testes
// automatizados) usam.
package web

import (
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"bobina/internal/escpos"
	"bobina/internal/impressora"
	"bobina/internal/zpl"
)

//go:embed ui
var arquivosUI embed.FS

type servidor struct {
	g *impressora.Gerenciador
}

// Rotas monta o roteador com a API e a tela.
func Rotas(g *impressora.Gerenciador) http.Handler {
	s := &servidor{g: g}
	m := http.NewServeMux()

	m.HandleFunc("GET /api/impressoras", s.listar)
	m.HandleFunc("POST /api/impressoras", s.criar)
	m.HandleFunc("PUT /api/impressoras/{id}", s.atualizar)
	m.HandleFunc("DELETE /api/impressoras/{id}", s.remover)
	m.HandleFunc("PATCH /api/impressoras/{id}/estado", s.alterarEstado)
	m.HandleFunc("GET /api/impressoras/{id}/trabalhos", s.trabalhos)
	m.HandleFunc("DELETE /api/impressoras/{id}/trabalhos", s.limpar)
	m.HandleFunc("GET /api/impressoras/{id}/eventos", s.eventos)
	m.HandleFunc("POST /api/impressoras/{id}/imprimir", s.imprimir)
	m.HandleFunc("POST /api/impressoras/{id}/teste/{amostra}", s.teste)
	m.HandleFunc("GET /api/trabalhos/{id}", s.trabalho)
	m.HandleFunc("GET /api/trabalhos/{id}/bruto", s.bruto)
	m.HandleFunc("POST /api/trabalhos/{id}/salvar", s.salvar)
	m.HandleFunc("GET /api/portas-seriais", func(w http.ResponseWriter, r *http.Request) {
		responder(w, impressora.PortasSeriais())
	})
	m.HandleFunc("GET /api/paginas-codigo", func(w http.ResponseWriter, r *http.Request) {
		responder(w, escpos.PaginasDisponiveis())
	})
	m.HandleFunc("GET /api/notificacoes", s.notificacoes)

	ui, _ := fs.Sub(arquivosUI, "ui")
	m.Handle("GET /", http.FileServerFS(ui))
	return m
}

func responder(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func falhar(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"erro": err.Error()})
}

func (s *servidor) impressora(w http.ResponseWriter, r *http.Request) *impressora.Impressora {
	i := s.g.Buscar(r.PathValue("id"))
	if i == nil {
		falhar(w, http.StatusNotFound, fmt.Errorf("impressora %q não encontrada", r.PathValue("id")))
	}
	return i
}

func (s *servidor) listar(w http.ResponseWriter, r *http.Request) {
	responder(w, s.g.Lista())
}

func (s *servidor) criar(w http.ResponseWriter, r *http.Request) {
	var c impressora.Config
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		falhar(w, http.StatusBadRequest, err)
		return
	}
	c.Estado = impressora.Estado{Ligada: true, Respostas: impressora.RespondeTodos}
	res, err := s.g.Criar(c)
	if err != nil {
		falhar(w, http.StatusBadRequest, err)
		return
	}
	w.WriteHeader(http.StatusCreated)
	responder(w, res)
}

func (s *servidor) atualizar(w http.ResponseWriter, r *http.Request) {
	var c impressora.Config
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		falhar(w, http.StatusBadRequest, err)
		return
	}
	res, err := s.g.Atualizar(r.PathValue("id"), c)
	if err != nil {
		falhar(w, http.StatusBadRequest, err)
		return
	}
	responder(w, res)
}

func (s *servidor) remover(w http.ResponseWriter, r *http.Request) {
	if err := s.g.Remover(r.PathValue("id")); err != nil {
		falhar(w, http.StatusNotFound, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// alterarEstado aceita só os campos que mudam: {"semPapel": true}.
func (s *servidor) alterarEstado(w http.ResponseWriter, r *http.Request) {
	i := s.impressora(w, r)
	if i == nil {
		return
	}
	corpo, err := io.ReadAll(r.Body)
	if err != nil {
		falhar(w, http.StatusBadRequest, err)
		return
	}
	var erroJSON error
	i.AlterarEstado(func(e *impressora.Estado) {
		novo := *e
		if erroJSON = json.Unmarshal(corpo, &novo); erroJSON == nil {
			*e = novo
		}
	}, "")
	if erroJSON != nil {
		falhar(w, http.StatusBadRequest, erroJSON)
		return
	}
	responder(w, i.Resumo())
}

// trabalhos lista os trabalhos. Com ?desde=ID devolve só os mais novos e, com
// ?aguardar=5s, espera chegar algum: útil em teste automatizado
// ("imprime e confere o que saiu").
func (s *servidor) trabalhos(w http.ResponseWriter, r *http.Request) {
	i := s.impressora(w, r)
	if i == nil {
		return
	}
	desde, _ := strconv.ParseInt(r.URL.Query().Get("desde"), 10, 64)
	aguardar, _ := time.ParseDuration(r.URL.Query().Get("aguardar"))
	filtrar := func() []*impressora.Trabalho {
		var r []*impressora.Trabalho
		for _, t := range i.Trabalhos() {
			if t.ID > desde {
				r = append(r, t)
			}
		}
		return r
	}
	lista := filtrar()
	if len(lista) == 0 && aguardar > 0 {
		ch, cancelar := s.g.Assinar()
		defer cancelar()
		limite := time.After(min(aguardar, 2*time.Minute))
	espera:
		for {
			select {
			case n := <-ch:
				if n.Tipo == "trabalho" && n.Impressora == i.Config().ID {
					lista = filtrar()
					break espera
				}
			case <-limite:
				break espera
			case <-r.Context().Done():
				return
			}
		}
	}
	if lista == nil {
		lista = []*impressora.Trabalho{}
	}
	responder(w, lista)
}

func (s *servidor) limpar(w http.ResponseWriter, r *http.Request) {
	if i := s.impressora(w, r); i != nil {
		i.Limpar()
		w.WriteHeader(http.StatusNoContent)
	}
}

func (s *servidor) eventos(w http.ResponseWriter, r *http.Request) {
	if i := s.impressora(w, r); i != nil {
		responder(w, i.Eventos())
	}
}

// imprimir recebe os bytes crus no corpo, como se viessem pela conexão.
func (s *servidor) imprimir(w http.ResponseWriter, r *http.Request) {
	i := s.impressora(w, r)
	if i == nil {
		return
	}
	dados, err := io.ReadAll(io.LimitReader(r.Body, 32<<20))
	if err != nil {
		falhar(w, http.StatusBadRequest, err)
		return
	}
	origem := r.URL.Query().Get("origem")
	if origem == "" {
		origem = "API"
	}
	novos := i.Imprimir(dados, origem)
	if novos == nil {
		novos = []*impressora.Trabalho{}
	}
	responder(w, novos)
}

func (s *servidor) teste(w http.ResponseWriter, r *http.Request) {
	i := s.impressora(w, r)
	if i == nil {
		return
	}
	cfg := i.Config()
	dados, ok := amostras(string(cfg.Linguagem), cfg.Colunas)[r.PathValue("amostra")]
	if !ok {
		falhar(w, http.StatusNotFound, errors.New("amostra desconhecida"))
		return
	}
	responder(w, i.Imprimir(dados, "teste"))
}

// detalhe é o trabalho com a lista de comandos, para o inspetor.
type detalhe struct {
	*impressora.Trabalho
	Comandos []comandoInspetor `json:"comandos"`
}

type comandoInspetor struct {
	Offset    int    `json:"offset"`
	Tamanho   int    `json:"tamanho"`
	Tipo      string `json:"tipo"`
	Nome      string `json:"nome"`
	Descricao string `json:"descricao"`
	Hex       string `json:"hex"`
	Texto     string `json:"texto,omitempty"`
}

func (s *servidor) buscarTrabalho(w http.ResponseWriter, r *http.Request) *impressora.Trabalho {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	t := s.g.Trabalho(id)
	if t == nil {
		falhar(w, http.StatusNotFound, fmt.Errorf("trabalho %d não encontrado (só os 200 mais recentes ficam guardados)", id))
	}
	return t
}

func (s *servidor) trabalho(w http.ResponseWriter, r *http.Request) {
	t := s.buscarTrabalho(w, r)
	if t == nil {
		return
	}
	bruto := t.Bruto()
	d := detalhe{Trabalho: t}
	resumirHex := func(b []byte) string {
		if len(b) > 32 {
			return impressora.Hex(b[:32]) + fmt.Sprintf(" … (+%d bytes)", len(b)-32)
		}
		return impressora.Hex(b)
	}
	if t.Linguagem == impressora.ZPL {
		for _, c := range zpl.Ler(bruto) {
			trecho := bruto[c.Offset : c.Offset+c.Tamanho]
			d.Comandos = append(d.Comandos, comandoInspetor{c.Offset, c.Tamanho, c.Tipo, c.Nome, c.Descricao, resumirHex(trecho), resumirTexto(string(trecho))})
		}
	} else {
		cfg := impressora.Config{PaginaCodigo: 2}
		if i := s.g.Buscar(t.Impressora); i != nil {
			cfg = i.Config()
		}
		modo := escpos.ModoESCPOS
		if t.ModoComandos == "bematech" {
			modo = escpos.ModoBematech
		}
		for _, c := range escpos.LerNoModo(bruto, modo) {
			ci := comandoInspetor{c.Offset, c.Tamanho, string(c.Tipo), c.Nome, c.Descricao, resumirHex(c.Bytes), ""}
			if c.Tipo == escpos.Texto {
				ci.Texto = resumirTexto(escpos.Decodificar(c.Bytes, cfg.PaginaCodigo))
			}
			d.Comandos = append(d.Comandos, ci)
		}
	}
	responder(w, d)
}

func resumirTexto(s string) string {
	r := []rune(s)
	if len(r) > 80 {
		return string(r[:80]) + "…"
	}
	return s
}

func (s *servidor) bruto(w http.ResponseWriter, r *http.Request) {
	t := s.buscarTrabalho(w, r)
	if t == nil {
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="trabalho-%d.prn"`, t.ID))
	_, _ = w.Write(t.Bruto())
}

// notificacoes mantém a tela atualizada em tempo real (Server-Sent Events).
func (s *servidor) notificacoes(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		falhar(w, http.StatusInternalServerError, errors.New("streaming não suportado"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	ch, cancelar := s.g.Assinar()
	defer cancelar()
	fmt.Fprint(w, ": conectado\n\n")
	fl.Flush()
	pulso := time.NewTicker(20 * time.Second)
	defer pulso.Stop()
	for {
		select {
		case n, aberto := <-ch:
			if !aberto {
				return
			}
			b, _ := json.Marshal(n)
			fmt.Fprintf(w, "data: %s\n\n", b)
			fl.Flush()
		case <-pulso.C:
			fmt.Fprint(w, ": pulso\n\n")
			fl.Flush()
		case <-r.Context().Done():
			return
		}
	}
}

// salvar grava os bytes do trabalho na pasta Downloads. Numa janela de
// desktop não existe o "baixar" do navegador, então o emulador grava e diz onde.
func (s *servidor) salvar(w http.ResponseWriter, r *http.Request) {
	t := s.buscarTrabalho(w, r)
	if t == nil {
		return
	}
	pasta := pastaDownloads()
	if err := os.MkdirAll(pasta, 0o755); err != nil {
		falhar(w, http.StatusInternalServerError, err)
		return
	}
	caminho := filepath.Join(pasta, fmt.Sprintf("bobina-trabalho-%d.prn", t.ID))
	if err := os.WriteFile(caminho, t.Bruto(), 0o644); err != nil {
		falhar(w, http.StatusInternalServerError, err)
		return
	}
	responder(w, map[string]string{"caminho": caminho})
}

// pastaDownloads respeita a pasta configurada no Linux (user-dirs.dirs), que
// muda conforme o idioma do sistema.
func pastaDownloads() string {
	casa, _ := os.UserHomeDir()
	if cfg, err := os.ReadFile(filepath.Join(casa, ".config", "user-dirs.dirs")); err == nil {
		for _, linha := range strings.Split(string(cfg), "\n") {
			if v, ok := strings.CutPrefix(strings.TrimSpace(linha), "XDG_DOWNLOAD_DIR="); ok {
				v = strings.ReplaceAll(strings.Trim(v, `"`), "$HOME", casa)
				if v != "" {
					return v
				}
			}
		}
	}
	return filepath.Join(casa, "Downloads")
}
