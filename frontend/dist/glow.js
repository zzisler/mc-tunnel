// glow.js — мягкое свечение, которое плавно едет за курсором.
// Отдельный файл: app.js не трогаем.
(() => {
  const glow = document.getElementById('glow');
  if (!glow) return;

  const SIZE = 560;       // должен совпадать с width/height у .glow в CSS
  const EASE = 0.08;      // 0..1: меньше — плавнее и «ленивее»
  const IDLE_FPS = 30;    // потолок fps, когда окно не в фокусе

  const reduceMotion = matchMedia('(prefers-reduced-motion: reduce)').matches;

  // x,y — где свечение сейчас; tx,ty — куда оно должно приехать (курсор)
  let x = innerWidth * 0.7, y = innerHeight * 0.9;
  let tx = x, ty = y;
  let raf = 0;            // id запланированного кадра, 0 — цикл не крутится
  let last = 0;           // время прошлого кадра
  let focused = document.hasFocus();

  const draw = () => {
    glow.style.transform = `translate3d(${x - SIZE / 2}px, ${y - SIZE / 2}px, 0)`;
  };

  function tick(now) {
    raf = 0;
    if (document.hidden) return;                        // окно свёрнуто — не рисуем

    const dt = Math.min(now - last, 100);
    if (!focused && dt < 1000 / IDLE_FPS - 1) {         // не в фокусе — режем до 30 fps
      raf = requestAnimationFrame(tick);
      return;
    }
    last = now;

    // скорость не зависит от fps: за один и тот же отрезок времени пятно проедет столько же
    const k = 1 - Math.pow(1 - EASE, dt / 16.67);
    x += (tx - x) * k;
    y += (ty - y) * k;
    draw();

    // доехали — цикл останавливаем; проснётся от следующего mousemove
    if (Math.abs(tx - x) < 0.5 && Math.abs(ty - y) < 0.5) return;
    raf = requestAnimationFrame(tick);
  }

  const wake = () => {
    if (!raf && !document.hidden) raf = requestAnimationFrame(tick);
  };

  draw();
  if (reduceMotion) return;                             // «уменьшить движение» — пятно стоит на месте

  addEventListener('mousemove', (e) => { tx = e.clientX; ty = e.clientY; wake(); }, { passive: true });
  addEventListener('focus', () => { focused = true; wake(); });
  addEventListener('blur', () => { focused = false; });
  document.addEventListener('visibilitychange', () => {
    if (document.hidden) { cancelAnimationFrame(raf); raf = 0; }
    else wake();
  });
})();
