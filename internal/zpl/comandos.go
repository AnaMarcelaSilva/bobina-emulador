// Package zpl lê e desenha etiquetas ZPL (Zebra) sem depender de serviço
// externo. Cobre os comandos que aparecem no dia a dia: texto, blocos de
// texto, caixas, códigos de barras, QR Code e imagens.
package zpl

import (
	"strings"
)

// Comando é um trecho da etiqueta já separado, no mesmo formato que o
// inspetor usa para o ESC/POS.
type Comando struct {
	Offset    int    `json:"offset"`
	Tamanho   int    `json:"tamanho"`
	Tipo      string `json:"tipo"`
	Nome      string `json:"nome"`
	Descricao string `json:"descricao"`

	Params string `json:"-"`
}

// Ler separa os comandos (^XX ou ~XX) e seus parâmetros.
func Ler(dados []byte) []Comando {
	var r []Comando
	i := 0
	for i < len(dados) {
		c := dados[i]
		if c != '^' && c != '~' {
			// Texto solto fora de comando (quebras de linha, espaços).
			j := i
			for j < len(dados) && dados[j] != '^' && dados[j] != '~' {
				j++
			}
			if strings.TrimSpace(string(dados[i:j])) != "" {
				r = append(r, Comando{Offset: i, Tamanho: j - i, Tipo: "desconhecido", Nome: "texto solto",
					Descricao: "Texto fora de um ^FD, a impressora ignora"})
			}
			i = j
			continue
		}
		nome := string(c)
		j := i + 1
		for k := 0; k < 2 && j < len(dados); k++ {
			nome += strings.ToUpper(string(dados[j]))
			j++
		}
		inicioParams := j
		for j < len(dados) && dados[j] != '^' && dados[j] != '~' {
			j++
		}
		params := string(limparQuebras(dados[inicioParams:j], nome))
		info := descricao(nome, params)
		r = append(r, Comando{Offset: i, Tamanho: j - i, Tipo: info.tipo, Nome: nome, Descricao: info.desc, Params: params})
		i = j
	}
	return r
}

// A impressora ignora quebras de linha entre comandos; dentro do ^FD elas
// também não entram no texto.
func limparQuebras(b []byte, nome string) []byte {
	s := strings.NewReplacer("\r", "", "\n", "").Replace(string(b))
	if nome != "^FD" && nome != "^FV" && nome != "^FX" {
		s = strings.TrimRight(s, " \t")
	}
	return []byte(s)
}

type infoCmd struct{ tipo, desc string }

func descricao(nome, p string) infoCmd {
	if strings.HasPrefix(nome, "^A") && nome != "^A@" {
		return infoCmd{"formatacao", "Fonte " + strings.TrimPrefix(nome, "^A") + " do próximo campo"}
	}
	if d, ok := descricoes[nome]; ok {
		return d
	}
	return infoCmd{"desconhecido", "Comando que o emulador não conhece"}
}

var descricoes = map[string]infoCmd{
	"^XA": {"controle", "Começa a etiqueta"},
	"^XZ": {"controle", "Termina a etiqueta e imprime"},
	"^FO": {"formatacao", "Posição do campo (canto superior esquerdo)"},
	"^FT": {"formatacao", "Posição do campo (linha de base do texto)"},
	"^FD": {"texto", "Conteúdo do campo"},
	"^FV": {"texto", "Conteúdo variável do campo"},
	"^FS": {"controle", "Fim do campo"},
	"^FB": {"formatacao", "Bloco de texto: largura, linhas, espaçamento e alinhamento"},
	"^FH": {"formatacao", "O conteúdo do campo tem códigos hexadecimais (_XX)"},
	"^FR": {"formatacao", "Campo invertido (branco no preto)"},
	"^FW": {"formatacao", "Orientação padrão dos campos"},
	"^FX": {"controle", "Comentário"},
	"^CF": {"formatacao", "Fonte padrão"},
	"^CI": {"formatacao", "Codificação dos caracteres (28 = UTF-8)"},
	"^A@": {"formatacao", "Fonte carregada pelo nome"},
	"^GB": {"imagem", "Caixa ou linha"},
	"^GC": {"imagem", "Círculo"},
	"^GE": {"imagem", "Elipse"},
	"^GD": {"imagem", "Linha diagonal"},
	"^GF": {"imagem", "Imagem (gráfico)"},
	"^BY": {"codigo-barras", "Padrão dos códigos de barras: largura da barra, razão e altura"},
	"^BC": {"codigo-barras", "Código de barras Code 128"},
	"^BE": {"codigo-barras", "Código de barras EAN-13"},
	"^B8": {"codigo-barras", "Código de barras EAN-8"},
	"^BU": {"codigo-barras", "Código de barras UPC-A"},
	"^B3": {"codigo-barras", "Código de barras Code 39"},
	"^B2": {"codigo-barras", "Código de barras 2 de 5 intercalado"},
	"^BQ": {"codigo-barras", "QR Code"},
	"^LH": {"formatacao", "Origem da etiqueta"},
	"^PW": {"formatacao", "Largura da etiqueta em pontos"},
	"^LL": {"formatacao", "Comprimento da etiqueta em pontos"},
	"^LS": {"formatacao", "Deslocamento horizontal da etiqueta"},
	"^LT": {"formatacao", "Deslocamento vertical da etiqueta"},
	"^LR": {"formatacao", "Inverte a etiqueta inteira"},
	"^PO": {"formatacao", "Orientação da impressão (I = de ponta-cabeça)"},
	"^PQ": {"controle", "Quantidade de cópias"},
	"^PR": {"controle", "Velocidade de impressão"},
	"^MD": {"controle", "Ajuste de escurecimento"},
	"~SD": {"controle", "Escurecimento"},
	"^MM": {"controle", "Modo de impressão (destacar, cortar...)"},
	"^MN": {"controle", "Tipo de mídia (com espaço, contínua...)"},
	"^MT": {"controle", "Térmica direta ou com ribbon"},
	"^JM": {"controle", "Densidade de pontos"},
	"^JU": {"controle", "Salva ou recarrega a configuração"},
	"^PM": {"controle", "Imagem espelhada"},
	"^CC": {"controle", "Troca o caractere de comando"},
	"^SN": {"texto", "Número serial (o emulador mostra o valor inicial)"},
	"^FN": {"texto", "Campo variável de modelo (não suportado)"},
	"^DF": {"controle", "Grava modelo de etiqueta (não suportado)"},
	"^XF": {"controle", "Usa modelo de etiqueta (não suportado)"},
	"~HS": {"status", "Pede o status da impressora"},
	"~HQ": {"status", "Pede o status de erros e avisos (~HQES)"},
	"~HI": {"status", "Pede a identificação da impressora"},
	"~WC": {"status", "Imprime a etiqueta de configuração"},
	"~JA": {"controle", "Cancela tudo o que está na fila"},
	"~PS": {"controle", "Retoma a impressão (sai da pausa)"},
	"~PP": {"controle", "Pausa a impressão"},
	"~DG": {"imagem", "Grava imagem na memória"},
	"^XG": {"imagem", "Imprime imagem da memória"},
	"^IM": {"imagem", "Imprime imagem da memória"},
}
