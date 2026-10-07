package impressora

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// vigiaPasta imprime os arquivos (.prn, .txt, .bin...) que aparecem numa
// pasta, como uma fila de impressão. Cada arquivo impresso vai para a
// subpasta "impressos". Desligada, os arquivos ficam esperando na pasta.
//
// Só arquivos com extensão de impressão são tocados: se a pasta for
// configurada errado (a pasta de documentos, por exemplo), nada mais é movido.
type vigiaPasta struct {
	parada chan struct{}
}

const subpastaImpressos = "impressos"

// extensoesImpressao são os arquivos que um sistema manda para uma
// impressora "em arquivo".
var extensoesImpressao = map[string]bool{
	".prn": true, ".txt": true, ".bin": true, ".raw": true, ".zpl": true, ".esc": true, ".dat": true,
}

func iniciarPasta(imp *Impressora, cfg Config) (conexao, string, error) {
	pasta := cfg.Pasta
	if strings.HasPrefix(pasta, "~") {
		if casa, err := os.UserHomeDir(); err == nil {
			pasta = filepath.Join(casa, strings.TrimPrefix(pasta, "~"))
		}
	}
	if err := os.MkdirAll(filepath.Join(pasta, subpastaImpressos), 0o755); err != nil {
		return nil, "", fmt.Errorf("não foi possível usar a pasta %s: %v", pasta, err)
	}
	v := &vigiaPasta{parada: make(chan struct{})}
	go v.vigiar(imp, pasta)
	return v, pasta, nil
}

func (v *vigiaPasta) vigiar(imp *Impressora, pasta string) {
	// Um arquivo só é lido quando o tamanho para de mudar, para não pegar
	// pela metade um arquivo que ainda está sendo gravado.
	tamanhos := map[string]int64{}
	t := time.NewTicker(400 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-v.parada:
			return
		case <-t.C:
		}
		if !imp.Config().Estado.Ligada {
			continue
		}
		entradas, err := os.ReadDir(pasta)
		if err != nil {
			continue
		}
		vistos := map[string]bool{}
		for _, e := range entradas {
			nome := e.Name()
			if e.IsDir() || strings.HasPrefix(nome, ".") || !extensoesImpressao[strings.ToLower(filepath.Ext(nome))] {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			vistos[nome] = true
			anterior, conhecido := tamanhos[nome]
			tamanhos[nome] = info.Size()
			if !conhecido || anterior != info.Size() {
				continue
			}
			caminho := filepath.Join(pasta, nome)
			dados, err := os.ReadFile(caminho)
			if err != nil {
				continue
			}
			destino := filepath.Join(pasta, subpastaImpressos, time.Now().Format("20060102-150405.000-")+nome)
			if err := os.Rename(caminho, destino); err != nil {
				imp.evento("erro", "Não foi possível mover "+nome+": "+err.Error(), "", 0)
				continue
			}
			delete(tamanhos, nome)
			imp.evento("conexao", "Arquivo encontrado na pasta: "+nome, destino, 0)
			s := novaSessao(imp, "arquivo "+nome, nil)
			s.Receber(dados)
			s.Encerrar()
		}
		for nome := range tamanhos {
			if !vistos[nome] {
				delete(tamanhos, nome)
			}
		}
	}
}

func (v *vigiaPasta) Parar() { close(v.parada) }
