#!/bin/zsh
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
APP="$PROJECT_ROOT/VeilPass.app"
CONTENTS="$APP/Contents"
MODULE_CACHE="${TMPDIR:-/tmp}/veilpass-swift-module-cache"
ICONSET_DIR="$MODULE_CACHE/AppIcon.iconset"
ICON_GENERATOR="$MODULE_CACHE/generate_icon.swift"

mkdir -p "$MODULE_CACHE"
export SWIFT_MODULECACHE_PATH="$MODULE_CACHE"
export CLANG_MODULECACHE_PATH="$MODULE_CACHE"

mkdir -p "$CONTENTS/MacOS" "$CONTENTS/Resources"

cat > "$ICON_GENERATOR" <<'SWIFT'
import Cocoa

let iconsetPath = CommandLine.arguments[1]
let fm = FileManager.default
try? fm.removeItem(atPath: iconsetPath)
try fm.createDirectory(atPath: iconsetPath, withIntermediateDirectories: true)

func renderIcon(pixelSize: Int) -> Data? {
    let size = CGFloat(pixelSize)
    guard let rep = NSBitmapImageRep(
        bitmapDataPlanes: nil,
        pixelsWide: pixelSize,
        pixelsHigh: pixelSize,
        bitsPerSample: 8,
        samplesPerPixel: 4,
        hasAlpha: true,
        isPlanar: false,
        colorSpaceName: .deviceRGB,
        bytesPerRow: 0,
        bitsPerPixel: 0
    ) else { return nil }
    rep.size = NSSize(width: size, height: size)

    NSGraphicsContext.saveGraphicsState()
    guard let ctx = NSGraphicsContext(bitmapImageRep: rep) else { return nil }
    NSGraphicsContext.current = ctx
    let cg = ctx.cgContext

    let inset = size * 0.098
    let rect = CGRect(x: inset, y: inset, width: size - inset * 2, height: size - inset * 2)
    let cornerRadius = rect.width * 0.225
    let squircle = NSBezierPath(roundedRect: rect, xRadius: cornerRadius, yRadius: cornerRadius)

    cg.saveGState()
    cg.setShadow(
        offset: CGSize(width: 0, height: -size * 0.018),
        blur: size * 0.036,
        color: NSColor(calibratedWhite: 0, alpha: 0.34).cgColor
    )
    NSColor.black.setFill()
    squircle.fill()
    cg.restoreGState()

    cg.saveGState()
    squircle.addClip()

    let cs = CGColorSpaceCreateDeviceRGB()
    let bgColors = [
        NSColor(calibratedRed: 0.043, green: 0.118, blue: 0.106, alpha: 1.0).cgColor,
        NSColor(calibratedRed: 0.059, green: 0.298, blue: 0.243, alpha: 1.0).cgColor,
        NSColor(calibratedRed: 0.094, green: 0.576, blue: 0.447, alpha: 1.0).cgColor
    ] as CFArray
    if let grad = CGGradient(colorsSpace: cs, colors: bgColors, locations: [0.0, 0.5, 1.0]) {
        cg.drawLinearGradient(
            grad,
            start: CGPoint(x: rect.minX, y: rect.minY),
            end: CGPoint(x: rect.maxX, y: rect.maxY),
            options: []
        )
    }

    let glowColors = [
        NSColor(calibratedRed: 0.35, green: 0.96, blue: 0.78, alpha: 0.24).cgColor,
        NSColor(calibratedRed: 0.35, green: 0.96, blue: 0.78, alpha: 0.0).cgColor
    ] as CFArray
    if let glow = CGGradient(colorsSpace: cs, colors: glowColors, locations: [0.0, 1.0]) {
        cg.drawRadialGradient(
            glow,
            startCenter: CGPoint(x: rect.midX, y: rect.maxY * 0.86),
            startRadius: 0,
            endCenter: CGPoint(x: rect.midX, y: rect.maxY * 0.86),
            endRadius: rect.width * 0.65,
            options: []
        )
    }

    let rimRect = rect.insetBy(dx: max(1.0, size * 0.004), dy: max(1.0, size * 0.004))
    let rimPath = NSBezierPath(
        roundedRect: rimRect,
        xRadius: cornerRadius - size * 0.004,
        yRadius: cornerRadius - size * 0.004
    )
    rimPath.lineWidth = max(1.0, size * 0.012)
    NSColor(calibratedWhite: 1.0, alpha: 0.20).setStroke()
    rimPath.stroke()

    let center = CGPoint(x: size * 0.5, y: size * 0.5)
    let dialRadius = rect.width * 0.31

    let dialDisc = NSBezierPath(ovalIn: CGRect(
        x: center.x - dialRadius,
        y: center.y - dialRadius,
        width: dialRadius * 2,
        height: dialRadius * 2
    ))
    NSColor(calibratedWhite: 1.0, alpha: 0.12).setFill()
    dialDisc.fill()

    dialDisc.lineWidth = max(1.5, rect.width * 0.042)
    NSColor(calibratedWhite: 1.0, alpha: 0.94).setStroke()
    dialDisc.stroke()

    if pixelSize >= 64 {
        let tickInner = dialRadius * 0.76
        let tickOuter = dialRadius * 0.89
        let tickWidth = max(1.2, rect.width * 0.018)
        NSColor(calibratedWhite: 1.0, alpha: 0.55).setStroke()
        for i in 0..<8 {
            let angle = CGFloat(i) * (.pi / 4.0)
            let p1 = CGPoint(x: center.x + cos(angle) * tickInner, y: center.y + sin(angle) * tickInner)
            let p2 = CGPoint(x: center.x + cos(angle) * tickOuter, y: center.y + sin(angle) * tickOuter)
            let tick = NSBezierPath()
            tick.move(to: p1)
            tick.line(to: p2)
            tick.lineWidth = (i % 2 == 0) ? tickWidth : tickWidth * 0.65
            tick.lineCapStyle = .round
            tick.stroke()
        }
    }

    cg.saveGState()
    cg.translateBy(x: center.x, y: center.y)
    cg.rotate(by: -.pi / 4.0)
    cg.setShadow(
        offset: CGSize(width: 0, height: -size * 0.010),
        blur: size * 0.022,
        color: NSColor(calibratedWhite: 0, alpha: 0.28).cgColor
    )

    let tipLen = dialRadius * 0.70
    let halfWidth = dialRadius * 0.22

    let northNeedle = NSBezierPath()
    northNeedle.move(to: CGPoint(x: 0, y: tipLen))
    northNeedle.line(to: CGPoint(x: halfWidth, y: 0))
    northNeedle.line(to: CGPoint(x: -halfWidth, y: 0))
    northNeedle.close()
    NSColor(calibratedRed: 0.58, green: 0.99, blue: 0.84, alpha: 1.0).setFill()
    northNeedle.fill()

    let southNeedle = NSBezierPath()
    southNeedle.move(to: CGPoint(x: 0, y: -tipLen))
    southNeedle.line(to: CGPoint(x: halfWidth, y: 0))
    southNeedle.line(to: CGPoint(x: -halfWidth, y: 0))
    southNeedle.close()
    NSColor(calibratedWhite: 1.0, alpha: 0.96).setFill()
    southNeedle.fill()

    let pivotR = dialRadius * 0.09
    let pivot = NSBezierPath(ovalIn: CGRect(x: -pivotR, y: -pivotR, width: pivotR * 2, height: pivotR * 2))
    NSColor(calibratedRed: 0.05, green: 0.22, blue: 0.18, alpha: 1.0).setFill()
    pivot.fill()

    cg.restoreGState()
    cg.restoreGState()
    NSGraphicsContext.restoreGraphicsState()

    return rep.representation(using: .png, properties: [:])
}

let specs: [(String, Int)] = [
    ("icon_16x16.png", 16),
    ("icon_16x16@2x.png", 32),
    ("icon_32x32.png", 32),
    ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128),
    ("icon_128x128@2x.png", 256),
    ("icon_256x256.png", 256),
    ("icon_256x256@2x.png", 512),
    ("icon_512x512.png", 512),
    ("icon_512x512@2x.png", 1024),
]

for (filename, px) in specs {
    if let data = renderIcon(pixelSize: px) {
        let dest = URL(fileURLWithPath: iconsetPath).appendingPathComponent(filename)
        try data.write(to: dest)
    }
}
SWIFT

swift "$ICON_GENERATOR" "$ICONSET_DIR"
iconutil -c icns "$ICONSET_DIR" -o "$CONTENTS/Resources/AppIcon.icns"

ARM64_BIN="$MODULE_CACHE/VeilPass-arm64"
X64_BIN="$MODULE_CACHE/VeilPass-x86_64"
SWIFT_SRC="$PROJECT_ROOT/macos/StableBrowserApp.swift"

if swiftc -O -target arm64-apple-macos12.0 -framework Cocoa -framework CFNetwork "$SWIFT_SRC" -o "$ARM64_BIN" \
   && swiftc -O -target x86_64-apple-macos12.0 -framework Cocoa -framework CFNetwork "$SWIFT_SRC" -o "$X64_BIN"; then
  lipo -create "$ARM64_BIN" "$X64_BIN" -output "$CONTENTS/MacOS/VeilPass"
else
  swiftc \
    -O \
    -framework Cocoa \
    -framework CFNetwork \
    "$SWIFT_SRC" \
    -o "$CONTENTS/MacOS/VeilPass"
fi

cat > "$CONTENTS/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>CFBundleDisplayName</key>
    <string>VeilPass</string>
    <key>CFBundleExecutable</key>
    <string>VeilPass</string>
    <key>CFBundleIconFile</key>
    <string>AppIcon</string>
    <key>CFBundleIdentifier</key>
    <string>io.github.chenxicore.veilpass</string>
    <key>CFBundleName</key>
    <string>VeilPass</string>
    <key>CFBundlePackageType</key>
    <string>APPL</string>
    <key>CFBundleShortVersionString</key>
    <string>2.0</string>
    <key>CFBundleVersion</key>
    <string>2</string>
    <key>LSMinimumSystemVersion</key>
    <string>12.0</string>
    <key>NSHighResolutionCapable</key>
    <true/>
</dict>
</plist>
PLIST

chmod +x "$CONTENTS/MacOS/VeilPass"
codesign --force --sign - "$APP"
touch "$APP"

LSREGISTER="/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister"
if [ -x "$LSREGISTER" ]; then
  "$LSREGISTER" -f "$APP" >/dev/null 2>&1 || true
fi

if [[ "${1:-}" == "--install" ]]; then
  INSTALL_APP="/Applications/VeilPass.app"
  rm -rf "$INSTALL_APP"
  ditto "$APP" "$INSTALL_APP"
  touch "$INSTALL_APP"
  if [ -x "$LSREGISTER" ]; then
    "$LSREGISTER" -f "$INSTALL_APP" >/dev/null 2>&1 || true
  fi
  echo "Installed: $INSTALL_APP"
fi

if [[ "${1:-}" == "--zip" ]]; then
  STAGE_DIR="$MODULE_CACHE/zip_stage"
  ZIP_OUT="$PROJECT_ROOT/VeilPass-macOS.zip"
  rm -rf "$STAGE_DIR" "$ZIP_OUT"
  mkdir -p "$STAGE_DIR"
  ditto "$APP" "$STAGE_DIR/VeilPass.app"
  ditto -c -k --sequesterRsrc --keepParent "$STAGE_DIR/VeilPass.app" "$ZIP_OUT"
  echo "Packaged: $ZIP_OUT"
fi

if [[ "${1:-}" == "--dmg" ]]; then
  DMG_STAGE="$MODULE_CACHE/dmg_stage"
  DMG_OUT="$PROJECT_ROOT/VeilPass.dmg"
  rm -rf "$DMG_STAGE" "$DMG_OUT"
  mkdir -p "$DMG_STAGE"

  ditto "$APP" "$DMG_STAGE/VeilPass.app"
  ln -s /Applications "$DMG_STAGE/Applications"
  cp "$CONTENTS/Resources/AppIcon.icns" "$DMG_STAGE/.VolumeIcon.icns"

  SETFILE_BIN="$(xcrun -find SetFile 2>/dev/null || true)"
  if [ -n "$SETFILE_BIN" ]; then
    "$SETFILE_BIN" -c icnC "$DMG_STAGE/.VolumeIcon.icns" 2>/dev/null || true
    "$SETFILE_BIN" -a C "$DMG_STAGE" 2>/dev/null || true
  fi

  hdiutil create \
    -volname "VeilPass" \
    -srcfolder "$DMG_STAGE" \
    -ov \
    -format UDZO \
    -imagekey zlib-level=9 \
    "$DMG_OUT" >/dev/null

  echo "Packaged DMG: $DMG_OUT"
fi

echo "Built: $APP"
