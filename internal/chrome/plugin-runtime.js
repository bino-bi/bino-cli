// Injected into headless PDF and screenshot renders only. Plugin elements call
// window.binoPlugin?.track(this, promise) for each draw; the host awaits
// settled() before it prints.
(() => {
  if (window.binoPlugin) return;
  const pending = new Set();
  const failures = [];
  let tracked = 0;
  const fail = (tag, id, message) => failures.push({ tag, id, message });

  // A throw before track() is called, e.g. in a constructor or in
  // connectedCallback, only shows up here.
  window.addEventListener("error", (ev) => {
    let path = "";
    try {
      path = new URL(ev.filename, location.href).pathname;
    } catch (e) {
      return;
    }
    if (path.startsWith("/plugins/")) fail("", "", String(ev.message) + " (" + path + ")");
  });

  const frame = () => new Promise((resolve) => requestAnimationFrame(resolve));
  const sleep = (ms) => new Promise((resolve) => setTimeout(resolve, ms));

  window.binoPlugin = {
    track(element, promise) {
      const p = Promise.resolve(promise);
      tracked++;
      pending.add(p);
      p.catch((reason) => {
        fail(element ? element.localName : "", (element && element.id) || "", String((reason && reason.message) || reason));
      }).finally(() => pending.delete(p));
      return promise;
    },
    async settled() {
      for (;;) {
        while (pending.size > 0) await Promise.allSettled([...pending]);
        // A quiet period catches a redraw that a late resize starts.
        const before = tracked;
        await frame();
        await frame();
        await sleep(100);
        if (tracked === before && pending.size === 0) break;
      }
      const undefinedTags = new Set();
      for (const el of document.querySelectorAll("bn-context :not(:defined)")) {
        const name = el.localName;
        if (!name.startsWith("bn-") && !name.startsWith("bino-")) undefinedTags.add(name);
      }
      return { failures: failures.slice(), undefined: [...undefinedTags] };
    },
  };
})();
