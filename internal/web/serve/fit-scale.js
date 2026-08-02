/**
 * Fit-to-width page scaling for the serve viewer.
 *
 * Report pages (bn-layout-page) are fixed-size boxes (e.g. 1024px XGA). The
 * scaler measures each page and writes --bino-fit-scale/-w/-h as inline
 * custom properties; serve.css applies the transform + margin compensation.
 * Scale is capped at 1 (no upscaling — pinch-zoom covers that).
 *
 * offsetWidth/offsetHeight report the layout size, which a transform does
 * not change, so re-applying is idempotent.
 *
 * Invariant: content swaps replace the bn-context without firing the
 * ResizeObservers — every swap site in serve-app.js must call rebind().
 */
export function createFitScaler(outlet) {
  var pending = false;

  // setTimeout instead of requestAnimationFrame: rAF is throttled to a halt
  // in occluded/background tabs, which would leave pages unscaled after a
  // rotation or window resize until the tab repaints.
  function schedule() {
    if (!pending) {
      pending = true;
      setTimeout(function() {
        pending = false;
        apply();
      }, 0);
    }
  }

  function apply() {
    var ctx = document.querySelector('bn-context');
    if (!ctx) return;
    var cs = getComputedStyle(ctx);
    var avail = ctx.clientWidth - parseFloat(cs.paddingLeft) - parseFloat(cs.paddingRight);
    if (avail <= 0) return;
    ctx.querySelectorAll(':scope > bn-layout-page').forEach(function(page) {
      var pw = page.offsetWidth;
      var ph = page.offsetHeight;
      if (pw <= 0) return;
      var scale = Math.min(1, avail / pw);
      page.style.setProperty('--bino-fit-scale', String(scale));
      page.style.setProperty('--bino-fit-w', pw + 'px');
      page.style.setProperty('--bino-fit-h', ph + 'px');
    });
  }

  // Outlet resize covers window resize, rotation, and sidebar pin/collapse.
  var outletObserver = new ResizeObserver(schedule);
  outletObserver.observe(outlet);

  // Pages are sized late by engine hydration (0 -> 1024px), so observe them too.
  var pageObserver = new ResizeObserver(schedule);

  function rebind() {
    pageObserver.disconnect();
    document.querySelectorAll('bn-context > bn-layout-page').forEach(function(page) {
      pageObserver.observe(page);
    });
    schedule();
  }

  return { rebind: rebind, schedule: schedule };
}
