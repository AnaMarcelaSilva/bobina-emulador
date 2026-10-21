package escpos

import "fmt"

// Comandos do ESC/Bematech, o modo próprio das impressoras Bematech (MP-4200
// e família). A maior parte é igual ao ESC/POS; aqui ficam só os que mudam de
// tamanho ou de sentido, conferidos com o driver Bematech do serviço de
// impressão.

// lerBematech trata o que é diferente no modo Bematech. tratou=false quando o
// comando é igual ao do ESC/POS e deve seguir a leitura comum.
func lerBematech(b []byte) (c Comando, ok bool, tratou bool) {
	switch b[0] {
	case 0x0F: // SI
		return Comando{Tamanho: 1, Tipo: Formatacao, Nome: "SI", Descricao: "Liga a fonte condensada", Bytes: b[:1]}, true, true
	case 0x12: // DC2
		return Comando{Tamanho: 1, Tipo: Formatacao, Nome: "DC2", Descricao: "Volta à fonte normal (desliga o condensado)", Bytes: b[:1]}, true, true
	case 0x1B:
		if len(b) < 2 {
			return Comando{}, false, true
		}
		switch b[1] {
		case 'E':
			c, ok = cmd(b, 2, Formatacao, "ESC E", "Liga o negrito (sem parâmetro no modo Bematech)")
			return c, ok, true
		case 'F':
			c, ok = cmd(b, 2, Formatacao, "ESC F", "Desliga o negrito")
			return c, ok, true
		case 0x0F:
			c, ok = cmd(b, 2, Formatacao, "ESC SI", "Liga a fonte condensada")
			return c, ok, true
		case 0x12:
			c, ok = cmd(b, 2, Formatacao, "ESC DC2", "Desliga a fonte condensada")
			return c, ok, true
		case 'W':
			c, ok = cmd(b, 3, Formatacao, "ESC W", "Expandido (largura dupla): "+map[bool]string{true: "liga", false: "desliga"}[len(b) > 2 && b[2]&1 == 1])
			return c, ok, true
		case 'v':
			c, ok = cmd(b, 3, Gaveta, "ESC v", "Abre a gaveta")
			return c, ok, true
		}
	case 0x1D:
		// GS k Q a b c d nL nH dados: QR Code no formato Bematech.
		if len(b) >= 3 && b[1] == 'k' && b[2] == 'Q' {
			if len(b) < 9 {
				return Comando{}, false, true
			}
			n := int(b[7]) + int(b[8])*256
			c, ok = cmd(b, 9+n, CodigoBarras, "GS k Q", fmt.Sprintf("QR Code (formato Bematech) com %d bytes", n))
			return c, ok, true
		}
	}
	return Comando{}, false, false
}

// lerTrocaModo lê o GS F9 da Bematech. "GS F9 5 n" troca o conjunto de
// comandos (0 = ESC/Bematech, 1 = ESC/POS); "GS F9 SP n" faz o mesmo e grava
// na memória da impressora, valendo também depois de desligar.
func lerTrocaModo(b []byte) (Comando, bool) {
	if len(b) < 4 {
		return Comando{}, false
	}
	c, ok := cmd(b, 4, Controle, "GS F9", "Configuração da Bematech")
	switch b[2] {
	case 0x35, 0x20:
		c.NovoModo = ModoBematech
		if b[3]&1 == 1 {
			c.NovoModo = ModoESCPOS
		}
		c.Grava = b[2] == 0x20
		c.Descricao = "Muda os comandos para " + NomeModo(c.NovoModo)
		if c.Grava {
			c.Descricao += " e grava na memória da impressora"
		}
	case 0x37:
		c.Descricao = "Modo internacional da Bematech"
	}
	return c, ok
}
