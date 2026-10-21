package impressora

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"time"

	"go.bug.st/serial"
)

// portaSerial lê uma porta serial e responde por ela. Com o campo Serial
// vazio, cria uma porta virtual (Linux): o sistema testado abre o caminho
// mostrado na tela como se fosse a impressora.
type portaSerial struct {
	rw       io.ReadWriteCloser
	limpar   func() // apaga o atalho da porta virtual
	parada   chan struct{}
	terminou sync.WaitGroup
}

func iniciarSerial(imp *Impressora, cfg Config) (conexao, string, error) {
	p := &portaSerial{parada: make(chan struct{})}
	var endereco string
	if cfg.Serial == "" {
		v, err := criarPortaVirtual(cfg.ID)
		if err != nil {
			return nil, "", err
		}
		p.rw, p.limpar, endereco = v.rw, v.limpar, v.descricao
	} else {
		porta, err := serial.Open(cfg.Serial, &serial.Mode{BaudRate: cfg.Baud})
		if err != nil {
			return nil, "", fmt.Errorf("não foi possível abrir %s: %v", cfg.Serial, err)
		}
		p.rw = porta
		endereco = fmt.Sprintf("%s (%d bps)", cfg.Serial, cfg.Baud)
	}

	var escritaMu sync.Mutex
	sessao := novaSessao(imp, "serial", func(b []byte) error {
		escritaMu.Lock()
		defer escritaMu.Unlock()
		_, err := p.rw.Write(b)
		return err
	})
	p.terminou.Add(1)
	go func() {
		defer p.terminou.Done()
		defer sessao.Encerrar()
		buf := make([]byte, 16*1024)
		for {
			n, err := p.rw.Read(buf)
			if n > 0 {
				sessao.Receber(append([]byte{}, buf[:n]...))
			}
			if err != nil {
				select {
				case <-p.parada:
				default:
					imp.evento("erro", "A porta serial parou: "+err.Error(), "", 0)
				}
				return
			}
		}
	}()
	return p, endereco, nil
}

func (p *portaSerial) Parar() {
	close(p.parada)
	p.rw.Close()
	// A leitura costuma soltar na hora; o limite evita travar se o sistema
	// operacional demorar a liberar a porta.
	pronto := make(chan struct{})
	go func() { p.terminou.Wait(); close(pronto) }()
	select {
	case <-pronto:
	case <-time.After(time.Second):
	}
	if p.limpar != nil {
		p.limpar()
	}
}

// PortasSeriais lista as portas do computador, para a tela de configuração.
func PortasSeriais() []string {
	portas, _ := serial.GetPortsList()
	sort.Strings(portas)
	return portas
}

type portaVirtual struct {
	rw        io.ReadWriteCloser
	limpar    func()
	descricao string
}
