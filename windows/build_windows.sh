#!/bin/zsh
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
WIN_DIR="$PROJECT_ROOT/windows"
OUT_EXE="$PROJECT_ROOT/VeilPass.exe"
CACHE_DIR="${TMPDIR:-/tmp}/veilpass-win-build"
ICON_DIR="$CACHE_DIR/iconset"
ICON_GEN="$CACHE_DIR/generate_win_icon.swift"

mkdir -p "$CACHE_DIR" "$ICON_DIR"

cat > "$ICON_GEN" <<'SWIFT'
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

    let inset = size * 0.06
    let rect = CGRect(x: inset, y: inset, width: size - inset * 2, height: size - inset * 2)
    let cornerRadius = rect.width * 0.225
    let squircle = NSBezierPath(roundedRect: rect, xRadius: cornerRadius, yRadius: cornerRadius)

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

    let rimRect = rect.insetBy(dx: max(1.0, size * 0.006), dy: max(1.0, size * 0.006))
    let rimPath = NSBezierPath(
        roundedRect: rimRect,
        xRadius: cornerRadius - size * 0.006,
        yRadius: cornerRadius - size * 0.006
    )
    rimPath.lineWidth = max(1.0, size * 0.015)
    NSColor(calibratedWhite: 1.0, alpha: 0.22).setStroke()
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

    dialDisc.lineWidth = max(1.5, rect.width * 0.045)
    NSColor(calibratedWhite: 1.0, alpha: 0.94).setStroke()
    dialDisc.stroke()

    if pixelSize >= 64 {
        let tickInner = dialRadius * 0.76
        let tickOuter = dialRadius * 0.89
        let tickWidth = max(1.2, rect.width * 0.020)
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
    ("icon_32x32.png", 32),
    ("icon_32x32@2x.png", 64),
    ("icon_128x128.png", 128),
    ("icon_256x256.png", 256),
]

for (filename, px) in specs {
    if let data = renderIcon(pixelSize: px) {
        let dest = URL(fileURLWithPath: iconsetPath).appendingPathComponent(filename)
        try data.write(to: dest)
    }
}
SWIFT

swift "$ICON_GEN" "$ICON_DIR"

cd "$WIN_DIR"
go run gen_syso.go "$ICON_DIR" "$WIN_DIR/rsrc_windows_amd64.syso"
trap 'rm -f "$WIN_DIR/rsrc_windows_amd64.syso"' EXIT

go test -count=1 -v ./...
GOOS=windows GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -H windowsgui" -o "$OUT_EXE" .

echo "Built: $OUT_EXE"
