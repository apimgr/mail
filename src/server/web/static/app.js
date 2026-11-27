"use strict";

const OFFLINE_DEFAULTS = {
  lang: "en",
  dir: "ltr",
  title: "Offline",
  heading: "You are offline",
  message: "This page could not be loaded and no cached copy is available. Check your connection and try again.",
  reload: "Reload"
};
function offlineString(name, fallback) {
  const element = document.querySelector(`meta[name="offline-${name}"]`);
  return element ? element.content : fallback;
}

const OFFLINE_STRINGS = Object.assign({}, OFFLINE_DEFAULTS, {
  lang: document.documentElement.lang || OFFLINE_DEFAULTS.lang,
  dir: document.documentElement.dir || OFFLINE_DEFAULTS.dir,
  title: offlineString("title", OFFLINE_DEFAULTS.title),
  heading: offlineString("heading", OFFLINE_DEFAULTS.heading),
  message: offlineString("message", OFFLINE_DEFAULTS.message),
  reload: offlineString("reload", OFFLINE_DEFAULTS.reload)
});
window.OFFLINE_STRINGS = OFFLINE_STRINGS;

if ("caches" in window) {
  caches.open("mail-offline-strings")
    .then((cache) => cache.put("/offline-strings.json", new Response(JSON.stringify(OFFLINE_STRINGS), {
      headers: { "Content-Type": "application/json; charset=utf-8" }
    })))
    .catch(() => {});
}

const bootTimer = window.setTimeout(recoverPage, 10000);
const finishFirstRender = () => {
  window.clearTimeout(bootTimer);
};
if (document.readyState !== "complete") {
  document.addEventListener("DOMContentLoaded", finishFirstRender, { once: true });
} else {
  finishFirstRender();
}

async function recoverPage() {
  let recovered;
  try {
    recovered = window.sessionStorage.getItem("mail_recovered") === "1";
    if (!recovered) window.sessionStorage.setItem("mail_recovered", "1");
  } catch (_) {
    renderRecoveryError();
    return;
  }
  if (recovered) {
    renderRecoveryError();
    return;
  }

  const cleanup = [];
  if (navigator.serviceWorker) {
    cleanup.push(navigator.serviceWorker.getRegistrations()
      .then((registrations) => Promise.allSettled(registrations.map((registration) => registration.unregister())))
      .catch(() => {}));
  }
  if ("caches" in window) {
    cleanup.push(caches.keys()
      .then((names) => Promise.allSettled(names.map((name) => caches.delete(name))))
      .catch(() => {}));
  }
  await Promise.race([
    Promise.allSettled(cleanup),
    new Promise((resolve) => window.setTimeout(resolve, 3000))
  ]);
  window.location.reload();
}

function renderRecoveryError() {
  document.documentElement.lang = OFFLINE_STRINGS.lang;
  document.documentElement.dir = OFFLINE_STRINGS.dir;
  document.title = OFFLINE_STRINGS.title;

  const main = document.createElement("main");
  main.className = "container container-md";
  const heading = document.createElement("h1");
  heading.textContent = OFFLINE_STRINGS.heading;
  const message = document.createElement("p");
  message.textContent = OFFLINE_STRINGS.message;
  const reload = document.createElement("a");
  reload.href = window.location.href;
  reload.textContent = OFFLINE_STRINGS.reload;
  main.append(heading, message, reload);
  document.body.replaceChildren(main);
}

document.querySelectorAll("[data-copy]").forEach((button) => {
  button.addEventListener("click", async () => {
    const input = document.getElementById(button.dataset.copy);
    if (!input) return;
    input.select();
    try { await navigator.clipboard.writeText(input.value); } catch (_) { document.execCommand("copy"); }
  });
});

if ("serviceWorker" in navigator) {
  window.addEventListener("load", async () => {
    try {
      const registration = await navigator.serviceWorker.register("/sw.js", { scope: "/" });
      const notice = document.getElementById("update-notice");
      const apply = document.getElementById("apply-update");
      let acceptedUpdate = false;

      navigator.serviceWorker.addEventListener("controllerchange", () => {
        if (acceptedUpdate) window.location.reload();
      });

      const showWaitingWorker = () => {
        if (!registration.waiting || !navigator.serviceWorker.controller || !notice || !apply) return;
        notice.hidden = false;
        apply.addEventListener("click", () => {
          acceptedUpdate = true;
          registration.waiting.postMessage("SKIP_WAITING");
          window.setTimeout(() => {
            if (acceptedUpdate) window.location.reload();
          }, 5000);
        }, { once: true });
      };

      showWaitingWorker();
      registration.addEventListener("updatefound", () => {
        const worker = registration.installing;
        if (!worker) return;
        worker.addEventListener("statechange", showWaitingWorker);
      });
      window.setInterval(() => registration.update().catch(() => {}), 60 * 60 * 1000);
    } catch (_) {
      // The site remains usable when service worker registration is unavailable.
    }
  });
}
