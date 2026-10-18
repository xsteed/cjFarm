#!/usr/bin/env python3
"""从品牌图标生成安卓各密度启动图标（由 build-android-shell.sh 调用）。

为什么要有这个脚本：
    frontend/android/ 是 `npx cap add android` 生成物、不入库，
    每次重新生成都会回到 Capacitor 的默认图标。
    把「生成图标」放进构建流水线，才能保证任何一次出包都带上店铺 Logo。

输入：
    frontend/public/favicon-512x512.png  —— 橙色圆角方块 + 白色果实

输出（写入 android/app/src/main/res/）：
    mipmap-{mdpi,hdpi,xhdpi,xxhdpi,xxxhdpi}/ic_launcher.png          传统图标（API < 26）
    mipmap-{...}/ic_launcher_round.png                               圆形传统图标
    mipmap-{...}/ic_launcher_foreground.png                          自适应图标前景（108dp 画布）
    values/ic_launcher_background.xml                                自适应图标背景色（取自 Logo 底色）

自适应图标的要点：
    画布 108dp，系统按 66dp 的「安全区」裁切。因此前景内容只占画布约 58%，
    否则在不同形状的遮罩（圆形/方形/水滴）下会被切边。

抠图思路：
    Logo 底色是橙色（HSV 饱和度 140+），主体是白色/米色（饱和度 ≤ 67）。
    以饱和度做软阈值（80→不透明，100→透明），保留果实底部的米色阴影，
    避免硬阈值在果实底部切出一道橙色缝隙。
"""

from __future__ import annotations

import sys
from pathlib import Path

from PIL import Image, ImageDraw, ImageFilter

# 各密度倍率：mdpi=1x，其余按 Android 约定
DENSITIES = {
    'mdpi': 1.0,
    'hdpi': 1.5,
    'xhdpi': 2.0,
    'xxhdpi': 3.0,
    'xxxhdpi': 4.0,
}

LEGACY_DP = 48        # 传统图标 48dp
FOREGROUND_DP = 108   # 自适应图标画布 108dp
# 前景内容占画布比例。
# 原 Logo 的主体约占画面 65%，但它是「圆角方形」画布；放到圆形遮罩里同样的 65%
# 会显得过满，因此这里收到 0.45（比原图更透气）。想更饱满改 0.50 / 0.55。
CONTENT_RATIO = 0.45
LEGACY_RATIO = 0.66   # 传统图标里主体的占比（全幅显示，可略饱满）

# 饱和度软阈值：≤ S_OPAQUE 完全保留，≥ S_CLEAR 完全透明
S_OPAQUE = 80
S_CLEAR = 100

# 内缩比例：去掉 Logo 外围的圆角与浅色描边，避免被当成前景
INSET_RATIO = 0.10


def sample_background(logo: Image.Image) -> tuple[int, int, int]:
    """取 Logo 左侧中部一条竖带的中位色作为品牌底色，避开中间的果实。"""
    w, h = logo.size
    xs = range(int(w * 0.08), int(w * 0.14))
    ys = range(int(h * 0.35), int(h * 0.65))
    pixels = [logo.getpixel((x, y))[:3] for x in xs for y in ys]
    pixels.sort(key=lambda p: sum(p))
    return pixels[len(pixels) // 2]


def extract_foreground(logo: Image.Image) -> Image.Image:
    """抠出浅色主体（果实/叶片），返回透明底、已按主体裁剪的图片。"""
    w, h = logo.size
    inset = int(min(w, h) * INSET_RATIO)
    core = logo.crop((inset, inset, w - inset, h - inset))

    _, s, _ = core.convert('HSV').split()
    span = S_CLEAR - S_OPAQUE
    alpha = s.point(lambda x: 0 if x >= S_CLEAR else (255 if x <= S_OPAQUE else int((S_CLEAR - x) * 255 / span)))
    alpha = alpha.filter(ImageFilter.GaussianBlur(0.5))

    fg = core.copy()
    fg.putalpha(alpha)
    bbox = fg.getbbox()
    return fg.crop(bbox) if bbox else fg


def alpha_centroid(img: Image.Image) -> tuple[float, float]:
    """按 alpha 加权求质心。

    果实占绝大部分不透明面积，因此质心基本落在果实中心；叶片、果柄只轻微拉动。
    用质心对齐而不是包围盒居中，是因为叶片向右上伸出会把包围盒重心带偏，
    按包围盒居中会让果实整体偏到左下（原 Logo 的构图并非如此）。
    """
    w, h = img.size
    small = img.getchannel('A').resize((32, 32), Image.BOX)
    px = small.load()
    sx = sy = sa = 0.0
    for y in range(32):
        for x in range(32):
            v = px[x, y]
            if v:
                sx += x * v
                sy += y * v
                sa += v
    if not sa:
        return w / 2, h / 2
    return sx / sa * w / 32, sy / sa * h / 32


def fit_center(src: Image.Image, canvas_px: int, ratio: float, background=None) -> Image.Image:
    """等比缩放后按质心对齐到画布中心；background 为 None 时用透明底。"""
    canvas = Image.new('RGBA', (canvas_px, canvas_px), background or (0, 0, 0, 0))
    target = int(canvas_px * ratio)
    scale = min(target / src.width, target / src.height)
    size = (max(1, round(src.width * scale)), max(1, round(src.height * scale)))
    resized = src.resize(size, Image.LANCZOS)

    cx, cy = alpha_centroid(resized)
    x = round(canvas_px / 2 - cx)
    y = round(canvas_px / 2 - cy)
    # 夹住，避免质心偏离过大时把内容推出画布
    x = max(0, min(canvas_px - size[0], x))
    y = max(0, min(canvas_px - size[1], y))

    canvas.alpha_composite(resized, (x, y))
    return canvas


def circular(img: Image.Image) -> Image.Image:
    """裁成圆形（传统 round 图标用）。"""
    size = img.size[0]
    mask = Image.new('L', (size * 4, size * 4), 0)
    ImageDraw.Draw(mask).ellipse((0, 0, size * 4 - 1, size * 4 - 1), fill=255)
    mask = mask.resize((size, size), Image.LANCZOS)
    out = img.copy().convert('RGBA')
    out.putalpha(mask)
    return out


def main() -> int:
    if len(sys.argv) != 3:
        print('用法: gen-android-icons.py <logo.png> <res目录>', file=sys.stderr)
        return 2

    logo_path, res_dir = Path(sys.argv[1]), Path(sys.argv[2])
    if not logo_path.is_file():
        print(f'找不到图标源文件: {logo_path}', file=sys.stderr)
        return 1
    if not res_dir.is_dir():
        print(f'找不到 res 目录: {res_dir}', file=sys.stderr)
        return 1

    logo = Image.open(logo_path).convert('RGBA')
    background = sample_background(logo)
    bg_rgba = (*background, 255)
    subject = extract_foreground(logo)

    if subject.width < 8 or subject.height < 8:
        print('前景提取失败（结果过小），改用整图作为前景', file=sys.stderr)
        subject = logo

    for name, scale in DENSITIES.items():
        out_dir = res_dir / f'mipmap-{name}'
        out_dir.mkdir(parents=True, exist_ok=True)

        legacy_px = round(LEGACY_DP * scale)
        legacy = fit_center(subject, legacy_px, LEGACY_RATIO, bg_rgba)
        legacy.convert('RGB').save(out_dir / 'ic_launcher.png')
        circular(legacy).save(out_dir / 'ic_launcher_round.png')

        fg_px = round(FOREGROUND_DP * scale)
        fit_center(subject, fg_px, CONTENT_RATIO).save(out_dir / 'ic_launcher_foreground.png')

    values_dir = res_dir / 'values'
    values_dir.mkdir(parents=True, exist_ok=True)
    hex_color = '#%02X%02X%02X' % background
    (values_dir / 'ic_launcher_background.xml').write_text(
        '<?xml version="1.0" encoding="utf-8"?>\n'
        '<resources>\n'
        f'    <color name="ic_launcher_background">{hex_color}</color>\n'
        '</resources>\n',
        encoding='utf-8',
    )

    print(f'图标已生成（背景色 {hex_color}，主体 {subject.width}x{subject.height}）')
    return 0


if __name__ == '__main__':
    sys.exit(main())
