package web

import (
	"net"
	"net/http"
	"net/url"
	"strings"
)

// Protegido barra pedidos que não vêm da própria tela do Bobina nem de uma
// ferramenta local (curl, testes). Sem isso, qualquer site aberto no
// navegador poderia mandar um POST para 127.0.0.1 e, por exemplo, criar uma
// impressora "pasta" apontando para uma pasta do usuário.
//
//   - Origin: navegadores mandam esse cabeçalho em pedidos de outro site;
//     só passa quando é o próprio endereço da tela. curl e testes não mandam.
//   - Host: com o servidor só em 127.0.0.1, só aceita nomes locais. Isso
//     impede o "DNS rebinding" (um domínio externo que passa a apontar para
//     127.0.0.1 para ler e mudar a API). Quem escolhe -host 0.0.0.0 abre a
//     API para a rede de propósito, e aí essa conferência não se aplica.
func Protegido(h http.Handler, soLocal bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if soLocal && !HostLocal(r.Host) {
			http.Error(w, "endereço não permitido", http.StatusForbidden)
			return
		}
		if origem := r.Header.Get("Origin"); origem != "" {
			u, err := url.Parse(origem)
			if err != nil || u.Host != r.Host {
				http.Error(w, "pedido de outro site não permitido", http.StatusForbidden)
				return
			}
		}
		h.ServeHTTP(w, r)
	})
}

// HostLocal diz se o endereço só é alcançável deste computador.
func HostLocal(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.Trim(host, "[]")
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// limiteConfig é o tamanho máximo de um JSON de configuração ou estado.
const limiteConfig = 1 << 20
