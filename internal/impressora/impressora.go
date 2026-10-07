package impressora

import (
	"fmt"
	"sync"
	"time"

	"bobina/internal/escpos"
	"bobina/internal/zpl"
)

const (
	maxTrabalhos = 200
	maxEventos   = 400
)

// conexao é o que mantém a impressora escutando (servidor TCP, porta serial,
// vigia da pasta). Parar libera a porta.
type conexao interface {
	Parar()
}

// Impressora é uma impressora virtual em funcionamento.
type Impressora struct {
	g *Gerenciador

	mu        sync.Mutex
	cfg       Config
	trabalhos []*Trabalho
	eventos   []Evento
	con       conexao
	endereco  string // onde ela está escutando, para mostrar ao usuário
	erro      string // por que não conseguiu escutar
	sessoes   map[*Sessao]bool
	removida  bool
	contador  int // numera os trabalhos desta impressora: 1, 2, 3...
}

// Resumo é o que a tela precisa saber de uma impressora.
type Resumo struct {
	Config
	Endereco  string          `json:"endereco"`
	Erro      string          `json:"erro,omitempty"`
	Conexoes  int             `json:"conexoes"`
	Trabalhos int             `json:"trabalhos"`
	Respostas []LinhaResposta `json:"respostas"`
}

// Config devolve uma cópia da configuração atual.
func (i *Impressora) Config() Config {
	i.mu.Lock()
	defer i.mu.Unlock()
	return i.cfg
}

func (i *Impressora) Resumo() Resumo {
	i.mu.Lock()
	defer i.mu.Unlock()
	return Resumo{
		Config:    i.cfg,
		Endereco:  i.endereco,
		Erro:      i.erro,
		Conexoes:  len(i.sessoes),
		Trabalhos: len(i.trabalhos),
		Respostas: TabelaRespostas(i.cfg),
	}
}

// iniciar abre a conexão configurada. Na rede, desligada quer dizer que a
// porta nem aceita conexão; na serial e na pasta a porta continua lá, mas
// ninguém responde.
func (i *Impressora) iniciar() {
	cfg := i.Config()
	con, endereco, err := iniciarConexao(i, cfg)
	i.mu.Lock()
	i.con, i.endereco, i.erro = con, endereco, ""
	if err != nil {
		i.erro = err.Error()
	}
	i.mu.Unlock()
	if err != nil {
		i.evento("erro", "Não foi possível iniciar: "+err.Error()+". Tentando de novo a cada 3 s.", "", 0)
		i.tentarDeNovo(cfg)
	} else if con != nil {
		i.evento("conexao", "Escutando em "+endereco, "", 0)
	}
	i.notificar()
}

func iniciarConexao(i *Impressora, cfg Config) (conexao, string, error) {
	switch cfg.Conexao {
	case Rede:
		if !cfg.Estado.Ligada {
			return nil, fmt.Sprintf("%s:%d (desligada: recusa conexões)", cfg.Host, cfg.Porta), nil
		}
		return iniciarRede(i, cfg)
	case Serial:
		return iniciarSerial(i, cfg)
	case Pasta:
		return iniciarPasta(i, cfg)
	}
	return nil, "", fmt.Errorf("tipo de conexão desconhecido: %q", cfg.Conexao)
}

// tentarDeNovo religa a impressora quando a porta estava ocupada (outro
// programa, ou uma instância anterior ainda fechando). Desiste quando a
// configuração muda, a impressora é desligada ou removida.
func (i *Impressora) tentarDeNovo(cfg Config) {
	time.AfterFunc(3*time.Second, func() {
		// O estado do painel pode mudar à vontade; só a configuração importa.
		aindaPrecisa := func() bool {
			atual := i.cfg
			atual.Estado = cfg.Estado
			return atual == cfg && i.con == nil && i.erro != "" && !i.removida &&
				(i.cfg.Conexao != Rede || i.cfg.Estado.Ligada)
		}
		i.mu.Lock()
		precisa := aindaPrecisa()
		i.mu.Unlock()
		if !precisa {
			return
		}
		con, endereco, err := iniciarConexao(i, cfg)
		if err != nil {
			i.tentarDeNovo(cfg)
			return
		}
		i.mu.Lock()
		if !aindaPrecisa() {
			i.mu.Unlock()
			if con != nil {
				con.Parar()
			}
			return
		}
		i.con, i.endereco, i.erro = con, endereco, ""
		i.mu.Unlock()
		i.evento("conexao", "Porta liberada. Escutando em "+endereco, "", 0)
		i.notificar()
	})
}

// parar fecha a conexão e as sessões abertas.
func (i *Impressora) parar() {
	i.mu.Lock()
	con := i.con
	i.con = nil
	var sessoes []*Sessao
	for s := range i.sessoes {
		sessoes = append(sessoes, s)
	}
	i.mu.Unlock()
	if con != nil {
		con.Parar()
	}
	for _, s := range sessoes {
		s.Encerrar()
	}
}

func (i *Impressora) registrarSessao(s *Sessao, ativa bool) {
	i.mu.Lock()
	if i.sessoes == nil {
		i.sessoes = map[*Sessao]bool{}
	}
	if ativa {
		i.sessoes[s] = true
	} else {
		delete(i.sessoes, s)
	}
	i.mu.Unlock()
	i.notificar()
}

// AlterarEstado muda o estado (pelo painel, pela API ou por um comando como
// o ~PS) e avisa quem estiver acompanhando.
func (i *Impressora) AlterarEstado(mudar func(*Estado), motivo string) {
	i.mu.Lock()
	antes := i.cfg.Estado
	mudar(&i.cfg.Estado)
	depois := i.cfg.Estado
	linguagem := i.cfg.Linguagem
	if depois.Respostas == "" {
		i.cfg.Estado.Respostas = RespondeTodos
		depois = i.cfg.Estado
	}
	var sessoes []*Sessao
	for s := range i.sessoes {
		sessoes = append(sessoes, s)
	}
	i.mu.Unlock()
	if antes == depois {
		return
	}
	if motivo == "" {
		motivo = descreverMudanca(antes, depois, linguagem)
	}
	i.evento("estado", motivo, "", 0)
	// Ligar ou desligar uma impressora de rede abre ou fecha a porta.
	if antes.Ligada != depois.Ligada && i.Config().Conexao == Rede {
		i.parar()
		i.iniciar()
	}
	for _, s := range sessoes {
		go s.enviarASB(depois)
	}
	i.g.salvar()
	i.notificar()
}

func descreverMudanca(a, d Estado, l Linguagem) string {
	nomes := []struct {
		antes, depois bool
		nome          string
	}{
		{a.Ligada, d.Ligada, "ligada"},
		{a.Offline, d.Offline, map[Linguagem]string{ESCPOS: "offline", ZPL: "pausada"}[l]},
		{a.SemPapel, d.SemPapel, "sem papel"},
		{a.PoucoPapel, d.PoucoPapel, "papel acabando"},
		{a.TampaAberta, d.TampaAberta, map[Linguagem]string{ESCPOS: "tampa aberta", ZPL: "cabeça aberta"}[l]},
		{a.ErroGuilhotina, d.ErroGuilhotina, "erro na guilhotina"},
		{a.GavetaAberta, d.GavetaAberta, "gaveta aberta"},
		{a.SemRibbon, d.SemRibbon, "sem ribbon"},
	}
	for _, n := range nomes {
		if n.antes != n.depois {
			if n.nome == "ligada" {
				if n.depois {
					return "Impressora ligada"
				}
				return "Impressora desligada"
			}
			if n.depois {
				return "Estado: " + n.nome
			}
			return "Estado: deixou de estar " + n.nome
		}
	}
	if a.Respostas != d.Respostas {
		return "Respostas de status: " + string(d.Respostas)
	}
	if a.AtrasoMs != d.AtrasoMs {
		return fmt.Sprintf("Atraso das respostas: %d ms", d.AtrasoMs)
	}
	return "Estado alterado"
}

// novoTrabalho desenha o que chegou e guarda.
func (i *Impressora) novoTrabalho(dados []byte, origem string, modo escpos.Modo) *Trabalho {
	cfg := i.Config()
	t := &Trabalho{
		ID:         i.g.proximoID(),
		Impressora: cfg.ID,
		Recebido:   time.Now(),
		Origem:     origem,
		Bytes:      len(dados),
		Linguagem:  cfg.Linguagem,
		bruto:      append([]byte{}, dados...),
	}
	if cfg.Linguagem == ZPL {
		e := zpl.Desenhar(dados, zpl.Midia{Dpmm: cfg.Dpmm, LarguraMM: cfg.LarguraMM, AlturaMM: cfg.AlturaMM})
		t.Visual, t.Texto, t.Avisos, t.Quantidade = e.SVG, e.Texto, e.Avisos, e.Quantidade
		t.LarguraMM = float64(e.Largura) / float64(max(cfg.Dpmm, 1))
		t.AlturaMM = float64(e.Altura) / float64(max(cfg.Dpmm, 1))
	} else {
		t.ModoComandos = nomeModoConfig(modo)
		c := escpos.Desenhar(escpos.LerNoModo(dados, modo), escpos.Papel{
			Colunas: cfg.Colunas, Pontos: pontosDoPapel(cfg.LarguraPapel), PaginaCodigo: cfg.PaginaCodigo,
		})
		t.Visual, t.Texto, t.Avisos = c.HTML, c.Texto, c.Avisos
		t.LarguraMM = float64(cfg.LarguraPapel)
	}
	t.Motivo = cfg.Estado.MotivoNaoImprime(cfg.Linguagem)
	t.Impresso = t.Motivo == ""

	i.mu.Lock()
	i.contador++
	t.Numero = i.contador
	i.trabalhos = append(i.trabalhos, t)
	if len(i.trabalhos) > maxTrabalhos {
		i.trabalhos = i.trabalhos[len(i.trabalhos)-maxTrabalhos:]
	}
	i.mu.Unlock()

	tipo := map[Linguagem]string{ESCPOS: "Cupom", ZPL: "Etiqueta"}[cfg.Linguagem]
	texto := fmt.Sprintf("%s impresso (%d bytes)", tipo, len(dados))
	if !t.Impresso {
		texto = fmt.Sprintf("%s recebido, mas não impresso: %s", tipo, t.Motivo)
	}
	if len(t.Avisos) > 0 {
		texto += fmt.Sprintf(" · %d aviso(s)", len(t.Avisos))
	}
	i.evento("trabalho", texto, origem, t.ID)
	i.g.publicar(Notificacao{Tipo: "trabalho", Impressora: cfg.ID, Dados: t})
	i.notificar()
	return t
}

// pontosDoPapel é a largura da cabeça de impressão em pontos (203 dpi).
func pontosDoPapel(mm int) int {
	if mm > 0 && mm <= 60 {
		return 384
	}
	return 576
}

func (i *Impressora) evento(tipo, texto, detalhe string, trabalho int64) {
	cfg := i.Config()
	e := Evento{ID: i.g.proximoID(), Impressora: cfg.ID, Hora: time.Now(), Tipo: tipo, Texto: texto, Detalhe: detalhe, Trabalho: trabalho}
	i.mu.Lock()
	i.eventos = append(i.eventos, e)
	if len(i.eventos) > maxEventos {
		i.eventos = i.eventos[len(i.eventos)-maxEventos:]
	}
	i.mu.Unlock()
	i.g.publicar(Notificacao{Tipo: "evento", Impressora: cfg.ID, Dados: e})
}

func (i *Impressora) notificar() {
	i.g.publicar(Notificacao{Tipo: "impressora", Impressora: i.Config().ID, Dados: i.Resumo()})
}

// Trabalhos devolve os trabalhos, do mais novo para o mais antigo.
func (i *Impressora) Trabalhos() []*Trabalho {
	i.mu.Lock()
	defer i.mu.Unlock()
	r := make([]*Trabalho, len(i.trabalhos))
	for k, t := range i.trabalhos {
		r[len(r)-1-k] = t
	}
	return r
}

// Eventos devolve a atividade, do mais novo para o mais antigo.
func (i *Impressora) Eventos() []Evento {
	i.mu.Lock()
	defer i.mu.Unlock()
	r := make([]Evento, len(i.eventos))
	for k, e := range i.eventos {
		r[len(r)-1-k] = e
	}
	return r
}

// Limpar apaga trabalhos e atividade e zera a contagem: a próxima
// impressão volta a ser a 0001.
func (i *Impressora) Limpar() {
	i.mu.Lock()
	i.trabalhos, i.eventos = nil, nil
	i.contador = 0
	i.mu.Unlock()
	i.notificar()
}

// Imprimir trata dados como se tivessem chegado pela conexão (usado pelo
// botão de teste, pelo arrastar arquivo e pela API).
func (i *Impressora) Imprimir(dados []byte, origem string) []*Trabalho {
	ultimo := int64(0)
	if t := i.Trabalhos(); len(t) > 0 {
		ultimo = t[0].ID
	}
	s := novaSessao(i, origem, nil)
	s.Receber(dados)
	s.Encerrar()
	var novos []*Trabalho
	for _, t := range i.Trabalhos() {
		if t.ID > ultimo {
			novos = append(novos, t)
		}
	}
	return novos
}

// trocouModo registra que o sistema trocou o conjunto de comandos (GS F9 da
// Bematech). Quando o comando pede para gravar, a impressora passa a ligar
// nesse modo, como a de verdade, e a configuração é salva.
func (i *Impressora) trocouModo(modo escpos.Modo, grava bool) {
	texto := "O sistema mudou os comandos para " + escpos.NomeModo(modo)
	if grava {
		i.mu.Lock()
		mudou := i.cfg.modoInicial() != modo
		i.cfg.ModoComandos = nomeModoConfig(modo)
		i.mu.Unlock()
		if !mudou {
			return // o driver grava o mesmo modo a cada impressão; não precisa avisar de novo
		}
		texto += " e gravou na memória da impressora"
		i.g.salvar()
		i.notificar()
	}
	i.evento("estado", texto, "", 0)
}
