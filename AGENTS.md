# Contexto e orientações para agentes

## Visão do projeto

Biblioteca será uma biblioteca digital pessoal composta inicialmente por:

- Um backend em Go para gerenciar livros e expor uma API.
- Um cliente web que usa essa API para exibir a coleção e permitir a leitura.

Os livros serão armazenados como arquivos no sistema de arquivos, com exatamente um arquivo por livro. O sistema deverá permitir uploads e edição de metadados pelos usuários. Metadados poderão ser obtidos de serviços de terceiros. Durante a leitura, o progresso deverá ser salvo automaticamente.

No futuro, clientes mobile e um aplicativo nativo para Kindle também deverão conversar com a API. Por isso, funcionalidades do domínio e operações de leitura não devem depender exclusivamente de comportamentos específicos do cliente web.

## Diretrizes de implementação

- Use Go para o backend, priorizando desempenho sem sacrificar clareza, segurança ou facilidade de manutenção.
- Trate a API como uma interface compartilhada entre clientes web, mobile e Kindle.
- Preserve a relação de um arquivo por livro; não introduza armazenamento dividido em múltiplos arquivos sem uma decisão explícita.
- Considere serviços de metadados externos como dependências sujeitas a falhas; não faça deles a única fonte dos dados editáveis do usuário.
- Ao lidar com uploads ou caminhos de arquivos, valide entradas e impeça acesso fora do diretório de armazenamento autorizado.
- Salve o progresso de leitura automaticamente e associe-o ao usuário e ao livro conforme o modelo de identidade definido no projeto.
- SSG foi apenas considerado, não escolhido. Antes de introduzir essa arquitetura, esclareça onde ela se aplicaria e avalie seus efeitos sobre conteúdo dinâmico, leitura e clientes futuros.
- Não invente decisões ausentes (como banco de dados, autenticação, formatos aceitos, fornecedor de metadados ou estratégia de implantação). Quando necessárias, proponha opções e registre a decisão junto ao código ou à documentação.
- Mantenha documentação e testes alinhados às mudanças. Use apenas ferramentas e comandos que já estejam configurados no repositório.
- Nunca inclua credenciais ou segredos em arquivos, exemplos, logs ou commits.

## Estado do repositório

No momento, o repositório não contém implementação nem configuração de build ou testes. Inspecione o estado atual antes de escolher ferramentas ou afirmar que algo pode ser executado.
