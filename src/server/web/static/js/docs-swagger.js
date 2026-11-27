"use strict";

(() => {
  const mount = document.getElementById("swagger-ui");
  if (!mount || typeof SwaggerUIBundle !== "function") return;

  const theme = mount.dataset.theme === "light" ? "theme-light" : "theme-dark";
  SwaggerUIBundle({
    url: mount.dataset.endpoint,
    dom_id: "#swagger-ui",
    deepLinking: true,
    displayRequestDuration: true,
    presets: [SwaggerUIBundle.presets.apis, SwaggerUIStandalonePreset],
    layout: "StandaloneLayout",
  });
  mount.classList.add(theme);
})();
