/* MC Tunnel front end.
 *
 * Talks to the Go side through the Wails bindings (window.go.main.App) and
 * listens to the "log" event for streamed log lines. When the page is opened
 * in a plain browser (no Wails runtime) it falls back to a small simulation,
 * so the layout can be reviewed without building the app.
 */

(() => {
  'use strict';

  const $ = (id) => document.getElementById(id);
  const Wails = window.go?.main?.App ?? null;
  const Runtime = window.runtime ?? null;

  const els = {
    server: $('server'),
    proxy: $('proxy'),
    port: $('port'),
    start: $('startBtn'),
    stop: $('stopBtn'),
    test: $('testBtn'),
    title: $('trafficTitle'),
    dot: $('statusDot'),
    card: $('trafficCard'),
    sessions: $('statSessions'),
    uptime: $('statUptime'),
    traffic: $('statTraffic'),
    logLines: $('logLines'),
    logScroll: $('logScroll'),
    chartLine: $('chartLine'),
    chartArea: $('chartArea'),
  };

  const ui = { busy: null, running: false, since: 0, ticking: 0, history: [] };

  // ---------------------------------------------------------------- helpers

  const pad = (n) => String(n).padStart(2, '0');

  function clockText(d) {
    return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}.${String(d.getMilliseconds()).padStart(3, '0')}`;
  }

  function bytes(n) {
    n = Number(n) || 0;
    if (n < 1024) return `${n} B`;
    if (n < 1024 * 1024) return `${(n / 1024).toFixed(1)} KB`;
    if (n < 1024 ** 3) return `${(n / 1024 ** 2).toFixed(1)} MB`;
    return `${(n / 1024 ** 3).toFixed(2)} GB`;
  }

  function duration(sec) {
    sec = Math.max(0, Math.floor(sec));
    if (sec < 60) return `${sec} сек.`;
    if (sec < 3600) return `${Math.floor(sec / 60)} мин.`;
    return `${Math.floor(sec / 3600)} ч ${Math.floor((sec % 3600) / 60)} мин.`;
  }

  function proxyKind() {
    return document.querySelector('input[name="proxy-type"]:checked').value;
  }

  function setKind(kind) {
    const input = document.querySelector(`input[name="proxy-type"][value="${kind}"]`);
    if (input) input.checked = true;
  }

  // ------------------------------------------------------------------- log

  const nearBottom = () =>
    els.logScroll.scrollHeight - els.logScroll.scrollTop - els.logScroll.clientHeight < 40;

  function addLine({ time, level, msg }) {
    const stick = nearBottom();
    const line = document.createElement('div');
    const lvl = (level || 'INF').toUpperCase();
    line.className = 'log-line ' + (lvl === 'WRN' ? 'wrn' : lvl === 'ERR' ? 'err' : lvl === 'DBG' ? 'dbg' : '');

    // Backend lines usually start with a peer address; show it in its own column.
    const m = /^(\d{1,3}(?:\.\d{1,3}){3}:\d+|\[[0-9a-f:]+\]:\d+)\s+(.*)$/i.exec(msg || '');
    const addr = m ? m[1] : '';
    const rest = m ? m[2] : msg || '';

    const timeEl = document.createElement('time');
    timeEl.textContent = time || clockText(new Date());
    const lvlEl = document.createElement('b');
    lvlEl.textContent = lvl;
    const addrEl = document.createElement('span');
    addrEl.className = 'log-addr';
    addrEl.textContent = addr;
    const msgEl = document.createElement('span');
    msgEl.textContent = rest;

    line.append(timeEl, lvlEl, addrEl, msgEl);
    els.logLines.append(line);

    while (els.logLines.children.length > 600) els.logLines.firstElementChild.remove();
    if (stick) els.logScroll.scrollTop = els.logScroll.scrollHeight;
  }

  // ---------------------------------------------------------------- states

  function renderState(s) {
    ui.running = !!s.running;
    ui.since = s.startedAt || ui.since;

    const pending = ui.busy === 'start';
    els.card.classList.toggle('running', ui.running);
    els.title.className = 'traffic-title' + (ui.running ? '' : pending ? ' pending' : ' stopped');
    els.dot.className = 'status-dot' + (ui.running ? '' : pending ? ' pending' : ' stopped');
    els.title.firstChild.textContent = ui.running
      ? 'Туннель запущен '
      : pending
        ? 'Подключение… '
        : 'Туннель остановлен ';

    els.sessions.textContent = String(s.sessions ?? 0);
    els.uptime.textContent = ui.running && s.uptimeSec ? duration(s.uptimeSec) : '—';
    els.traffic.textContent = `${bytes(s.up)} / ${bytes(s.down)}`;

    const startDisabled = ui.running || ui.busy === 'start' || ui.busy === 'test';
    els.start.disabled = startDisabled;
    els.stop.disabled = !ui.running || ui.busy === 'start';
    els.test.disabled = ui.busy === 'test' || ui.busy === 'start';

    const total = (Number(s.up) || 0) + (Number(s.down) || 0);
    ui.history.push(total);
    if (ui.history.length > 40) ui.history.shift();
    drawChart();
  }

  function drawChart() {
    const h = ui.history.length ? ui.history : [0];
    const max = Math.max(...h, 1);
    const w = 160;
    const points = h.map((v, i) => {
      const x = (i / Math.max(h.length - 1, 1)) * w;
      const y = 56 - (v / max) * 44;
      return [x, y];
    });
    if (points.length === 1) points.push([w, points[0][1]]);

    const d = points
      .map(([x, y], i) => `${i === 0 ? 'M' : 'L'}${x.toFixed(1)} ${y.toFixed(1)}`)
      .join(' ');
    els.chartLine.setAttribute('d', d);
    els.chartArea.setAttribute('d', `${d} L${w} 60 L0 60 Z`);
  }

  function withBusy(kind, fn) {
    ui.busy = kind;
    const button = kind === 'test' ? els.test : els.start;
    const label = button.querySelector('span');
    const icon = button.querySelector('svg');
    const saved = label.textContent;
    icon.style.display = 'none';
    label.textContent = kind === 'test' ? 'Проверяю…' : 'Запускаю…';
    button.classList.add('busy');
    refresh();

    return Promise.resolve()
      .then(fn)
      .catch((err) => addLine({ level: 'ERR', msg: String(err?.message ?? err) }))
      .finally(() => {
        ui.busy = null;
        button.classList.remove('busy');
        icon.style.display = '';
        label.textContent = saved;
        refresh();
      });
  }

  // ------------------------------------------------------------------- api

  const api = Wails
    ? {
        load: () => Wails.Load(),
        start: (server, proxy, kind, port) => Wails.Start(server, proxy, kind, port),
        stop: () => Wails.Stop(),
        test: (server, proxy, kind) => Wails.Test(server, proxy, kind),
        state: () => Wails.State(),
        saveLog: (text) => Wails.SaveLog(text),
      }
    : mockApi();

  let pollTimer = 0;

  function refresh() {
    api.state().then(renderState).catch(() => {});
  }

  function startPolling() {
    if (pollTimer) return;
    pollTimer = setInterval(refresh, 500);
  }

  // --------------------------------------------------------------- actions

  els.start.addEventListener('click', () => {
    withBusy('start', async () => {
      const result = await api.start(
        els.server.value.trim(),
        els.proxy.value.trim(),
        proxyKind(),
        Number(els.port.value) || 25565,
      );
      if (result?.kind) setKind(result.kind);
      await refresh();
    });
  });

  els.stop.addEventListener('click', () => {
    withBusy('stop', async () => {
      await api.stop();
      await refresh();
    });
  });

  els.test.addEventListener('click', () => {
    withBusy('test', async () => {
      await api.test(els.server.value.trim(), els.proxy.value.trim(), proxyKind());
      await refresh();
    });
  });

  const copy = async (value, button) => {
    if (!value) return;
    try {
      if (Runtime?.ClipboardSetText) await Runtime.ClipboardSetText(value);
      else await navigator.clipboard.writeText(value);
      button.classList.add('copied');
      setTimeout(() => button.classList.remove('copied'), 900);
    } catch {
      addLine({ level: 'WRN', msg: 'clipboard unavailable' });
    }
  };

  $('copyServer').addEventListener('click', (e) => copy(els.server.value.trim(), e.currentTarget));
  $('copyProxy').addEventListener('click', (e) => copy(els.proxy.value.trim(), e.currentTarget));

  $('clearLog').addEventListener('click', () => els.logLines.replaceChildren());

  $('saveLog').addEventListener('click', async () => {
    const text = [...els.logLines.children].map((el) => el.textContent.replace(/\s+/g, ' ').trim()).join('\n');
    try {
      const saved = await api.saveLog(text);
      if (saved) addLine({ level: 'INF', msg: `log saved to ${saved}` });
    } catch (err) {
      addLine({ level: 'ERR', msg: String(err?.message ?? err) });
    }
  });

  $('minimizeBtn').addEventListener('click', () => Runtime?.WindowMinimise());
  $('closeBtn').addEventListener('click', () => Runtime?.Quit());

  // Ctrl+Enter starts, Esc stops: handy while the mouse is in the log.
  document.addEventListener('keydown', (e) => {
    if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
      e.preventDefault();
      els.start.click();
    }
    if (e.key === 'Escape' && !els.stop.disabled) els.stop.click();
  });

  // ------------------------------------------------------------- bootstrap

  if (Runtime?.EventsOn) {
    Runtime.EventsOn('log', (entry) => addLine(entry));
    Runtime.EventsOn('state', (state) => renderState(state));
  }

  api
    .load()
    .then((s) => {
      if (!s) return;
      els.server.value = s.server ?? '';
      els.proxy.value = s.proxy ?? '';
      els.port.value = String(s.port ?? 25565);
      if (s.kind) setKind(s.kind);
    })
    .catch((err) => addLine({ level: 'WRN', msg: `settings: ${err?.message ?? err}` }))
    .finally(() => {
      refresh();
      startPolling();
    });

  // ------------------------------------------------------ browser preview

  function mockApi() {
    const state = { running: false, startedAt: 0, sessions: 0, up: 0, down: 0, uptimeSec: 0 };
    const wall = () =>
      addLine({ time: clockText(new Date()), level: 'INF', msg: 'browser preview: Go backend is not attached' });

    return {
      async load() {
        setTimeout(wall, 120);
        return { server: 'play.example.com:25565', proxy: 'socks5://user:pass@proxy.example.com:8000', kind: 'socks5', port: 25565 };
      },
      async start() {
        await new Promise((r) => setTimeout(r, 700));
        state.running = true;
        state.startedAt = Date.now();
        addLine({ level: 'INF', msg: `127.0.0.1:${els.port.value} listening` });
        return { kind: proxyKind() };
      },
      async stop() {
        state.running = false;
        state.startedAt = 0;
        addLine({ level: 'INF', msg: 'tunnel stopped' });
      },
      async test() {
        await new Promise((r) => setTimeout(r, 1100));
        addLine({ level: 'INF', msg: 'proxy check: ok, tunnel in 30 ms, exit IP 203.0.113.7' });
      },
      async state() {
        if (state.running) {
          state.uptimeSec = Math.floor((Date.now() - state.startedAt) / 1000);
          state.up += 900;
          state.down += 4300;
        }
        return { ...state };
      },
      async saveLog() {
        return '';
      },
    };
  }
})();
