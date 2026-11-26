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

A aplicação inicial inclui cadastro, login, logout e um dashboard protegido. O backend Go disponibiliza uma API JSON em `/api/v1/auth`; as telas web consomem essa API, sem colocar as regras de autenticação no cliente.

Contas e sessões são armazenadas em SQLite no arquivo `data/biblioteca.db` por padrão. As senhas são armazenadas com hash bcrypt e os tokens de sessão são aleatórios, enviados em cookie `HttpOnly` e persistidos no banco apenas como hash. A criação de contas está aberta nesta primeira versão; não há recuperação de senha.

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
| `BIBLIOTECA_COOKIE_SECURE` | `0` | Defina `1` quando TLS terminar em um proxy reverso; conexões HTTPS diretas já ativam o atributo |

Em produção, sirva a aplicação exclusivamente por HTTPS e habilite o cookie seguro quando houver terminação TLS fora do processo Go.

### API de autenticação

- `POST /api/v1/auth/register` — recebe `{ "email": "...", "password": "..." }`, cria a conta e inicia uma sessão.
- `POST /api/v1/auth/login` — valida as credenciais e inicia uma sessão.
- `POST /api/v1/auth/logout` — encerra a sessão atual.
- `GET /api/v1/auth/me` — retorna a conta autenticada.

As respostas e erros usam JSON. O cookie de sessão é `HttpOnly` e `SameSite=Strict`; senha deve ter ao menos 8 caracteres e no máximo 72 bytes.

### Desenvolvimento e testes

```sh
go test ./...
go build ./cmd/biblioteca
```

## Contexto para agentes de IA

Antes de implementar funcionalidades, leia [`AGENTS.md`](AGENTS.md) e as instruções específicas do GitHub Copilot em [`.github/copilot-instructions.md`](.github/copilot-instructions.md). Registre decisões relevantes e não trate opções ainda em aberto como requisitos fechados.
