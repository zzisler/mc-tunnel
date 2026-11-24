"""Renders the MC Tunnel cube (the SVG from the title bar) into a Windows .ico.

The SVG is six paths in a 32x32 view box; they are replayed here as polygons at
4x supersampling per size, so 16px still reads as a cube, then packed into an
ICO with PNG entries.

Usage: python icon.py <out.ico>
"""

import io
import struct
import sys

from PIL import Image, ImageDraw

# Geometry from the original SVG, in its 32x32 coordinate space.
TOP = [(16, 2.7), (28.1, 9.4), (16, 16.2), (3.9, 9.4)]
LEFT = [(3.9, 9.4), (16, 16.2), (16, 29.3), (3.9, 22.6)]
RIGHT = [(28.1, 9.4), (16, 16.2), (16, 29.3), (28.1, 22.6)]

BASE = (0x78, 0x60, 0xE9, 255)
TOP_C = (0xA1, 0x8B, 0xFF, 255)
LEFT_C = (0x57, 0x40, 0xC5, 255)
RIGHT_C = (0x38, 0x28, 0x8D, 255)
EDGE = (0xC7, 0xBC, 0xFF, 140)

SIZES = [16, 20, 24, 32, 40, 48, 64, 128, 256]
SS = 4  # supersampling factor


def project(points, scale, size):
    """Maps view-box points onto a canvas, centred, so the cube keeps its shape."""
    cx = cy = size / 2
    return [((x - 16) * scale + cx, (y - 16) * scale + cy) for x, y in points]


def render(size):
    canvas = size * SS
    # Leave a margin so the cube does not touch the edges at small sizes.
    scale = (canvas * 0.86) / 26.6
    img = Image.new("RGBA", (canvas, canvas), (0, 0, 0, 0))
    d = ImageDraw.Draw(img)

    d.polygon(project(TOP, scale, canvas), fill=TOP_C)
    d.polygon(project(LEFT, scale, canvas), fill=LEFT_C)
    d.polygon(project(RIGHT, scale, canvas), fill=RIGHT_C)

    # The light seam from the apex down the middle, as in the SVG.
    seam = project([(16, 2.7), (16, 16.2)], scale, canvas)
    d.line(seam, fill=EDGE, width=max(1, int(round(scale * 0.9))))

    return img.resize((size, size), Image.LANCZOS)


def write_ico(path, images):
    """Writes an ICO with PNG-compressed entries, one per size."""
    header = struct.pack("<HHH", 0, 1, len(images))
    entries = b""
    payload = b""
    offset = 6 + 16 * len(images)

    for size, png in images:
        dim = 0 if size >= 256 else size
        entries += struct.pack("<BBBBHHII", dim, dim, 0, 0, 1, 32, len(png), offset)
        offset += len(png)
        payload += png

    with open(path, "wb") as f:
        f.write(header + entries + payload)


def main():
    out = sys.argv[1] if len(sys.argv) > 1 else "build/windows/icon.ico"
    images = []
    for size in SIZES:
        buf = io.BytesIO()
        render(size).save(buf, format="PNG", optimize=True)
        images.append((size, buf.getvalue()))
        print(f"  {size}x{size}: {len(buf.getvalue())} bytes")

    write_ico(out, images)
    print(f"wrote {out}")


if __name__ == "__main__":
    main()
