package impressora

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
)

// Notificacao é enviada para a tela a cada mudança (via Server-Sent Events).
type Notificacao struct {
	Tipo       string `json:"tipo"` // impressora, removida, trabalho, evento, limpa
	Impressora string `json:"impressora"`
	Dados      any    `json:"dados,omitempty"`
}

// Gerenciador cuida de todas as impressoras e do arquivo de configuração.
type Gerenciador struct {
	arquivo string

	mu          sync.Mutex
	impressoras []*Impressora

	assinantesMu sync.Mutex
	assinantes   map[chan Notificacao]bool

	ultimoID atomic.Int64
}

// Novo carrega a configuração (ou cria as impressoras de exemplo na primeira
// vez) e liga todas.
func Novo(arquivo string) (*Gerenciador, error) {
	g := &Gerenciador{arquivo: arquivo, assinantes: map[chan Notificacao]bool{}}
	var cfgs []Config
	conteudo, err := os.ReadFile(arquivo)
	switch {
	case errors.Is(err, os.ErrNotExist):
		cfgs = exemplos()
	case err != nil:
		return nil, err
	default:
		if err := json.Unmarshal(conteudo, &cfgs); err != nil {
			return nil, fmt.Errorf("o arquivo %s não é uma configuração válida: %w", arquivo, err)
		}
	}
	for _, c := range cfgs {
		c = completar(c)
		g.impressoras = append(g.impressoras, &Impressora{g: g, cfg: c})
	}
	for _, i := range g.impressoras {
		i.iniciar()
	}
	g.salvar()
	return g, nil
}

// exemplos são as impressoras criadas na primeira execução: uma de cupom e
// uma de etiqueta na rede e, no Linux, uma de cupom numa serial virtual.
func exemplos() []Config {
	ligada := Estado{Ligada: true, Respostas: RespondeTodos}
	r := []Config{
		{ID: "cupom-rede", Nome: "Cupom (rede)", Cor: "#0ea5e9", Linguagem: ESCPOS, Conexao: Rede,
			Host: "0.0.0.0", Porta: 9100, LarguraPapel: 80, Colunas: 48, PaginaCodigo: 2, Estado: ligada},
		{ID: "etiqueta-rede", Nome: "Etiqueta (rede)", Cor: "#a855f7", Linguagem: ZPL, Conexao: Rede,
			Host: "0.0.0.0", Porta: 9101, Dpmm: 8, LarguraMM: 60, AlturaMM: 40, Estado: ligada},
	}
	if runtime.GOOS == "linux" {
		r = append(r, Config{ID: "cupom-serial", Nome: "Cupom (serial)", Cor: "#f59e0b", Linguagem: ESCPOS, Conexao: Serial,
			Baud: 9600, LarguraPapel: 80, Colunas: 42, PaginaCodigo: 2, Estado: ligada})
	}
	return r
}

// completar preenche o que faltar com valores que funcionam.
func completar(c Config) Config {
	if c.Linguagem == "" {
		c.Linguagem = ESCPOS
	}
	if c.Conexao == "" {
		c.Conexao = Rede
	}
	if c.Conexao == Rede {
		if c.Host == "" {
			c.Host = "0.0.0.0"
		}
		if c.Porta == 0 {
			c.Porta = 9100
		}
	}
	if c.Conexao == Serial && c.Baud == 0 {
		c.Baud = 9600
	}
	if c.Linguagem == ESCPOS {
		if c.LarguraPapel == 0 {
			c.LarguraPapel = 80
		}
		if c.Colunas == 0 {
			c.Colunas = map[bool]int{true: 32, false: 48}[c.LarguraPapel <= 60]
		}
	} else {
		if c.Dpmm == 0 {
			c.Dpmm = 8
		}
		if c.LarguraMM == 0 {
			c.LarguraMM = 100
		}
		if c.AlturaMM == 0 {
			c.AlturaMM = 150
		}
	}
	if c.Estado.Respostas == "" {
		c.Estado.Respostas = RespondeTodos
	}
	if c.Cor == "" {
		c.Cor = "#10b981"
	}
	return c
}

func (g *Gerenciador) proximoID() int64 { return g.ultimoID.Add(1) }

// Lista devolve o resumo de todas as impressoras, na ordem em que foram criadas.
func (g *Gerenciador) Lista() []Resumo {
	g.mu.Lock()
	imps := append([]*Impressora{}, g.impressoras...)
	g.mu.Unlock()
	r := make([]Resumo, 0, len(imps))
	for _, i := range imps {
		r = append(r, i.Resumo())
	}
	return r
}

// Buscar encontra uma impressora pelo id.
func (g *Gerenciador) Buscar(id string) *Impressora {
	g.mu.Lock()
	defer g.mu.Unlock()
	for _, i := range g.impressoras {
		if i.Config().ID == id {
			return i
		}
	}
	return nil
}

// validar confere a configuração e se a porta não está em uso por outra impressora.
func (g *Gerenciador) validar(c Config, ignorar string) error {
	if strings.TrimSpace(c.Nome) == "" {
		return errors.New("dê um nome para a impressora")
	}
	if c.Linguagem != ESCPOS && c.Linguagem != ZPL {
		return fmt.Errorf("linguagem inválida: %q", c.Linguagem)
	}
	switch c.Conexao {
	case Rede:
		if c.Porta < 1 || c.Porta > 65535 {
			return fmt.Errorf("porta inválida: %d", c.Porta)
		}
	case Serial:
		if c.Serial == "" && runtime.GOOS != "linux" {
			return errors.New("a porta serial virtual só é criada no Linux; no Windows, crie um par de portas com o com0com e informe uma delas")
		}
	case Pasta:
		if strings.TrimSpace(c.Pasta) == "" {
			return errors.New("informe a pasta onde os arquivos .prn vão chegar")
		}
	default:
		return fmt.Errorf("conexão inválida: %q", c.Conexao)
	}
	for _, outra := range g.Lista() {
		if outra.ID == ignorar {
			continue
		}
		if c.Conexao == Rede && outra.Conexao == Rede && c.Porta == outra.Porta {
			return fmt.Errorf("a porta %d já é usada pela impressora %q", c.Porta, outra.Nome)
		}
		if c.Conexao == Serial && outra.Conexao == Serial && c.Serial != "" && c.Serial == outra.Serial {
			return fmt.Errorf("a porta %s já é usada pela impressora %q", c.Serial, outra.Nome)
		}
		if c.Conexao == Pasta && outra.Conexao == Pasta && filepath.Clean(c.Pasta) == filepath.Clean(outra.Pasta) {
			return fmt.Errorf("a pasta %s já é usada pela impressora %q", c.Pasta, outra.Nome)
		}
	}
	return nil
}

var naoSlug = regexp.MustCompile(`[^a-z0-9]+`)

func (g *Gerenciador) novoIDPara(nome string) string {
	base := strings.Trim(naoSlug.ReplaceAllString(strings.ToLower(semAcento(nome)), "-"), "-")
	if base == "" {
		base = "impressora"
	}
	id := base
	for n := 2; g.Buscar(id) != nil; n++ {
		id = fmt.Sprintf("%s-%d", base, n)
	}
	return id
}

func semAcento(s string) string {
	return strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e", "í", "i",
		"ó", "o", "ô", "o", "õ", "o", "ú", "u", "ç", "c").Replace(s)
}

// Criar adiciona uma impressora e já liga.
func (g *Gerenciador) Criar(c Config) (Resumo, error) {
	c = completar(c)
	if err := g.validar(c, ""); err != nil {
		return Resumo{}, err
	}
	c.ID = g.novoIDPara(c.Nome)
	i := &Impressora{g: g, cfg: c}
	g.mu.Lock()
	g.impressoras = append(g.impressoras, i)
	g.mu.Unlock()
	i.iniciar()
	g.salvar()
	return i.Resumo(), nil
}

// Atualizar troca a configuração e reinicia a conexão. O estado do painel
// (sem papel etc.) é mantido.
func (g *Gerenciador) Atualizar(id string, c Config) (Resumo, error) {
	i := g.Buscar(id)
	if i == nil {
		return Resumo{}, fmt.Errorf("impressora %q não encontrada", id)
	}
	atual := i.Config()
	c.ID = id
	c.Estado = atual.Estado
	c = completar(c)
	if err := g.validar(c, id); err != nil {
		return Resumo{}, err
	}
	i.parar()
	i.mu.Lock()
	i.cfg = c
	i.mu.Unlock()
	i.evento("estado", "Configuração alterada", "", 0)
	i.iniciar()
	g.salvar()
	return i.Resumo(), nil
}

// Remover desliga e apaga a impressora.
func (g *Gerenciador) Remover(id string) error {
	i := g.Buscar(id)
	if i == nil {
		return fmt.Errorf("impressora %q não encontrada", id)
	}
	i.mu.Lock()
	i.removida = true
	i.mu.Unlock()
	i.parar()
	g.mu.Lock()
	for k, outra := range g.impressoras {
		if outra == i {
			g.impressoras = append(g.impressoras[:k], g.impressoras[k+1:]...)
			break
		}
	}
	g.mu.Unlock()
	g.salvar()
	g.publicar(Notificacao{Tipo: "removida", Impressora: id})
	return nil
}

// Trabalho procura um trabalho pelo id em todas as impressoras.
func (g *Gerenciador) Trabalho(id int64) *Trabalho {
	g.mu.Lock()
	imps := append([]*Impressora{}, g.impressoras...)
	g.mu.Unlock()
	for _, i := range imps {
		for _, t := range i.Trabalhos() {
			if t.ID == id {
				return t
			}
		}
	}
	return nil
}

// Encerrar desliga todas as conexões (ao fechar o programa).
func (g *Gerenciador) Encerrar() {
	g.mu.Lock()
	imps := append([]*Impressora{}, g.impressoras...)
	g.mu.Unlock()
	for _, i := range imps {
		i.parar()
	}
}

func (g *Gerenciador) salvar() {
	if g.arquivo == "" {
		return
	}
	g.mu.Lock()
	cfgs := make([]Config, 0, len(g.impressoras))
	for _, i := range g.impressoras {
		cfgs = append(cfgs, i.Config())
	}
	g.mu.Unlock()
	b, _ := json.MarshalIndent(cfgs, "", "  ")
	if err := os.MkdirAll(filepath.Dir(g.arquivo), 0o755); err == nil {
		tmp := g.arquivo + ".tmp"
		if os.WriteFile(tmp, b, 0o644) == nil {
			_ = os.Rename(tmp, g.arquivo)
		}
	}
}

// Assinar recebe as notificações até chamar a função devolvida.
func (g *Gerenciador) Assinar() (<-chan Notificacao, func()) {
	ch := make(chan Notificacao, 256)
	g.assinantesMu.Lock()
	g.assinantes[ch] = true
	g.assinantesMu.Unlock()
	return ch, func() {
		g.assinantesMu.Lock()
		if g.assinantes[ch] {
			delete(g.assinantes, ch)
			close(ch)
		}
		g.assinantesMu.Unlock()
	}
}

func (g *Gerenciador) publicar(n Notificacao) {
	g.assinantesMu.Lock()
	defer g.assinantesMu.Unlock()
	for ch := range g.assinantes {
		select {
		case ch <- n:
		default: // tela lenta: perde a notificação em vez de travar a impressora
		}
	}
}
