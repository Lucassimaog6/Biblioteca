# Biblioteca

Biblioteca é um projeto de biblioteca digital pessoal. A proposta é oferecer uma API para gerenciar livros e um cliente web para navegar e ler os livros. No futuro, clientes mobile e um aplicativo nativo para Kindle também deverão consumir a mesma API.

## Escopo planejado

- Backend em Go, com foco em desempenho.
- Livros armazenados no sistema de arquivos, com um arquivo por livro.
- Metadados enriquecidos por fontes de terceiros, com possibilidade de edição pelos usuários.
- Upload de livros pelos usuários.
- Salvamento automático do progresso de leitura.
- Cliente web que consulta a API e permite ler os livros.
- API preparada para atender clientes futuros, incluindo mobile e Kindle.

A possibilidade de usar SSG ainda está em aberto; não é uma decisão arquitetural definida.

## Estado atual

O repositório está no início: contém apenas a documentação de contexto, sem implementação, comandos de execução ou testes configurados.

## Contexto para agentes de IA

Antes de implementar funcionalidades, leia [`AGENTS.md`](AGENTS.md) e as instruções específicas do GitHub Copilot em [`.github/copilot-instructions.md`](.github/copilot-instructions.md). Registre decisões relevantes e não trate opções ainda em aberto como requisitos fechados.
