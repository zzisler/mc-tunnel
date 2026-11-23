"""Renders a mockup of a redesigned MC Tunnel window (dark theme).

Usage: python mock.py <out.png> <expanded|collapsed>
"""

import sys
from PIL import Image, ImageDraw, ImageFont

FONTS = r"C:\Windows\Fonts"
UI = FONTS + r"\segoeui.ttf"
UI_B = FONTS + r"\seguisb.ttf"
MONO = FONTS + r"\consola.ttf"


def font(path, size):
    return ImageFont.truetype(path, size)


BG = "#17191F"
PANEL = "#1F222A"
FIELD = "#262A34"
BORDER = "#343A48"
TEXT = "#E7EAF0"
MUTED = "#8B94A8"
DIM = "#5D6678"
ACCENT = "#4C8DFF"
GREEN = "#3FB950"
AMBER = "#D29922"
RED = "#F85149"


def rr(d, box, radius, fill=None, outline=None, width=1):
    d.rounded_rectangle(box, radius=radius, fill=fill, outline=outline, width=width)


def text(d, xy, s, f, color, anchor="la"):
    d.text(xy, s, font=f, fill=color, anchor=anchor)


def fit(d, s, f, maxw):
    """Shortens s with an ellipsis until it fits into maxw pixels."""
    if d.textlength(s, font=f) <= maxw:
        return s
    while s and d.textlength(s + "\u2026", font=f) > maxw:
        s = s[:-1]
    return s + "\u2026"


def draw_ui(expanded: bool) -> Image.Image:
    width = 930 if expanded else 430
    height = 391
    img = Image.new("RGB", (width, height), BG)
    d = ImageDraw.Draw(img)

    f_title = font(UI_B, 15)
    f_sub = font(UI, 11)
    f_label = font(UI, 11)
    f_hint = font(UI, 10)
    f_body = font(UI, 13)
    f_btn = font(UI_B, 13)
    f_val = font(UI_B, 13)
    f_seg = font(UI_B, 12)
    f_mono = font(MONO, 11)

    pad = 18

    # ---- header -----------------------------------------------------------
    text(d, (pad, 14), "MC Tunnel", f_title, TEXT)
    badge = (pad + 92, 17, pad + 150, 33)
    rr(d, badge, 8, fill="#242A36")
    text(d, ((badge[0] + badge[2]) / 2, 20), "1.0.0", f_sub, MUTED, anchor="ma")

    # caption buttons: minimize, close
    bx = width - pad
    d.line((bx - 34, 21, bx - 26, 21), fill=MUTED, width=1)
    d.line((bx - 6, 19, bx + 2, 27), fill=MUTED, width=1)
    d.line((bx - 6, 27, bx + 2, 19), fill=MUTED, width=1)

    # log toggle chip (only when the log panel is hidden)
    if not expanded:
        tg = (bx - 72, 16, bx - 46, 34)
        rr(d, tg, 6, fill="#242A36", outline=BORDER)
        for i in range(3):
            ly2 = tg[1] + 8 + i * 4
            d.line((tg[0] + 7, ly2, tg[2] - 7, ly2), fill=MUTED, width=1)

    # ---- left column ------------------------------------------------------
    lx = pad
    lw = 380
    y = 52

    text(d, (lx, y), "СЕРВЕР", f_label, DIM)
    y += 16
    rr(d, (lx, y, lx + lw, y + 32), 8, fill=FIELD, outline=BORDER)
    text(d, (lx + 11, y + 8), "play.example.com:25565", f_body, TEXT)
    y += 45

    text(d, (lx, y), "ПРОКСИ", f_label, DIM)
    text(d, (lx + 78, y), "scheme://user:pass@host:port", f_hint, DIM)
    y += 16
    rr(d, (lx, y, lx + lw, y + 32), 8, fill=FIELD, outline=ACCENT, width=1)
    text(d, (lx + 11, y + 8), "http://user:••••••••@198.51.100.7:39584", f_body, TEXT)
    y += 45

    text(d, (lx, y), "ПРОТОКОЛ", f_label, DIM)
    text(d, (lx + 255, y), "ЛОКАЛЬНЫЙ ПОРТ", f_label, DIM)
    y += 16
    seg = (lx, y, lx + 196, y + 30)
    rr(d, seg, 8, fill=FIELD, outline=BORDER)
    half = (seg[2] - seg[0] - 6) // 2
    off = (seg[0] + 3, seg[1] + 3, seg[0] + 3 + half, seg[3] - 3)
    on = (off[2], seg[1] + 3, seg[2] - 3, seg[3] - 3)
    text(d, ((off[0] + off[2]) / 2, off[1] + 5), "SOCKS5", f_seg, MUTED, anchor="ma")
    rr(d, on, 6, fill=ACCENT)
    text(d, ((on[0] + on[2]) / 2, on[1] + 5), "HTTP", f_seg, "#FFFFFF", anchor="ma")

    port = (lx + 255, y, lx + 335, y + 30)
    rr(d, port, 8, fill=FIELD, outline=BORDER)
    text(d, ((port[0] + port[2]) / 2, y + 6), "25565", f_body, TEXT, anchor="ma")

    y += 42

    b1 = (lx, y, lx + 150, y + 34)
    rr(d, b1, 9, fill=ACCENT)
    text(d, ((b1[0] + b1[2]) / 2, y + 8), "Запустить", f_btn, "#FFFFFF", anchor="ma")

    b2 = (lx + 158, y, lx + 258, y + 34)
    rr(d, b2, 9, fill="#22262F", outline=BORDER)
    text(d, ((b2[0] + b2[2]) / 2, y + 8), "Стоп", f_btn, DIM, anchor="ma")

    b3 = (lx + 266, y, lx + lw, y + 34)
    rr(d, b3, 9, fill="#22262F", outline=BORDER)
    text(d, ((b3[0] + b3[2]) / 2, y + 8), "Проверить прокси", f_btn, TEXT, anchor="ma")

    y += 48

    # ---- status card ------------------------------------------------------
    card = (lx, y, lx + lw, 391 - pad)
    rr(d, card, 12, fill=PANEL, outline=BORDER)

    cy = y + 14
    d.ellipse((lx + 14, cy + 3, lx + 22, cy + 11), fill=GREEN)
    text(d, (lx + 30, cy - 1), "Туннель активен", f_val, TEXT)
    text(d, (lx + lw - 14, cy), "127.0.0.1:25565", font(MONO, 11), MUTED, anchor="ra")

    cy += 26
    d.line((lx + 14, cy, lx + lw - 14, cy), fill="#2A2F3B")

    cy += 12
    col = lw // 3
    for i, (k, v, c) in enumerate(
        [
            ("ВЫХОДНОЙ IP", "203.0.113.7", TEXT),
            ("ПИНГ", "30 мс", GREEN),
            ("ТРАФИК", "1.2 МБ ↓ · 0.4 МБ ↑", TEXT),
        ]
    ):
        cx = lx + 14 + i * col
        text(d, (cx, cy), k, font(UI, 9), DIM)
        text(d, (cx, cy + 14), v, font(UI_B, 12), c)

    # ---- log panel --------------------------------------------------------
    if expanded:
        px = 424
        pw = width - pad - px
        panel = (px, 16, px + pw, 391 - pad)
        rr(d, panel, 12, fill=PANEL, outline=BORDER)

        text(d, (px + 16, 28), "Журнал", font(UI_B, 13), TEXT)

        chev = (panel[2] - 40, 24, panel[2] - 14, 48)
        rr(d, chev, 7, fill="#22262F", outline=BORDER)
        text(d, ((chev[0] + chev[2]) / 2, 29), "»", font(UI_B, 12), MUTED, anchor="ma")

        clr = (chev[0] - 90, 24, chev[0] - 8, 48)
        rr(d, clr, 7, fill="#22262F", outline=BORDER)
        text(d, ((clr[0] + clr[2]) / 2, 30), "Очистить", font(UI, 11), MUTED, anchor="ma")

        d.line((px + 14, 58, px + pw - 14, 58), fill="#2A2F3B")

        logs = [
            ("13:02:14", "INF", "settings loaded from %APPDATA%\\mctunnel\\config.json", MUTED),
            ("13:02:19", "INF", "proxy check: ok, tunnel in 30 ms, exit IP 203.0.113.7", DIM),
            ("13:02:21", "INF", "listening on 127.0.0.1:25565", DIM),
            ("13:02:25", "INF", "client connected", DIM),
            ("13:02:25", "INF", "tunnel established in 41 ms", DIM),
            ("13:02:44", "WRN", "connection reset by the proxy", AMBER),
            ("13:02:45", "ERR", "play.example.com: no status response: EOF", RED),
        ]
        msgx = px + 112
        maxw = panel[2] - 16 - msgx
        ly = 72
        for ts, lvl, msg, colr in logs:
            text(d, (px + 16, ly), ts, f_mono, DIM)
            text(d, (px + 74, ly), lvl, f_mono, {"INF": MUTED, "WRN": AMBER, "ERR": RED}[lvl])
            text(d, (msgx, ly), fit(d, msg, f_mono, maxw), f_mono, colr)
            ly += 21

    else:
        # collapsed: the form column fills the window, the log is hidden
        pass

    return img


if __name__ == "__main__":
    out = sys.argv[1]
    mode = sys.argv[2] if len(sys.argv) > 2 else "expanded"
    draw_ui(mode == "expanded").save(out)
    print("wrote", out)
