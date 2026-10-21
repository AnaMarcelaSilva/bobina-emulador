package impressora

import (
	"errors"
	"fmt"
	"net"
	"sync"
)

// servidorRede é a impressora de rede: aceita conexões TCP (a porta 9100 é o
// padrão das impressoras) e mantém cada conexão aberta enquanto o sistema
// quiser, respondendo status no meio do caminho como uma impressora real.
type servidorRede struct {
	ln       net.Listener
	mu       sync.Mutex
	conexoes map[net.Conn]bool
}

func iniciarRede(imp *Impressora, cfg Config) (conexao, string, error) {
	endereco := fmt.Sprintf("%s:%d", cfg.Host, cfg.Porta)
	ln, err := net.Listen("tcp", endereco)
	if err != nil {
		return nil, "", fmt.Errorf("a porta %d não está livre (%v)", cfg.Porta, err)
	}
	s := &servidorRede{ln: ln, conexoes: map[net.Conn]bool{}}
	go s.aceitar(imp)
	return s, endereco, nil
}

func (s *servidorRede) aceitar(imp *Impressora) {
	for {
		c, err := s.ln.Accept()
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				imp.evento("erro", "Erro ao aceitar conexão: "+err.Error(), "", 0)
			}
			return
		}
		s.mu.Lock()
		s.conexoes[c] = true
		s.mu.Unlock()
		go s.atender(imp, c)
	}
}

func (s *servidorRede) atender(imp *Impressora, c net.Conn) {
	origem := c.RemoteAddr().String()
	imp.evento("conexao", "Conexão aberta por "+origem, "", 0)
	var escritaMu sync.Mutex
	sessao := novaSessao(imp, origem, func(b []byte) error {
		escritaMu.Lock()
		defer escritaMu.Unlock()
		_, err := c.Write(b)
		return err
	})
	buf := make([]byte, 64*1024)
	for {
		n, err := c.Read(buf)
		if n > 0 {
			sessao.Receber(append([]byte{}, buf[:n]...))
		}
		if err != nil {
			break
		}
	}
	sessao.Encerrar()
	c.Close()
	s.mu.Lock()
	delete(s.conexoes, c)
	s.mu.Unlock()
	imp.evento("conexao", "Conexão fechada por "+origem, "", 0)
}

// Parar fecha a porta e derruba as conexões abertas, como uma impressora
// que foi desligada.
func (s *servidorRede) Parar() {
	s.ln.Close()
	s.mu.Lock()
	for c := range s.conexoes {
		c.Close()
	}
	s.mu.Unlock()
}
