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

## Primeira versão

A aplicação inicial inclui cadastro, login, logout, catálogo compartilhado e um dashboard protegido. O backend Go disponibiliza APIs JSON para autenticação e livros; as telas web consomem essas APIs, sem colocar as regras do domínio no cliente.

Contas e sessões são armazenadas em SQLite no arquivo `data/biblioteca.db` por padrão. As senhas são armazenadas com hash bcrypt e os tokens de sessão são aleatórios, enviados em cookie `HttpOnly` e persistidos no banco apenas como hash. A criação de contas está aberta nesta primeira versão; não há recuperação de senha.

Os metadados dos livros ficam no SQLite; cada livro tem exatamente um arquivo no diretório `data/books`. O catálogo é visível para todas as contas autenticadas, mas só quem enviou um livro pode editar seus metadados ou substituir seu arquivo. Uploads aceitam arquivos de até 100 MiB sem restringir o formato; a leitura no navegador oferece EPUB (epub.js) e PDF (visualizador integrado do navegador). Outros formatos permanecem armazenados, mas ainda não têm leitor na aplicação.

### Executar localmente

Requer Go 1.26 ou superior. Na raiz do projeto:

```sh
go run ./cmd/biblioteca
```

Abra `http://localhost:8080`. O diretório e o banco SQLite são criados automaticamente.

Configuração opcional:

| Variável | Padrão | Uso |
| --- | --- | --- |
| `BIBLIOTECA_ADDR` | `:8080` | Endereço HTTP do servidor |
| `BIBLIOTECA_DB_PATH` | `data/biblioteca.db` | Caminho do arquivo SQLite |
| `BIBLIOTECA_BOOKS_DIR` | `data/books` | Diretório privado para os arquivos dos livros |
| `BIBLIOTECA_COOKIE_SECURE` | `0` | Defina `1` quando TLS terminar em um proxy reverso; conexões HTTPS diretas já ativam o atributo |

Em produção, sirva a aplicação exclusivamente por HTTPS e habilite o cookie seguro quando houver terminação TLS fora do processo Go.

### Executar com Docker Compose

Requer Docker com o plugin Compose. Na raiz do projeto:

```sh
docker compose up --build
```

Abra `http://localhost:8080`. Para parar a aplicação, use `docker compose down`. O banco SQLite e os arquivos dos livros ficam no volume persistente `biblioteca-data`; para escolher outra porta no computador, defina `BIBLIOTECA_PORT` antes de iniciar. Remover o volume com `docker compose down --volumes` também apaga esses dados.

### API de autenticação

- `POST /api/v1/auth/register` — recebe `{ "email": "...", "password": "..." }`, cria a conta e inicia uma sessão.
- `POST /api/v1/auth/login` — valida as credenciais e inicia uma sessão.
- `POST /api/v1/auth/logout` — encerra a sessão atual.
- `GET /api/v1/auth/me` — retorna a conta autenticada.
- `GET /api/v1/books` — lista todos os livros e informa `can_edit` para o usuário atual.
- `POST /api/v1/books` — envia metadados e um arquivo com `multipart/form-data`.
- `GET /api/v1/books/{id}` — consulta os metadados de um livro.
- `PUT /api/v1/books/{id}` — atualiza metadados e, opcionalmente, substitui o arquivo; apenas o proprietário pode editar.
- `GET /api/v1/books/{id}/file` — transmite o arquivo autenticado para o leitor do navegador.

Os metadados enviados são `title`, `author` e `description`; o arquivo usa o campo `file`. Respostas e erros da API usam JSON. O cookie de sessão é `HttpOnly` e `SameSite=Strict`; senha deve ter ao menos 8 caracteres e no máximo 72 bytes.

### Leitor EPUB e dependências web

O bundle local do epub.js é mantido em `internal/httpapi/web/vendor`. Para atualizar/recriar esse asset:

```sh
cd web-assets
npm ci
npm run vendor:epub
```

O leitor PDF usa o visualizador nativo do navegador dentro da página, sem enviar o arquivo a terceiros.

### Desenvolvimento e testes

```sh
go test ./...
go build ./cmd/biblioteca
```

## Contexto para agentes de IA

Antes de implementar funcionalidades, leia [`AGENTS.md`](AGENTS.md) e as instruções específicas do GitHub Copilot em [`.github/copilot-instructions.md`](.github/copilot-instructions.md). Registre decisões relevantes e não trate opções ainda em aberto como requisitos fechados.
