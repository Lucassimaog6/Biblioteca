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

  const email = document.querySelector("[data-user-email]");
  if (email) {
    fetch("/api/v1/auth/me")
      .then((response) => {
        if (!response.ok) {
          window.location.replace("/login");
          return null;
        }
        return response.json();
      })
      .then((result) => {
        if (result) email.textContent = result.user.email;
      })
      .catch(() => window.location.replace("/login"));
  }

  const logout = document.querySelector("[data-logout]");
  if (logout) {
    logout.addEventListener("click", async () => {
      await fetch("/api/v1/auth/logout", { method: "POST" });
      window.location.assign("/login");
    });
  }
});
