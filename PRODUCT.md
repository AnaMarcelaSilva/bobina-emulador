# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

(A tela é HTML/CSS/JS servida localmente e exibida numa janela nativa de desktop: GTK + WebKit no Linux, WebView2 no Windows. Não roda num navegador para o usuário final.)

## Users

Devs que testam a integração de sistemas com impressoras: serviço de impressão, PDV, app de atendimento e microterminal. Estão no meio de uma tarefa de desenvolvimento, com o sistema rodando ao lado, mandam imprimir e querem ver na hora o que saiu no papel, e às vezes forçar um erro (sem papel, desligada, sem resposta de status) para ver como o sistema reage.

## Product Purpose

Substituir impressoras físicas de cupom (ESC/POS) e etiqueta (ZPL) durante o desenvolvimento e os testes. Sucesso é o dev confiar no que vê: o cupom na tela é o que a impressora real imprimiria, e a impressora responde ao status como a real responderia.

## Positioning

Várias impressoras ao mesmo tempo, cada uma na sua conexão (rede, serial virtual, pasta PRN), desenhando tudo localmente, sem mandar cupons para serviços externos, e respondendo ao status exatamente como os drivers Bematech, Elgin e ESC/POS genérico conferem, inclusive simulando impressoras que não respondem.

## Operating Context

- Usado ao lado da IDE, durante um teste manual, com o sistema testado imprimindo de verdade pela rede ou pela serial.
- O mais frequente é olhar o cupom ou etiqueta que acabou de sair. Forçar estados, inspecionar bytes e acompanhar os pedidos de status são ações eventuais.
- O dev escolhe quais impressoras ficam visíveis lado a lado (normalmente duas a quatro: cupom de caixa, cozinha, etiqueta).
- Também roda sem janela, controlado pela API, em testes automatizados; aí a tela não é usada.

## Capabilities and Constraints

- Linguagens: ESC/POS (cupom, bobina de 80 ou 58 mm) e ZPL (etiqueta, em mm e dpi).
- Conexões: rede TCP (porta 9100 por padrão), serial (virtual no Linux, com0com no Windows), pasta PRN.
- Estados simuláveis: desligada, offline/pausada, sem papel, papel acabando, tampa/cabeça aberta, erro na guilhotina, gaveta aberta, sem ribbon; respostas a todos os pedidos, só ESC/POS, só Bematech ou nenhum; atraso nas respostas.
- Inspetor por trabalho: comandos explicados, hexdump, texto puro, avisos (UTF-8, linha longa, Code 128 `{C`, `^CI28`, campo fora da etiqueta), salvar `.prn`, reimprimir.
- Atividade ao vivo (conexões, pedidos de status e respostas) e API REST + SSE.
- O papel do cupom e da etiqueta é sempre branco, como na vida real, em qualquer tema.
- Sem dependências externas na tela: nada de CDN ou fonte hospedada (a janela pode estar offline).

## Brand Commitments

Nome: Bobina. Linguagem da interface em português do Brasil, direta e técnica, para devs.

Direção visual escolhida em 04/10/2026: Painel de senha (cada impressora é um guichê; cada impressão é uma senha chamada). Controles continuam sendo os padrões de uma ferramenta de dev.

## Evidence on Hand

Amostras reais de uso: cupom de exemplo (padaria, com QR e Code 128), cupom com erros comuns, etiqueta de comanda no layout do serviço de impressão (60 x 40 mm, 203 dpi), etiqueta com erros. Não há clientes, depoimentos nem números de uso para exibir.

## Product Principles

1. O papel é o protagonista: o que saiu da impressora vem antes de qualquer controle.
2. Fidelidade antes de enfeite: o que a tela mostra tem que ser o que a impressora real faria.
3. Controles e diagnóstico aparecem quando pedidos, não o tempo todo.
4. Várias impressoras convivem na mesma tela sem se atrapalhar.
5. Nada sai da máquina.
