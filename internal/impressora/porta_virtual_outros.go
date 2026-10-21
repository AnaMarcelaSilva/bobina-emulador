//go:build !linux

package impressora

import "errors"

func criarPortaVirtual(id string) (portaVirtual, error) {
	return portaVirtual{}, errors.New("a porta serial virtual só é criada no Linux; no Windows, crie um par de portas com o com0com (ex.: COM10 ↔ COM11), informe uma delas aqui e configure o sistema na outra")
}
