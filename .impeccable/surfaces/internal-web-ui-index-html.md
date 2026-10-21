---
version: 1
slug: "internal-web-ui-index-html"
primary_target: "internal/web/ui/index.html"
related_targets: []
---

# Tela principal do Bobina

Modo: Operate. Público: devs testando integração com impressoras. Tarefa principal: ver o cupom ou etiqueta que acabou de sair, em várias impressoras lado a lado; forçar estados e investigar bytes são eventuais e ficam atrás de um clique. O usuário escolhe quais impressoras ficam visíveis.

## Direction contract

THESIS: Cada impressora é um guichê; cada impressão é uma senha chamada. Recusa o painel de controle cheio de chaves e abas sempre abertas que toda ferramenta de emulação mostra.

OWN-WORLD: Chassi grafite fosco (barra superior e cabeçalho de cada guichê) sobre um chão de aço claro; dígitos de sete segmentos desenhados em SVG, vermelhos com os segmentos apagados visíveis, como num painel de senha; papel térmico sempre branco. Texto em fonte de sistema, cor só para estado. Desligado: guichê inteiro em cinza (raise do catálogo de mascotes). A coluna tem a largura exata da bobina (raise do encarte cassete).

STORY: O dev abre, vê os guichês que escolheu, manda imprimir do sistema e vê o número do guichê trocar e o papel descer. Se precisar, abre o estado ou os detalhes daquele guichê.

FIRST VIEWPORT: Barra grafite fina no topo com o nome Bobina e os guichês como botões liga/mostra. Abaixo, colunas fixas lado a lado; cada uma com cabeçalho grafite: nome e conexão à esquerda, número do último trabalho em LED à direita (o único título, raise do catálogo Factory), e uma linha de ações: ligar, estado, imprimir teste, mais. Faixa de alerta só quando há problema. O papel ocupa o resto.

FORM: Painel de senha, posição 6 da lista própria, seed 527e4e2b. Sinal característico: ao chegar um trabalho, os dígitos trocam e o cabeçalho pisca uma vez; as colunas nunca se mexem (raise do painel split-flap).

FINISH: unreviewed and undocumented is unfinished; this build ends with the finish review, the verdict, DESIGN.md, and every shipping raster carrying its provenance
