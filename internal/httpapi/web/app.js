document.addEventListener("DOMContentLoaded", () => {
  const form = document.querySelector("[data-auth-form]");
  if (form) {
    form.addEventListener("submit", async (event) => {
      event.preventDefault();
      const error = form.querySelector(".form-error");
      const button = form.querySelector("button[type=submit]");
      error.textContent = "";
      button.disabled = true;

      try {
        const values = new FormData(form);
        const response = await fetch(`/api/v1/auth/${form.dataset.authForm}`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({
            email: values.get("email"),
            password: values.get("password"),
          }),
        });
        const result = await response.json();
        if (!response.ok) {
          error.textContent = result.error || "Não foi possível concluir a solicitação.";
          return;
        }
        window.location.assign("/dashboard");
      } catch {
        error.textContent = "Não foi possível conectar. Tente novamente.";
      } finally {
        button.disabled = false;
      }
    });
  }

  const logout = document.querySelector("[data-logout]");
  if (logout) {
    logout.addEventListener("click", async () => {
      try {
        await fetch("/api/v1/auth/logout", { method: "POST" });
      } finally {
        window.location.assign("/login");
      }
    });
  }

  const bookList = document.querySelector("[data-book-list]");
  if (bookList) initializeDashboard();

  const reader = document.querySelector("[data-reader]");
  if (reader) initializeReader(reader);
});

async function initializeDashboard() {
  const email = document.querySelector("[data-user-email]");
  const list = document.querySelector("[data-book-list]");
  const message = document.querySelector("[data-catalog-message]");
  const dialog = document.querySelector("[data-book-dialog]");
  const form = document.querySelector("[data-book-form]");
  const fileInput = document.querySelector("[data-book-file]");
  let currentBook = null;

  try {
    const response = await fetch("/api/v1/auth/me");
    if (response.status === 401) {
      window.location.replace("/login");
      return;
    }
    if (!response.ok) throw new Error("Não foi possível validar sua sessão.");
    const result = await response.json();
    email.textContent = result.user.email;
  } catch {
    window.location.replace("/login");
    return;
  }

  async function loadBooks() {
    try {
      list.replaceChildren();
      const response = await fetch("/api/v1/books");
      if (!response.ok) throw new Error("Não foi possível carregar os livros.");
      const result = await response.json();
      message.textContent = "";
      if (result.books.length === 0) {
        const empty = document.createElement("section");
        empty.className = "empty-state";
        const icon = document.createElement("div");
        icon.className = "book-mark";
        icon.setAttribute("aria-hidden", "true");
        icon.textContent = "▤";
        const heading = document.createElement("h2");
        heading.textContent = "Sua estante está esperando";
        const copy = document.createElement("p");
        copy.textContent = "Envie seu primeiro livro para começar sua biblioteca.";
        empty.append(icon, heading, copy);
        list.append(empty);
        return true;
      }
      for (const book of result.books) list.append(renderBook(book));
      return true;
    } catch {
      message.textContent = "Não foi possível carregar os livros. Tente novamente.";
      return false;
    }
  }

  function renderBook(book) {
    const card = document.createElement("article");
    card.className = "book-card";
    const information = document.createElement("div");
    information.className = "book-information";
    const title = document.createElement("h3");
    title.textContent = book.title;
    information.append(title);
    if (book.author) {
      const author = document.createElement("p");
      author.className = "book-author";
      author.textContent = book.author;
      information.append(author);
    }
    if (book.description) {
      const description = document.createElement("p");
      description.className = "book-description";
      description.textContent = book.description;
      information.append(description);
    }
    const filename = document.createElement("p");
    filename.className = "book-filename";
    filename.textContent = `${book.filename} · ${formatFileSize(book.file_size)}`;
    information.append(filename);

    const actions = document.createElement("div");
    actions.className = "book-actions";
    const readButton = document.createElement("a");
    readButton.className = "primary-button";
    readButton.href = `/read/${book.id}`;
    readButton.textContent = "Ler livro";
    actions.append(readButton);
    const editButton = document.createElement("button");
    editButton.className = "button-quiet";
    editButton.type = "button";
    editButton.textContent = "Editar";
    editButton.disabled = !book.can_edit;
    if (!book.can_edit) editButton.title = "Somente o proprietário pode editar este livro.";
    editButton.addEventListener("click", () => openBookDialog(book));
    actions.append(editButton);
    card.append(information, actions);
    return card;
  }

  function openBookDialog(book = null) {
    currentBook = book;
    form.reset();
    form.querySelector("#book-title").value = book?.title || "";
    form.querySelector("#book-author").value = book?.author || "";
    form.querySelector("#book-description").value = book?.description || "";
    fileInput.required = !book;
    document.querySelector("[data-dialog-title]").textContent = book ? "Editar livro" : "Enviar livro";
    document.querySelector("[data-file-label]").textContent = book ? "Substituir arquivo (opcional)" : "Arquivo do livro";
    document.querySelector("[data-file-help]").textContent = book
      ? "Selecione um novo arquivo apenas se quiser substituir o atual. Limite de 100 MiB."
      : "Envie um arquivo de até 100 MiB. EPUB e PDF podem ser lidos no navegador.";
    document.querySelector("[data-book-error]").textContent = "";
    dialog.showModal();
  }

  document.querySelector("[data-new-book]").addEventListener("click", () => openBookDialog());
  document.querySelectorAll("[data-close-dialog]").forEach((button) => {
    button.addEventListener("click", () => dialog.close());
  });
  form.addEventListener("submit", async (event) => {
    event.preventDefault();
    const error = document.querySelector("[data-book-error]");
    const submit = form.querySelector("button[type=submit]");
    error.textContent = "";
    submit.disabled = true;
    try {
      const payload = new FormData(form);
      if (fileInput.files.length === 0) payload.delete("file");
      const url = currentBook ? `/api/v1/books/${currentBook.id}` : "/api/v1/books";
      const response = await fetch(url, {
        method: currentBook ? "PUT" : "POST",
        body: payload,
      });
      const result = await response.json();
      if (!response.ok) {
        error.textContent = result.error || "Não foi possível salvar o livro.";
        return;
      }
      const wasEdit = Boolean(currentBook);
      dialog.close();
      if (await loadBooks()) message.textContent = wasEdit ? "Livro atualizado." : "Livro enviado.";
    } catch {
      error.textContent = "Não foi possível conectar. Tente novamente.";
    } finally {
      submit.disabled = false;
    }
  });

  await loadBooks();
}

function formatFileSize(size) {
  if (size < 1024) return `${size} B`;
  if (size < 1024 * 1024) return `${(size / 1024).toFixed(1)} KiB`;
  return `${(size / (1024 * 1024)).toFixed(1)} MiB`;
}

async function initializeReader(reader) {
  const status = document.querySelector("[data-reader-status]");
  const id = window.location.pathname.match(/^\/read\/([1-9]\d*)$/)?.[1];
  if (!id) {
    status.textContent = "Livro não encontrado.";
    return;
  }
  try {
    const response = await fetch(`/api/v1/books/${id}`);
    if (response.status === 401) {
      window.location.replace("/login");
      return;
    }
    if (!response.ok) {
      status.textContent = "Não foi possível abrir este livro.";
      return;
    }
    const { book } = await response.json();
    document.querySelector("[data-reader-title]").textContent = book.title;
    const fileURL = `/api/v1/books/${id}/file/${encodeURIComponent(book.filename)}`;
    const extension = book.filename.split(".").pop().toLowerCase();
    if (extension === "epub") {
      if (typeof window.ePub !== "function") {
        status.textContent = "O leitor EPUB não está disponível.";
        return;
      }
      const controls = document.querySelector("[data-epub-controls]");
      const viewport = document.querySelector("[data-epub-viewport]");
      controls.hidden = false;
      viewport.hidden = false;
      status.textContent = "Carregando livro…";
      let publication = window.ePub(fileURL);
      if (publication && typeof publication.then === "function") publication = await publication;
      if (publication?.ready && typeof publication.ready.then === "function") {
        await Promise.race([
          publication.ready,
          new Promise((_, reject) => publication.once("openFailed", reject)),
        ]);
      }
      const rendition = publication.renderTo(viewport, { width: "100%", height: "100%", flow: "paginated", method: "url" });
      await rendition.display();
      status.textContent = "";
      document.querySelector("[data-previous-page]").addEventListener("click", () => rendition.prev());
      document.querySelector("[data-next-page]").addEventListener("click", () => rendition.next());
      rendition.on("relocated", (location) => {
        const page = location.start?.displayed?.page;
        const total = location.start?.displayed?.total;
        status.textContent = page && total ? `Página ${page} de ${total}` : "";
      });
      window.addEventListener("beforeunload", () => publication.destroy(), { once: true });
    } else if (extension === "pdf") {
      const frame = document.querySelector("[data-pdf-viewer]");
      frame.hidden = false;
      frame.src = fileURL;
      status.textContent = "";
    } else {
      status.textContent = "A leitura no navegador está disponível para EPUB e PDF.";
    }
  } catch {
    status.textContent = "Não foi possível carregar o livro.";
  }
}
