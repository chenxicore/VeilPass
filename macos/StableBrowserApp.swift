import CFNetwork
import Cocoa
import CryptoKit
import Darwin
import SwiftUI

// MARK: - Proxy Mode & Validation

private enum ProxyMode: String, CaseIterable, Identifiable {
    case system = "系统代理"
    case manual = "手动代理"
    case direct = "直连网络"

    var id: String { rawValue }

    var symbolName: String {
        switch self {
        case .system: return "globe.badge.chevron.backward"
        case .manual: return "slider.horizontal.3"
        case .direct: return "bolt.horizontal"
        }
    }

    var hint: String {
        switch self {
        case .system:
            if let detected = SystemProxyResolver.currentProxyURL() {
                return "已检测到系统代理 \(detected)，将自动匹配出口语言、时区与指纹。"
            }
            return "当前未开启系统代理，将按默认网络出口构建独立浏览器环境。"
        case .manual:
            return "支持 http / https / socks4 / socks5（暂不支持带账号密码的代理）。"
        case .direct:
            return "强制直连（绕过系统代理），基于本地真实网络构造独立浏览器环境。"
        }
    }

    static func normalizeManualProxy(_ address: String) throws -> String {
        let input = address.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !input.isEmpty else {
            throw ProxyInputError.empty
        }
        let url = input.contains("://") ? input : "http://\(input)"
        guard
            input.rangeOfCharacter(from: .whitespacesAndNewlines) == nil,
            var components = URLComponents(string: url),
            let scheme = components.scheme?.lowercased(),
            ["http", "https", "socks4", "socks5", "socks5h"].contains(scheme),
            let host = components.host, !host.isEmpty,
            let port = components.port, (1...65535).contains(port),
            components.path.isEmpty || components.path == "/",
            components.query == nil, components.fragment == nil
        else {
            throw ProxyInputError.invalidAddress
        }
        guard components.user == nil, components.password == nil else {
            throw ProxyInputError.authentication
        }
        components.scheme = scheme
        components.path = ""
        guard let proxy = components.string else {
            throw ProxyInputError.invalidAddress
        }
        return proxy
    }
}

private enum ProxyInputError: LocalizedError {
    case empty
    case invalidAddress
    case authentication

    var errorDescription: String? {
        switch self {
        case .empty:
            return "请输入代理地址，例如 socks5://127.0.0.1:1080。"
        case .invalidAddress:
            return "代理格式无效，请填写地址和端口，例如 socks5://127.0.0.1:1080。"
        case .authentication:
            return "手动填写仅支持无需账号密码的代理地址。"
        }
    }
}

private enum SystemProxyResolver {
    static func currentProxyURL() -> String? {
        guard let dict = CFNetworkCopySystemProxySettings()?.takeRetainedValue() as? [String: Any] else {
            return nil
        }
        if (dict[kCFNetworkProxiesSOCKSEnable as String] as? NSNumber)?.intValue == 1,
           let host = dict[kCFNetworkProxiesSOCKSProxy as String] as? String, !host.isEmpty,
           let port = (dict[kCFNetworkProxiesSOCKSPort as String] as? NSNumber)?.intValue, port > 0 {
            return "socks5://\(host):\(port)"
        }
        if (dict[kCFNetworkProxiesHTTPSEnable as String] as? NSNumber)?.intValue == 1,
           let host = dict[kCFNetworkProxiesHTTPSProxy as String] as? String, !host.isEmpty,
           let port = (dict[kCFNetworkProxiesHTTPSPort as String] as? NSNumber)?.intValue, port > 0 {
            return "http://\(host):\(port)"
        }
        if (dict[kCFNetworkProxiesHTTPEnable as String] as? NSNumber)?.intValue == 1,
           let host = dict[kCFNetworkProxiesHTTPProxy as String] as? String, !host.isEmpty,
           let port = (dict[kCFNetworkProxiesHTTPPort as String] as? NSNumber)?.intValue, port > 0 {
            return "http://\(host):\(port)"
        }
        return nil
    }
}

// MARK: - Environment & Profile Data Models

private struct BrowserEnvironment: Codable, Sendable {
    let version: Int
    let proxyKey: String
    let exitIP: String
    let exitCountry: String
    let exitRegion: String
    let exitCity: String
    let exitSource: String
    let locale: String
    let languages: [String]
    let acceptLanguage: String
    let timezone: String
    let viewportWidth: Int
    let viewportHeight: Int
    let screenWidth: Int
    let screenHeight: Int
    let deviceScaleFactor: Double
    let hardwareConcurrency: Int
    let deviceMemory: Int
}

private struct ProfileMeta: Codable, Sendable {
    var profileId: String
    var profileDir: String
    var devtoolsPort: Int
    var browserPid: Int
    var useProxy: Bool
    var forceDirect: Bool
    var proxyUsed: String
    var chromeProxyServer: String
    var browserEnvironment: BrowserEnvironment?
    var updatedAt: String
}

private struct ProxyExitInfo: Sendable {
    let ip: String
    let country: String
    let region: String
    let city: String
    let timezone: String
    let source: String
}

private struct ActiveInstance: Identifiable, Equatable, Sendable {
    let profileId: String
    let devtoolsPort: Int
    let browserPid: Int
    let proxyUsed: String
    let useProxy: Bool
    let forceDirect: Bool
    let exitCountry: String
    let exitCity: String
    let locale: String
    let timezone: String
    let updatedAt: String

    var id: String { profileId }

    var regionSummary: String {
        let region = [exitCountry, exitCity]
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
            .joined(separator: " / ")
        var parts: [String] = []
        if !region.isEmpty { parts.append(region) }
        if !timezone.isEmpty { parts.append(timezone) }
        if parts.isEmpty { parts.append(profileId) }
        return parts.joined(separator: " · ")
    }

    var proxySummary: String {
        if !proxyUsed.isEmpty {
            return proxyUsed
        }
        if forceDirect {
            return "直连（无代理）"
        }
        return useProxy ? "系统代理" : "直连（无代理）"
    }
}

private struct LaunchOutcome {
    let profileId: String
    let devtoolsPort: Int
    let proxyLabel: String
    let timeSummary: String
    let environmentSummary: String
}

private enum LaunchFeedback {
    case success(LaunchOutcome)
    case failure(String)
}

// MARK: - Standalone Browser Engine (Zero External Dependencies)

private enum BrowserEngineError: LocalizedError {
    case chromeNotFound
    case portAllocationFailed
    case devtoolsTimeout(Int)
    case pageTargetMissing
    case websocketError(String)
    case cdpError(String)
    case closeFailed(String)

    var errorDescription: String? {
        switch self {
        case .chromeNotFound:
            return "未检测到 Google Chrome，请先安装 Google Chrome.app。"
        case .portAllocationFailed:
            return "无法分配本地调试端口。"
        case .devtoolsTimeout(let port):
            return "Chrome 调试端口 \(port) 启动超时，请重试。"
        case .pageTargetMissing:
            return "未找到活动的 Chrome 页面标签。"
        case .websocketError(let msg):
            return "浏览器调试连接失败：\(msg)"
        case .cdpError(let msg):
            return "浏览器环境注入失败：\(msg)"
        case .closeFailed(let msg):
            return msg
        }
    }
}

private final class CDPSession: @unchecked Sendable {
    private let session: URLSession
    private let task: URLSessionWebSocketTask
    private let lock = NSLock()
    private var nextId = 1
    private var isClosed = false

    init(websocketURL: URL) throws {
        let config = URLSessionConfiguration.ephemeral
        config.connectionProxyDictionary = [:]
        self.session = URLSession(configuration: config)
        self.task = self.session.webSocketTask(with: websocketURL)
        self.task.resume()
    }

    func sendCommand(_ method: String, params: [String: Any] = [:], timeout: TimeInterval = 8.0) throws -> [String: Any] {
        lock.lock()
        let commandId = nextId
        nextId += 1
        lock.unlock()

        let payload: [String: Any] = [
            "id": commandId,
            "method": method,
            "params": params,
        ]
        let jsonData = try JSONSerialization.data(withJSONObject: payload, options: [])
        guard let jsonString = String(data: jsonData, encoding: .utf8) else {
            throw BrowserEngineError.cdpError("无法序列化命令 \(method)")
        }

        let sendSem = DispatchSemaphore(value: 0)
        var sendErr: Error?
        task.send(.string(jsonString)) { error in
            sendErr = error
            sendSem.signal()
        }
        if sendSem.wait(timeout: .now() + timeout) == .timedOut {
            throw BrowserEngineError.cdpError("发送 \(method) 超时")
        }
        if let sendErr {
            throw BrowserEngineError.websocketError(sendErr.localizedDescription)
        }

        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            let remaining = max(0.2, deadline.timeIntervalSinceNow)
            let recvSem = DispatchSemaphore(value: 0)
            var recvMessage: URLSessionWebSocketTask.Message?
            var recvErr: Error?
            task.receive { result in
                switch result {
                case .success(let message):
                    recvMessage = message
                case .failure(let error):
                    recvErr = error
                }
                recvSem.signal()
            }
            if recvSem.wait(timeout: .now() + remaining) == .timedOut {
                throw BrowserEngineError.cdpError("DevTools \(method) 响应超时")
            }
            if let recvErr {
                throw BrowserEngineError.websocketError(recvErr.localizedDescription)
            }
            guard let recvMessage else { continue }
            let rawData: Data?
            switch recvMessage {
            case .string(let text):
                rawData = text.data(using: .utf8)
            case .data(let data):
                rawData = data
            @unknown default:
                rawData = nil
            }
            guard let rawData,
                  let dict = (try? JSONSerialization.jsonObject(with: rawData)) as? [String: Any] else {
                continue
            }
            if let responseId = dict["id"] as? Int, responseId == commandId {
                if let errorObj = dict["error"] {
                    let errData = try? JSONSerialization.data(withJSONObject: errorObj)
                    let errText = errData.flatMap { String(data: $0, encoding: .utf8) } ?? "\(errorObj)"
                    throw BrowserEngineError.cdpError("\(method): \(errText)")
                }
                return dict
            }
        }
        throw BrowserEngineError.cdpError("DevTools \(method) 超时")
    }

    func startKeepAliveDrain() {
        DispatchQueue.global(qos: .utility).async { [weak self] in
            self?.drainLoop()
        }
    }

    private func drainLoop() {
        while true {
            lock.lock()
            let closed = isClosed
            lock.unlock()
            if closed { break }

            let sem = DispatchSemaphore(value: 0)
            var failed = false
            task.receive { result in
                if case .failure = result {
                    failed = true
                }
                sem.signal()
            }
            _ = sem.wait(timeout: .now() + 60)
            if failed { break }
        }
    }

    func close() {
        lock.lock()
        isClosed = true
        lock.unlock()
        task.cancel(with: .normalClosure, reason: nil)
        session.invalidateAndCancel()
    }
}

private final class IsolatedBrowserEngine: @unchecked Sendable {
    static let shared = IsolatedBrowserEngine()

    private let lock = NSLock()
    private var activeSessions: [String: CDPSession] = [:]

    private let countryLocales: [String: String] = [
        "AE": "ar-AE", "AR": "es-AR", "AU": "en-AU", "BR": "pt-BR", "CA": "en-CA",
        "CH": "de-CH", "CL": "es-CL", "CO": "es-CO", "DE": "de-DE", "ES": "es-ES",
        "FR": "fr-FR", "GB": "en-GB", "HK": "zh-HK", "ID": "id-ID", "IN": "en-IN",
        "IT": "it-IT", "JP": "ja-JP", "KR": "ko-KR", "MO": "zh-MO", "MX": "es-MX",
        "MY": "ms-MY", "NL": "nl-NL", "NZ": "en-NZ", "PH": "en-PH", "PL": "pl-PL",
        "PT": "pt-PT", "RU": "ru-RU", "SE": "sv-SE", "SG": "en-SG", "TH": "th-TH",
        "TR": "tr-TR", "TW": "zh-TW", "UA": "uk-UA", "US": "en-US", "VN": "vi-VN",
    ]

    private let countryTimezones: [String: String] = [
        "AE": "Asia/Dubai", "AR": "America/Argentina/Buenos_Aires", "AU": "Australia/Sydney",
        "BR": "America/Sao_Paulo", "CA": "America/Toronto", "CH": "Europe/Zurich",
        "CL": "America/Santiago", "CO": "America/Bogota", "DE": "Europe/Berlin",
        "ES": "Europe/Madrid", "FR": "Europe/Paris", "GB": "Europe/London",
        "HK": "Asia/Hong_Kong", "ID": "Asia/Jakarta", "IN": "Asia/Kolkata",
        "IT": "Europe/Rome", "JP": "Asia/Tokyo", "KR": "Asia/Seoul",
        "MO": "Asia/Macau", "MX": "America/Mexico_City", "MY": "Asia/Kuala_Lumpur",
        "NL": "Europe/Amsterdam", "NZ": "Pacific/Auckland", "PH": "Asia/Manila",
        "PL": "Europe/Warsaw", "PT": "Europe/Lisbon", "RU": "Europe/Moscow",
        "SE": "Europe/Stockholm", "SG": "Asia/Singapore", "TH": "Asia/Bangkok",
        "TR": "Europe/Istanbul", "TW": "Asia/Taipei", "UA": "Europe/Kyiv",
        "US": "America/New_York", "VN": "Asia/Ho_Chi_Minh",
    ]

    private let viewports: [(Int, Int, Double)] = [
        (1280, 720, 1.0),
        (1365, 768, 1.0),
        (1440, 900, 1.0),
        (1536, 864, 1.25),
        (1600, 900, 1.0),
        (1920, 1080, 1.0),
    ]

    private let localhostSession: URLSession = {
        let config = URLSessionConfiguration.ephemeral
        config.connectionProxyDictionary = [:]
        config.timeoutIntervalForRequest = 3.0
        config.timeoutIntervalForResource = 5.0
        return URLSession(configuration: config)
    }()

    private var profilesRootDir: URL {
        let appSupport = FileManager.default.urls(for: .applicationSupportDirectory, in: .userDomainMask).first
            ?? FileManager.default.homeDirectoryForCurrentUser.appendingPathComponent("Library/Application Support")
        return appSupport
            .appendingPathComponent("IsolatedBrowser", isDirectory: true)
            .appendingPathComponent("Profiles", isDirectory: true)
    }

    private func metaFileURL(for profileDir: URL) -> URL {
        profileDir.appendingPathComponent(".stable-browser-meta.json")
    }

    private func readMeta(profileDir: URL) -> ProfileMeta? {
        guard let data = try? Data(contentsOf: metaFileURL(for: profileDir)) else { return nil }
        return try? JSONDecoder().decode(ProfileMeta.self, from: data)
    }

    private func writeMeta(_ meta: ProfileMeta, to profileDir: URL) {
        try? FileManager.default.createDirectory(at: profileDir, withIntermediateDirectories: true)
        let encoder = JSONEncoder()
        encoder.outputFormatting = [.prettyPrinted, .sortedKeys]
        if let data = try? encoder.encode(meta) {
            try? data.write(to: metaFileURL(for: profileDir), options: .atomic)
        }
    }

    private func currentTimestamp() -> String {
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.dateFormat = "yyyy-MM-dd'T'HH:mm:ss"
        return formatter.string(from: Date())
    }

    // MARK: - Launch

    func launchChrome(targetURL: String, proxyMode: ProxyMode, manualProxy: String) throws -> LaunchOutcome {
        let chromePath = try locateChromeExecutable()

        var selectedProxy = ""
        var useProxy = false
        var forceDirect = false

        switch proxyMode {
        case .system:
            if let sysProxy = SystemProxyResolver.currentProxyURL() {
                selectedProxy = sysProxy
                useProxy = true
            }
        case .manual:
            selectedProxy = try ProxyMode.normalizeManualProxy(manualProxy)
            useProxy = true
        case .direct:
            forceDirect = true
        }

        let profileId = "browser-\(Int(Date().timeIntervalSince1970))"
        let profileDir = profilesRootDir.appendingPathComponent(profileId, isDirectory: true)
        try FileManager.default.createDirectory(at: profileDir, withIntermediateDirectories: true)

        let exitInfo = detectProxyExit(proxy: selectedProxy, forceDirect: forceDirect)
        let environment = buildEnvironment(profileId: profileId, proxy: selectedProxy, exitInfo: exitInfo)
        syncChromeProfileLanguage(profileDir: profileDir, environment: environment)

        let port = try findFreePort()
        let chromeProxy = chromeProxyServerArg(proxy: selectedProxy)
        let args = buildChromeArguments(
            profileDir: profileDir,
            port: port,
            chromeProxy: chromeProxy,
            forceDirect: forceDirect,
            environment: environment
        )

        let process = Process()
        process.executableURL = URL(fileURLWithPath: chromePath)
        process.arguments = args
        var childEnv = ProcessInfo.processInfo.environment
        childEnv["TZ"] = environment.timezone
        process.environment = childEnv
        process.standardOutput = FileHandle.nullDevice
        process.standardError = FileHandle.nullDevice

        try process.run()
        let browserPid = Int(process.processIdentifier)

        do {
            try waitForDevTools(port: port, timeout: 20.0)
        } catch {
            process.terminate()
            throw error
        }

        let pages = (try? fetchJSON(url: URL(string: "http://127.0.0.1:\(port)/json/list")!)) as? [[String: Any]] ?? []
        var target = pickPageTarget(pages: pages, preferredURL: "about:blank")
        if target == nil {
            target = createNewPage(port: port)
        }
        guard let page = target,
              let targetId = page["id"] as? String,
              let wsString = page["webSocketDebuggerUrl"] as? String,
              let wsURL = URL(string: wsString) else {
            throw BrowserEngineError.pageTargetMissing
        }

        activatePage(port: port, targetId: targetId)

        let cdp = try CDPSession(websocketURL: wsURL)
        do {
            try applyBrowserEnvironment(cdp: cdp, environment: environment)
        } catch {
            cdp.close()
            throw error
        }

        lock.lock()
        activeSessions[profileId]?.close()
        activeSessions[profileId] = cdp
        lock.unlock()

        var meta = ProfileMeta(
            profileId: profileId,
            profileDir: profileDir.path,
            devtoolsPort: port,
            browserPid: browserPid,
            useProxy: useProxy,
            forceDirect: forceDirect,
            proxyUsed: selectedProxy,
            chromeProxyServer: chromeProxy,
            browserEnvironment: environment,
            updatedAt: currentTimestamp()
        )
        writeMeta(meta, to: profileDir)

        if targetURL != "about:blank" {
            let navResult = try cdp.sendCommand("Page.navigate", params: ["url": targetURL], timeout: 20.0)
            if let resultBody = navResult["result"] as? [String: Any],
               let errorText = resultBody["errorText"] as? String,
               !errorText.isEmpty {
                cdp.startKeepAliveDrain()
                throw BrowserEngineError.cdpError("页面导航失败：\(errorText)")
            }
        }
        cdp.startKeepAliveDrain()

        meta.updatedAt = currentTimestamp()
        writeMeta(meta, to: profileDir)

        let proxyLabel: String
        if !selectedProxy.isEmpty {
            proxyLabel = selectedProxy
        } else if forceDirect {
            proxyLabel = "直连（绕过系统代理）"
        } else {
            proxyLabel = "默认网络（无系统代理）"
        }

        let region = [environment.exitCountry, environment.exitCity]
            .map { $0.trimmingCharacters(in: .whitespacesAndNewlines) }
            .filter { !$0.isEmpty }
            .joined(separator: " / ")
        let envParts = [region, environment.locale, "\(environment.viewportWidth)×\(environment.viewportHeight)"]
            .filter { !$0.isEmpty }

        return LaunchOutcome(
            profileId: profileId,
            devtoolsPort: port,
            proxyLabel: proxyLabel,
            timeSummary: Self.formatProxyLocalTime(timezoneId: environment.timezone),
            environmentSummary: envParts.joined(separator: " · ")
        )
    }

    // MARK: - List & Close Running Chrome

    func listActiveInstances() -> [ActiveInstance] {
        let fm = FileManager.default
        let root = profilesRootDir
        guard fm.fileExists(atPath: root.path),
              let entries = try? fm.contentsOfDirectory(at: root, includingPropertiesForKeys: [.isDirectoryKey]) else {
            return []
        }

        let activeCommands = activeChromeCommands()
        var result: [ActiveInstance] = []

        for dir in entries {
            guard (try? dir.resourceValues(forKeys: [.isDirectoryKey]).isDirectory) == true else { continue }
            let meta = readMeta(profileDir: dir)
            let profileId = meta?.profileId ?? dir.lastPathComponent
            var port = meta?.devtoolsPort ?? 0
            var pid = meta?.browserPid ?? 0
            var matchedCmd: (pid: Int, command: String)?

            if port > 0, let entry = activeCommands[port], commandMatchesProfile(entry.command, profileDir: dir, profileName: dir.lastPathComponent) {
                matchedCmd = entry
            } else {
                for (candidatePort, entry) in activeCommands {
                    if commandMatchesProfile(entry.command, profileDir: dir, profileName: dir.lastPathComponent) {
                        port = candidatePort
                        matchedCmd = entry
                        break
                    }
                }
            }

            guard let matched = matchedCmd else { continue }
            if pid <= 0 { pid = matched.pid }

            var proxyUsed = meta?.proxyUsed ?? ""
            var useProxy = meta?.useProxy ?? false
            var forceDirect = meta?.forceDirect ?? false
            if proxyUsed.isEmpty {
                if let extracted = firstRegexMatch(in: matched.command, pattern: "--proxy-server=([^\\s]+)") {
                    proxyUsed = extracted
                    useProxy = true
                } else if matched.command.contains("--no-proxy-server") {
                    forceDirect = true
                }
            }

            let env = meta?.browserEnvironment
            let locale = env?.locale ?? firstRegexMatch(in: matched.command, pattern: "--lang=([^\\s]+)") ?? ""

            result.append(ActiveInstance(
                profileId: profileId,
                devtoolsPort: port,
                browserPid: pid,
                proxyUsed: proxyUsed,
                useProxy: useProxy,
                forceDirect: forceDirect,
                exitCountry: env?.exitCountry ?? "",
                exitCity: env?.exitCity ?? "",
                locale: locale,
                timezone: env?.timezone ?? "",
                updatedAt: meta?.updatedAt ?? ""
            ))
        }

        result.sort { $0.updatedAt > $1.updatedAt }
        return result
    }

    func closeInstance(profileId: String) throws {
        let profileDir = profilesRootDir.appendingPathComponent(profileId, isDirectory: true)
        var meta = readMeta(profileDir: profileDir)
        var port = meta?.devtoolsPort ?? 0
        var pid = meta?.browserPid ?? 0

        if port <= 0 || !isDevToolsAlive(port: port) {
            let activeCommands = activeChromeCommands()
            for (candidatePort, entry) in activeCommands {
                if commandMatchesProfile(entry.command, profileDir: profileDir, profileName: profileId) {
                    port = candidatePort
                    if pid <= 0 { pid = entry.pid }
                    break
                }
            }
        }

        lock.lock()
        let heldSession = activeSessions.removeValue(forKey: profileId)
        lock.unlock()
        heldSession?.close()

        var lastError = ""
        if port > 0 && isDevToolsAlive(port: port) {
            do {
                if let version = (try? fetchJSON(url: URL(string: "http://127.0.0.1:\(port)/json/version")!)) as? [String: Any],
                   let wsString = version["webSocketDebuggerUrl"] as? String,
                   let wsURL = URL(string: wsString) {
                    let closer = try CDPSession(websocketURL: wsURL)
                    defer { closer.close() }
                    _ = try closer.sendCommand("Browser.close", params: [:], timeout: 3.0)
                }
            } catch {
                lastError = error.localizedDescription
            }
        }

        var closed = waitUntilDevToolsClosed(port: port, timeout: 6.0)
        if !closed && pid > 0 {
            _ = Darwin.kill(pid_t(pid), SIGTERM)
            closed = waitUntilDevToolsClosed(port: port, timeout: 4.0)
        }

        if !closed {
            throw BrowserEngineError.closeFailed(lastError.isEmpty ? "Chrome 进程未能关闭（端口 \(port)）。" : lastError)
        }

        if var updated = meta {
            updated.devtoolsPort = 0
            updated.browserPid = 0
            updated.updatedAt = currentTimestamp()
            writeMeta(updated, to: profileDir)
            meta = updated
        }
    }

    // MARK: - Exit Detection & Environment Construction

    private func detectProxyExit(proxy: String, forceDirect: Bool) -> ProxyExitInfo {
        if let ipinfoText = curlGet(url: "https://ipinfo.io/json", proxy: proxy, forceDirect: forceDirect, timeout: 6),
           let data = ipinfoText.data(using: .utf8),
           let json = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any],
           let rawCountry = json["country"] as? String {
            let country = rawCountry.trimmingCharacters(in: .whitespacesAndNewlines).uppercased()
            if country.range(of: "^[A-Z]{2}$", options: .regularExpression) != nil {
                return ProxyExitInfo(
                    ip: (json["ip"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines),
                    country: country,
                    region: (json["region"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines),
                    city: (json["city"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines),
                    timezone: (json["timezone"] as? String ?? "").trimmingCharacters(in: .whitespacesAndNewlines),
                    source: "ipinfo"
                )
            }
        }

        if let cfText = curlGet(url: "https://www.cloudflare.com/cdn-cgi/trace", proxy: proxy, forceDirect: forceDirect, timeout: 6) {
            var trace: [String: String] = [:]
            for line in cfText.components(separatedBy: .newlines) {
                let parts = line.split(separator: "=", maxSplits: 1)
                if parts.count == 2 {
                    trace[String(parts[0]).trimmingCharacters(in: .whitespaces)] = String(parts[1]).trimmingCharacters(in: .whitespaces)
                }
            }
            let country = (trace["loc"] ?? "").uppercased()
            if country.range(of: "^[A-Z]{2}$", options: .regularExpression) != nil {
                return ProxyExitInfo(
                    ip: trace["ip"] ?? "",
                    country: country,
                    region: "",
                    city: "",
                    timezone: "",
                    source: "cloudflare"
                )
            }
        }

        return ProxyExitInfo(
            ip: "",
            country: countryHintFromProxy(proxy),
            region: "",
            city: "",
            timezone: "",
            source: "fallback"
        )
    }

    private func curlGet(url: String, proxy: String, forceDirect: Bool, timeout: Int) -> String? {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/usr/bin/curl")
        var args = [
            "--silent",
            "--show-error",
            "--location",
            "--max-time",
            String(max(1, timeout)),
        ]
        if !proxy.isEmpty {
            let curlProxy = proxy.hasPrefix("socks5://")
                ? "socks5h://" + proxy.dropFirst("socks5://".count)
                : proxy
            args += ["--proxy", curlProxy]
        } else if forceDirect {
            args += ["--noproxy", "*"]
        }
        args.append(url)
        process.arguments = args

        let stdoutPipe = Pipe()
        process.standardOutput = stdoutPipe
        process.standardError = FileHandle.nullDevice

        do {
            try process.run()
            let data = stdoutPipe.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()
            guard process.terminationStatus == 0 else { return nil }
            return String(data: data, encoding: .utf8)
        } catch {
            return nil
        }
    }

    private func countryHintFromProxy(_ proxy: String) -> String {
        if let match = firstRegexMatch(in: proxy, pattern: "(?:region[-_=]|country[-_=])([a-z]{2})(?:\\b|[-_])", caseInsensitive: true) {
            return match.uppercased()
        }
        if let match = firstRegexMatch(in: proxy, pattern: "(?:^|[.:/@_-])(jp|us|sg|kr|tw|gb|de|fr|hk)(?:[.:/_-]|$)", caseInsensitive: true) {
            return match.uppercased()
        }
        return "US"
    }

    private func buildEnvironment(profileId: String, proxy: String, exitInfo: ProxyExitInfo) -> BrowserEnvironment {
        let country = exitInfo.country.isEmpty ? "US" : exitInfo.country.uppercased()
        let locale = countryLocales[country] ?? "en-US"
        let baseLang = locale.split(separator: "-", maxSplits: 1).first.map(String.init) ?? locale
        let languages = baseLang == locale ? [locale] : [locale, baseLang]
        let acceptLanguage = languages.enumerated().map { idx, lang in
            if idx == 0 { return lang }
            let q = max(0.5, 0.9 - Double(idx) * 0.1)
            return String(format: "%@;q=%.1f", lang, q)
        }.joined(separator: ",")

        let timezone = !exitInfo.timezone.isEmpty
            ? exitInfo.timezone
            : (countryTimezones[country] ?? "UTC")

        let proxyKey = sha256Hex(proxy.trimmingCharacters(in: .whitespacesAndNewlines)).prefix(20)
        let seedBytes = sha256Bytes("browser-environment-v1|\(profileId.lowercased())|\(proxyKey)|\(country)")

        let (width, height, scale) = viewports[Int(seedBytes[0]) % viewports.count]
        let coresChoices = [4, 8, 8, 12, 16]
        let memChoices = [4, 8, 8, 16]
        let hardwareConcurrency = coresChoices[Int(seedBytes[1]) % coresChoices.count]
        let deviceMemory = memChoices[Int(seedBytes[2]) % memChoices.count]

        return BrowserEnvironment(
            version: 1,
            proxyKey: String(proxyKey),
            exitIP: exitInfo.ip,
            exitCountry: country,
            exitRegion: exitInfo.region,
            exitCity: exitInfo.city,
            exitSource: exitInfo.source,
            locale: locale,
            languages: languages,
            acceptLanguage: acceptLanguage,
            timezone: timezone,
            viewportWidth: width,
            viewportHeight: height,
            screenWidth: width,
            screenHeight: height,
            deviceScaleFactor: scale,
            hardwareConcurrency: hardwareConcurrency,
            deviceMemory: deviceMemory
        )
    }

    // MARK: - Chrome Configuration & CDP Injection

    private func locateChromeExecutable() throws -> String {
        let fm = FileManager.default
        let candidates = [
            "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
            fm.homeDirectoryForCurrentUser.appendingPathComponent("Applications/Google Chrome.app/Contents/MacOS/Google Chrome").path,
        ]
        for path in candidates where fm.isExecutableFile(atPath: path) {
            return path
        }
        if let appURL = NSWorkspace.shared.urlForApplication(withBundleIdentifier: "com.google.Chrome"),
           let execURL = Bundle(url: appURL)?.executableURL,
           fm.isExecutableFile(atPath: execURL.path) {
            return execURL.path
        }
        throw BrowserEngineError.chromeNotFound
    }

    private func chromeProxyServerArg(proxy: String) -> String {
        guard !proxy.isEmpty else { return "" }
        if proxy.hasPrefix("socks5h://") {
            return "socks5://" + proxy.dropFirst("socks5h://".count)
        }
        return proxy
    }

    private func buildChromeArguments(
        profileDir: URL,
        port: Int,
        chromeProxy: String,
        forceDirect: Bool,
        environment: BrowserEnvironment
    ) -> [String] {
        var args = [
            "--user-data-dir=\(profileDir.path)",
            "--remote-debugging-address=127.0.0.1",
            "--remote-debugging-port=\(port)",
            "--no-first-run",
            "--no-default-browser-check",
            "--new-window",
            "--lang=\(environment.locale)",
            "--accept-lang=\(environment.languages.joined(separator: ","))",
            "--window-size=\(environment.viewportWidth),\(environment.viewportHeight)",
            "-AppleLanguages",
            "(\(environment.locale))",
            "-AppleLocale",
            environment.locale.replacingOccurrences(of: "-", with: "_"),
        ]
        if !chromeProxy.isEmpty {
            args.append("--proxy-server=\(chromeProxy)")
        } else if forceDirect {
            args.append("--no-proxy-server")
        }
        args.append("about:blank")
        return args
    }

    private func syncChromeProfileLanguage(profileDir: URL, environment: BrowserEnvironment) {
        let languagesCSV = environment.languages.joined(separator: ",")
        let defaultDir = profileDir.appendingPathComponent("Default", isDirectory: true)
        try? FileManager.default.createDirectory(at: defaultDir, withIntermediateDirectories: true)

        updateJSONFile(
            at: defaultDir.appendingPathComponent("Preferences"),
            section: "intl",
            values: [
                "accept_languages": languagesCSV,
                "selected_languages": languagesCSV,
            ]
        )
        updateJSONFile(
            at: profileDir.appendingPathComponent("Local State"),
            section: "intl",
            values: ["app_locale": environment.locale]
        )
    }

    private func updateJSONFile(at url: URL, section: String, values: [String: Any]) {
        var root: [String: Any] = [:]
        if let data = try? Data(contentsOf: url),
           let parsed = (try? JSONSerialization.jsonObject(with: data)) as? [String: Any] {
            root = parsed
        }
        var current = root[section] as? [String: Any] ?? [:]
        for (k, v) in values {
            current[k] = v
        }
        root[section] = current
        if let data = try? JSONSerialization.data(withJSONObject: root, options: []) {
            try? data.write(to: url, options: .atomic)
        }
    }

    private func applyBrowserEnvironment(cdp: CDPSession, environment: BrowserEnvironment) throws {
        let script = buildInjectionScript(environment: environment)
        let commands: [(String, [String: Any])] = [
            ("Page.enable", [:]),
            ("Network.enable", [:]),
            ("Emulation.setTimezoneOverride", ["timezoneId": environment.timezone]),
            ("Emulation.setLocaleOverride", ["locale": environment.locale]),
            ("Emulation.setDeviceMetricsOverride", [
                "width": environment.viewportWidth,
                "height": environment.viewportHeight,
                "deviceScaleFactor": environment.deviceScaleFactor,
                "mobile": false,
                "screenWidth": environment.screenWidth,
                "screenHeight": environment.screenHeight,
            ]),
            ("Network.setExtraHTTPHeaders", ["headers": ["Accept-Language": environment.acceptLanguage]]),
            ("Page.addScriptToEvaluateOnNewDocument", ["source": script]),
            ("Runtime.evaluate", [
                "expression": script,
                "awaitPromise": true,
                "returnByValue": true,
            ]),
        ]
        for (method, params) in commands {
            _ = try cdp.sendCommand(method, params: params, timeout: 8.0)
        }
    }

    private func buildInjectionScript(environment: BrowserEnvironment) -> String {
        let payloadObj: [String: Any] = [
            "languages": environment.languages,
            "hardwareConcurrency": environment.hardwareConcurrency,
            "deviceMemory": environment.deviceMemory,
            "screenWidth": environment.screenWidth,
            "screenHeight": environment.screenHeight,
        ]
        let payloadData = (try? JSONSerialization.data(withJSONObject: payloadObj)) ?? Data("{}".utf8)
        let payloadJSON = String(data: payloadData, encoding: .utf8) ?? "{}"
        return """
        (() => {
          const fp = \(payloadJSON);
          const defineGetter = (target, name, value) => {
            try { Object.defineProperty(target, name, { get: () => value, configurable: true }); } catch (_) {}
          };
          defineGetter(Navigator.prototype, 'webdriver', undefined);
          defineGetter(Navigator.prototype, 'language', fp.languages[0]);
          defineGetter(Navigator.prototype, 'languages', fp.languages);
          defineGetter(Navigator.prototype, 'hardwareConcurrency', fp.hardwareConcurrency);
          defineGetter(Navigator.prototype, 'deviceMemory', fp.deviceMemory);
          defineGetter(Screen.prototype, 'width', fp.screenWidth);
          defineGetter(Screen.prototype, 'height', fp.screenHeight);
          defineGetter(Screen.prototype, 'availWidth', fp.screenWidth);
          defineGetter(Screen.prototype, 'availHeight', fp.screenHeight - 40);
        })();
        """
    }

    // MARK: - DevTools HTTP & Process Helpers

    private func findFreePort() throws -> Int {
        let fd = Darwin.socket(AF_INET, SOCK_STREAM, 0)
        guard fd >= 0 else { throw BrowserEngineError.portAllocationFailed }
        defer { Darwin.close(fd) }

        var addr = sockaddr_in()
        addr.sin_len = UInt8(MemoryLayout<sockaddr_in>.size)
        addr.sin_family = sa_family_t(AF_INET)
        addr.sin_port = 0
        addr.sin_addr = in_addr(s_addr: inet_addr("127.0.0.1"))

        let bindResult = withUnsafePointer(to: &addr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.bind(fd, $0, socklen_t(MemoryLayout<sockaddr_in>.size))
            }
        }
        guard bindResult == 0 else { throw BrowserEngineError.portAllocationFailed }

        var boundAddr = sockaddr_in()
        var len = socklen_t(MemoryLayout<sockaddr_in>.size)
        let nameResult = withUnsafeMutablePointer(to: &boundAddr) {
            $0.withMemoryRebound(to: sockaddr.self, capacity: 1) {
                Darwin.getsockname(fd, $0, &len)
            }
        }
        guard nameResult == 0 else { throw BrowserEngineError.portAllocationFailed }
        return Int(UInt16(bigEndian: boundAddr.sin_port))
    }

    private func fetchJSON(url: URL) throws -> Any {
        let sem = DispatchSemaphore(value: 0)
        var resultData: Data?
        var resultErr: Error?
        let task = localhostSession.dataTask(with: url) { data, response, error in
            if let error {
                resultErr = error
            } else if let http = response as? HTTPURLResponse, !(200..<300).contains(http.statusCode) {
                resultErr = NSError(domain: "HTTP", code: http.statusCode)
            } else {
                resultData = data
            }
            sem.signal()
        }
        task.resume()
        if sem.wait(timeout: .now() + 3.0) == .timedOut {
            task.cancel()
            throw NSError(domain: "Timeout", code: -1)
        }
        if let resultErr { throw resultErr }
        guard let resultData else { return [:] }
        return try JSONSerialization.jsonObject(with: resultData)
    }

    private func isDevToolsAlive(port: Int) -> Bool {
        guard port > 0, let url = URL(string: "http://127.0.0.1:\(port)/json/version") else { return false }
        return (try? fetchJSON(url: url)) != nil
    }

    private func waitForDevTools(port: Int, timeout: TimeInterval) throws {
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if isDevToolsAlive(port: port) {
                return
            }
            Thread.sleep(forTimeInterval: 0.2)
        }
        throw BrowserEngineError.devtoolsTimeout(port)
    }

    private func waitUntilDevToolsClosed(port: Int, timeout: TimeInterval) -> Bool {
        guard port > 0 else { return true }
        let deadline = Date().addingTimeInterval(timeout)
        while Date() < deadline {
            if !isDevToolsAlive(port: port) {
                return true
            }
            Thread.sleep(forTimeInterval: 0.25)
        }
        return !isDevToolsAlive(port: port)
    }

    private func pickPageTarget(pages: [[String: Any]], preferredURL: String) -> [String: Any]? {
        let valid = pages.filter { page in
            guard let type = page["type"] as? String, type == "page",
                  let ws = page["webSocketDebuggerUrl"] as? String, !ws.isEmpty else {
                return false
            }
            let url = page["url"] as? String ?? ""
            return !url.hasPrefix("chrome-extension://") && !url.hasPrefix("devtools://") && !url.hasPrefix("chrome://")
        }
        if let exact = valid.first(where: { ($0["url"] as? String) == preferredURL }) {
            return exact
        }
        return valid.first
    }

    private func createNewPage(port: Int) -> [String: Any]? {
        guard let url = URL(string: "http://127.0.0.1:\(port)/json/new?about:blank") else { return nil }
        var req = URLRequest(url: url)
        req.httpMethod = "PUT"
        let sem = DispatchSemaphore(value: 0)
        var dataOut: Data?
        localhostSession.dataTask(with: req) { data, _, _ in
            dataOut = data
            sem.signal()
        }.resume()
        _ = sem.wait(timeout: .now() + 3.0)
        guard let dataOut else { return nil }
        return (try? JSONSerialization.jsonObject(with: dataOut)) as? [String: Any]
    }

    private func activatePage(port: Int, targetId: String) {
        guard let url = URL(string: "http://127.0.0.1:\(port)/json/activate/\(targetId)") else { return }
        _ = try? fetchJSON(url: url)
    }

    private func activeChromeCommands() -> [Int: (pid: Int, command: String)] {
        let process = Process()
        process.executableURL = URL(fileURLWithPath: "/bin/ps")
        process.arguments = ["-axo", "pid=,command="]
        var env = ProcessInfo.processInfo.environment
        env["LANG"] = "en_US.UTF-8"
        env["LC_ALL"] = "en_US.UTF-8"
        process.environment = env

        let pipe = Pipe()
        process.standardOutput = pipe
        process.standardError = FileHandle.nullDevice

        do {
            try process.run()
            let data = pipe.fileHandleForReading.readDataToEndOfFile()
            process.waitUntilExit()
            guard let text = String(data: data, encoding: .utf8) else { return [:] }
            var map: [Int: (pid: Int, command: String)] = [:]
            for rawLine in text.components(separatedBy: .newlines) {
                let line = rawLine.trimmingCharacters(in: .whitespaces)
                guard !line.isEmpty else { continue }
                let parts = line.split(maxSplits: 1, omittingEmptySubsequences: true, whereSeparator: { $0 == " " || $0 == "\t" })
                guard parts.count == 2, let pid = Int(parts[0]) else { continue }
                let command = String(parts[1])
                guard let portStr = firstRegexMatch(in: command, pattern: "(?:^|\\s)--remote-debugging-port(?:=|\\s+)(\\d+)(?:\\s|$)"),
                      let port = Int(portStr) else {
                    continue
                }
                let isHelper = command.contains("--type=")
                if map[port] == nil || (!isHelper && map[port]!.command.contains("--type=")) {
                    map[port] = (pid, command)
                }
            }
            return map
        } catch {
            return [:]
        }
    }

    private func commandMatchesProfile(_ command: String, profileDir: URL, profileName: String) -> Bool {
        if command.contains(profileDir.path) {
            return true
        }
        let escapedName = NSRegularExpression.escapedPattern(for: profileName)
        return command.range(of: "--user-data-dir=[^\\s]*[/\\\\]\(escapedName)(?=\\s|$)", options: .regularExpression) != nil
    }

    private func firstRegexMatch(in text: String, pattern: String, caseInsensitive: Bool = false) -> String? {
        let options: NSRegularExpression.Options = caseInsensitive ? [.caseInsensitive] : []
        guard let regex = try? NSRegularExpression(pattern: pattern, options: options) else { return nil }
        let range = NSRange(text.startIndex..<text.endIndex, in: text)
        guard let match = regex.firstMatch(in: text, options: [], range: range),
              match.numberOfRanges >= 2,
              let captureRange = Range(match.range(at: 1), in: text) else {
            return nil
        }
        return String(text[captureRange])
    }

    private func sha256Bytes(_ input: String) -> [UInt8] {
        let digest = SHA256.hash(data: Data(input.utf8))
        return Array(digest)
    }

    private func sha256Hex(_ input: String) -> String {
        sha256Bytes(input).map { String(format: "%02x", $0) }.joined()
    }

    static func formatShortTime(timezoneId: String) -> String {
        let tzId = timezoneId.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !tzId.isEmpty, let tz = TimeZone(identifier: tzId) else { return "" }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = tz
        formatter.dateFormat = "MM-dd HH:mm"
        return formatter.string(from: Date())
    }

    static func formatProxyLocalTime(timezoneId: String) -> String {
        let tzId = timezoneId.trimmingCharacters(in: .whitespacesAndNewlines)
        guard !tzId.isEmpty, let tz = TimeZone(identifier: tzId) else { return "" }
        let formatter = DateFormatter()
        formatter.locale = Locale(identifier: "en_US_POSIX")
        formatter.timeZone = tz
        formatter.dateFormat = "yyyy-MM-dd HH:mm:ss"
        let seconds = tz.secondsFromGMT(for: Date())
        let sign = seconds >= 0 ? "+" : "-"
        let absSeconds = abs(seconds)
        let hours = absSeconds / 3600
        let mins = (absSeconds % 3600) / 60
        let gmtLabel = mins == 0
            ? String(format: "GMT%@%d", sign, hours)
            : String(format: "GMT%@%d:%02d", sign, hours, mins)
        return "\(formatter.string(from: Date())) (\(gmtLabel)) · \(tzId)"
    }
}

// MARK: - Theme Helpers

private func rgb(_ hex: UInt32, alpha: Double = 1.0) -> Color {
    Color(
        red: Double((hex >> 16) & 0xff) / 255.0,
        green: Double((hex >> 8) & 0xff) / 255.0,
        blue: Double(hex & 0xff) / 255.0,
        opacity: alpha
    )
}

private struct ContentHeightKey: PreferenceKey {
    static var defaultValue: CGFloat = 0
    static func reduce(value: inout CGFloat, nextValue: () -> CGFloat) {
        let next = nextValue()
        if next > 0 { value = next }
    }
}

// MARK: - Main Launcher View

private struct LauncherView: View {
    static let windowWidth: CGFloat = 512

    let onContentHeightChange: (CGFloat) -> Void

    @Environment(\.colorScheme) private var colorScheme
    @Namespace private var proxyTabAnimation

    private enum FocusTarget: Hashable {
        case url, proxy
    }
    @FocusState private var focusTarget: FocusTarget?

    @AppStorage("sb.targetUrl") private var address = ""
    @AppStorage("sb.standaloneProxyMode") private var storedProxyMode = ProxyMode.system.rawValue
    @AppStorage("sb.manualProxy") private var proxyAddress = ""

    @State private var isLaunching = false
    @State private var feedback: LaunchFeedback?
    @State private var copiedPort = false

    @State private var activeInstances: [ActiveInstance] = []
    @State private var isRefreshingInstances = false
    @State private var closingProfileIds: Set<String> = []

    private var proxyMode: ProxyMode {
        get { ProxyMode(rawValue: storedProxyMode) ?? .system }
        set { storedProxyMode = newValue.rawValue }
    }

    private var isDark: Bool { colorScheme == .dark }
    private var windowBg: Color { rgb(isDark ? 0x151918 : 0xF4F6F5) }
    private var headerBg: Color { rgb(isDark ? 0x1B201E : 0xFFFFFF) }
    private var headerDivider: Color { rgb(isDark ? 0x2A322E : 0xE5EAE7) }
    private var cardBg: Color { rgb(isDark ? 0x1D2320 : 0xFFFFFF) }
    private var cardBorder: Color { rgb(isDark ? 0x2C3531 : 0xE3E8E5) }
    private var fieldBg: Color { rgb(isDark ? 0x131715 : 0xF7F9F8) }
    private var fieldBorder: Color { rgb(isDark ? 0x313B36 : 0xDCE2DF) }

    private var accent: Color { rgb(isDark ? 0x1BB88E : 0x0F8E6E) }
    private var accentDeep: Color { rgb(isDark ? 0x12916F : 0x0B7057) }
    private var danger: Color { rgb(0xE5484D) }
    private var primaryInk: Color { rgb(isDark ? 0xEFF3F1 : 0x18201D) }
    private var secondaryInk: Color { rgb(isDark ? 0x95A49D : 0x62706A) }

    var body: some View {
        VStack(spacing: 0) {
            titleBar
            Rectangle().fill(headerDivider).frame(height: 1)

            VStack(spacing: 12) {
                urlCard
                proxyCard

                if let feedback {
                    feedbackBanner(feedback)
                        .transition(.opacity.combined(with: .move(edge: .top)))
                }

                launchButton
                activeInstancesCard
            }
            .padding(18)
        }
        .frame(width: Self.windowWidth)
        .fixedSize(horizontal: false, vertical: true)
        .background(windowBg)
        .background(
            GeometryReader { proxy in
                Color.clear.preference(key: ContentHeightKey.self, value: proxy.size.height)
            }
        )
        .onPreferenceChange(ContentHeightKey.self) { height in
            onContentHeightChange(height)
        }
        .foregroundStyle(primaryInk)
        .tint(accent)
        .onAppear {
            DispatchQueue.main.asyncAfter(deadline: .now() + 0.08) {
                focusTarget = .url
            }
            refreshActiveInstances()
        }
        .onReceive(NotificationCenter.default.publisher(for: NSApplication.didBecomeActiveNotification)) { _ in
            refreshActiveInstances()
        }
        .onReceive(NotificationCenter.default.publisher(for: .focusUrlField)) { _ in
            focusTarget = .url
        }
        .onReceive(NotificationCenter.default.publisher(for: .triggerLaunchChrome)) { _ in
            openChrome()
        }
    }

    // MARK: - Solid Title Bar

    private var titleBar: some View {
        ZStack {
            HStack(spacing: 8) {
                ZStack {
                    RoundedRectangle(cornerRadius: 6, style: .continuous)
                        .fill(
                            LinearGradient(
                                colors: [accent, accentDeep],
                                startPoint: .topLeading,
                                endPoint: .bottomTrailing
                            )
                        )
                    Image(systemName: "safari.fill")
                        .font(.system(size: 11, weight: .semibold))
                        .foregroundStyle(.white)
                }
                .frame(width: 20, height: 20)

                Text("隔离浏览器")
                    .font(.system(size: 13, weight: .semibold))
                    .foregroundStyle(primaryInk)
            }

            HStack {
                Spacer()
                HStack(spacing: 5) {
                    Circle()
                        .fill(isLaunching ? Color.orange : (activeInstances.isEmpty ? secondaryInk.opacity(0.6) : accent))
                        .frame(width: 6, height: 6)
                    Text(isLaunching ? "启动中" : (activeInstances.isEmpty ? "就绪" : "运行 \(activeInstances.count)"))
                        .font(.system(size: 10.5, weight: .medium))
                        .foregroundStyle(secondaryInk)
                }
                .padding(.horizontal, 8)
                .padding(.vertical, 3.5)
                .background(
                    Capsule(style: .continuous)
                        .fill(isDark ? Color.white.opacity(0.06) : Color.black.opacity(0.045))
                )
            }
            .padding(.trailing, 16)
        }
        .frame(height: 44)
        .background(headerBg)
    }

    // MARK: - URL Card

    private var urlCard: some View {
        VStack(alignment: .leading, spacing: 9) {
            HStack {
                Label("目标页面", systemImage: "safari")
                    .font(.system(size: 12.5, weight: .semibold))
                    .foregroundStyle(primaryInk)

                Spacer()

                Text("留空则打开空白页")
                    .font(.system(size: 11))
                    .foregroundStyle(secondaryInk)
            }

            HStack(spacing: 10) {
                Image(systemName: "globe")
                    .font(.system(size: 14, weight: .medium))
                    .foregroundStyle(focusTarget == .url ? accent : secondaryInk)

                TextField("https://example.com", text: $address)
                    .font(.system(size: 13.5))
                    .foregroundStyle(primaryInk)
                    .textFieldStyle(.plain)
                    .focused($focusTarget, equals: .url)
                    .onSubmit(openChrome)

                if !address.isEmpty {
                    Button {
                        address = ""
                        focusTarget = .url
                    } label: {
                        Image(systemName: "xmark.circle.fill")
                            .font(.system(size: 13))
                            .foregroundStyle(secondaryInk.opacity(0.75))
                    }
                    .buttonStyle(.plain)
                    .help("清空地址")
                }
            }
            .padding(.horizontal, 12)
            .frame(height: 40)
            .background(fieldBg, in: RoundedRectangle(cornerRadius: 9, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 9, style: .continuous)
                    .strokeBorder(focusTarget == .url ? accent : fieldBorder, lineWidth: focusTarget == .url ? 1.5 : 1)
                    .allowsHitTesting(false)
            }
        }
        .padding(14)
        .background(cardBg, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .strokeBorder(cardBorder, lineWidth: 1)
                .allowsHitTesting(false)
        }
        .shadow(color: Color.black.opacity(isDark ? 0.2 : 0.03), radius: 4, y: 1)
    }

    // MARK: - Proxy Card

    private var proxyCard: some View {
        VStack(alignment: .leading, spacing: 10) {
            Label("网络出口", systemImage: "network")
                .font(.system(size: 12.5, weight: .semibold))
                .foregroundStyle(primaryInk)

            HStack(spacing: 4) {
                ForEach(ProxyMode.allCases) { mode in
                    let isSelected = proxyMode == mode
                    Button {
                        withAnimation(.spring(response: 0.24, dampingFraction: 0.86)) {
                            storedProxyMode = mode.rawValue
                            feedback = nil
                        }
                        if mode == .manual {
                            DispatchQueue.main.asyncAfter(deadline: .now() + 0.06) {
                                focusTarget = .proxy
                            }
                        }
                    } label: {
                        HStack(spacing: 6) {
                            Image(systemName: mode.symbolName)
                                .font(.system(size: 11.5, weight: .medium))
                            Text(mode.rawValue)
                                .font(.system(size: 12, weight: isSelected ? .semibold : .medium))
                        }
                        .foregroundStyle(isSelected ? primaryInk : secondaryInk)
                        .frame(maxWidth: .infinity)
                        .frame(height: 30)
                        .background {
                            if isSelected {
                                RoundedRectangle(cornerRadius: 7, style: .continuous)
                                    .fill(isDark ? Color.white.opacity(0.12) : Color.white)
                                    .shadow(color: Color.black.opacity(isDark ? 0.25 : 0.08), radius: 2, y: 1)
                                    .matchedGeometryEffect(id: "ProxySegment", in: proxyTabAnimation)
                            }
                        }
                        .contentShape(Rectangle())
                    }
                    .buttonStyle(.plain)
                }
            }
            .padding(3)
            .background(fieldBg, in: RoundedRectangle(cornerRadius: 9, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 9, style: .continuous)
                    .strokeBorder(fieldBorder, lineWidth: 1)
                    .allowsHitTesting(false)
            }

            if proxyMode == .manual {
                HStack(spacing: 8) {
                    Image(systemName: "arrow.triangle.branch")
                        .font(.system(size: 12.5, weight: .medium))
                        .foregroundStyle(focusTarget == .proxy ? accent : secondaryInk)

                    TextField("socks5://127.0.0.1:1080 或 http://127.0.0.1:7890", text: $proxyAddress)
                        .font(.system(size: 12.5, design: .monospaced))
                        .foregroundStyle(primaryInk)
                        .textFieldStyle(.plain)
                        .focused($focusTarget, equals: .proxy)
                        .onSubmit(openChrome)

                    if !proxyAddress.isEmpty {
                        manualProxyValidityBadge
                    }
                }
                .padding(.horizontal, 11)
                .frame(height: 36)
                .background(fieldBg, in: RoundedRectangle(cornerRadius: 8, style: .continuous))
                .overlay {
                    RoundedRectangle(cornerRadius: 8, style: .continuous)
                        .strokeBorder(focusTarget == .proxy ? accent : fieldBorder, lineWidth: focusTarget == .proxy ? 1.5 : 1)
                        .allowsHitTesting(false)
                }
                .transition(.opacity.combined(with: .move(edge: .top)))
            }

            Text(proxyMode.hint)
                .font(.system(size: 11))
                .foregroundStyle(secondaryInk)
                .fixedSize(horizontal: false, vertical: true)
        }
        .padding(14)
        .background(cardBg, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .strokeBorder(cardBorder, lineWidth: 1)
                .allowsHitTesting(false)
        }
        .shadow(color: Color.black.opacity(isDark ? 0.2 : 0.03), radius: 4, y: 1)
        .disabled(isLaunching)
    }

    @ViewBuilder
    private var manualProxyValidityBadge: some View {
        let valid = (try? ProxyMode.normalizeManualProxy(proxyAddress)) != nil
        Image(systemName: valid ? "checkmark.circle.fill" : "exclamationmark.triangle.fill")
            .font(.system(size: 12))
            .foregroundStyle(valid ? accent : Color.orange)
    }

    // MARK: - Status Banner

    @ViewBuilder
    private func feedbackBanner(_ item: LaunchFeedback) -> some View {
        switch item {
        case .success(let outcome):
            VStack(alignment: .leading, spacing: 7) {
                HStack(spacing: 8) {
                    Image(systemName: "checkmark.circle.fill")
                        .font(.system(size: 14))
                        .foregroundStyle(accent)
                    Text("Chrome 浏览器环境已启动")
                        .font(.system(size: 12.5, weight: .semibold))
                        .foregroundStyle(primaryInk)

                    Spacer()

                    if outcome.devtoolsPort > 0 {
                        Button {
                            NSPasteboard.general.clearContents()
                            NSPasteboard.general.setString("127.0.0.1:\(outcome.devtoolsPort)", forType: .string)
                            copiedPort = true
                            DispatchQueue.main.asyncAfter(deadline: .now() + 1.5) {
                                copiedPort = false
                            }
                        } label: {
                            HStack(spacing: 4) {
                                Image(systemName: copiedPort ? "checkmark" : "doc.on.doc")
                                    .font(.system(size: 10))
                                Text(copiedPort ? "已复制" : "端口 \(String(outcome.devtoolsPort))")
                                    .font(.system(size: 10.5, weight: .medium, design: .monospaced))
                            }
                            .foregroundStyle(accent)
                            .padding(.horizontal, 7)
                            .padding(.vertical, 3)
                            .background(accent.opacity(0.13), in: Capsule(style: .continuous))
                        }
                        .buttonStyle(.plain)
                    }

                    if !outcome.profileId.isEmpty {
                        closeProfileButton(profileId: outcome.profileId, title: "关闭此 Chrome")
                    }

                    Button {
                        withAnimation(.easeInOut(duration: 0.16)) { feedback = nil }
                    } label: {
                        Image(systemName: "xmark")
                            .font(.system(size: 10.5, weight: .semibold))
                            .foregroundStyle(secondaryInk)
                    }
                    .buttonStyle(.plain)
                }

                VStack(alignment: .leading, spacing: 4) {
                    metaRow(label: "代理", value: outcome.proxyLabel)
                    if !outcome.timeSummary.isEmpty {
                        metaRow(label: "时间", value: outcome.timeSummary)
                    }
                    if !outcome.environmentSummary.isEmpty {
                        metaRow(label: "环境", value: outcome.environmentSummary)
                    }
                }
            }
            .padding(12)
            .background(
                RoundedRectangle(cornerRadius: 11, style: .continuous)
                    .fill(accent.opacity(isDark ? 0.15 : 0.08))
            )
            .overlay {
                RoundedRectangle(cornerRadius: 11, style: .continuous)
                    .strokeBorder(accent.opacity(0.32), lineWidth: 1)
                    .allowsHitTesting(false)
            }

        case .failure(let message):
            HStack(alignment: .top, spacing: 9) {
                Image(systemName: "exclamationmark.octagon.fill")
                    .font(.system(size: 14))
                    .foregroundStyle(danger)
                    .padding(.top, 1)

                VStack(alignment: .leading, spacing: 2) {
                    Text("操作失败")
                        .font(.system(size: 12, weight: .semibold))
                        .foregroundStyle(primaryInk)
                    Text(message)
                        .font(.system(size: 11.5))
                        .foregroundStyle(secondaryInk)
                        .textSelection(.enabled)
                        .fixedSize(horizontal: false, vertical: true)
                }

                Spacer(minLength: 4)

                Button {
                    withAnimation(.easeInOut(duration: 0.16)) { feedback = nil }
                } label: {
                    Image(systemName: "xmark")
                        .font(.system(size: 10.5, weight: .semibold))
                        .foregroundStyle(secondaryInk)
                }
                .buttonStyle(.plain)
            }
            .padding(12)
            .background(
                RoundedRectangle(cornerRadius: 11, style: .continuous)
                    .fill(rgb(0xE5484D, alpha: isDark ? 0.15 : 0.09))
            )
            .overlay {
                RoundedRectangle(cornerRadius: 11, style: .continuous)
                    .strokeBorder(rgb(0xE5484D, alpha: 0.35), lineWidth: 1)
                    .allowsHitTesting(false)
            }
        }
    }

    private func metaRow(label: String, value: String) -> some View {
        HStack(alignment: .firstTextBaseline, spacing: 6) {
            Text(label)
                .font(.system(size: 10.5, weight: .semibold))
                .foregroundStyle(secondaryInk)
                .frame(width: 30, alignment: .leading)
            Text(value)
                .font(.system(size: 11, design: .monospaced))
                .foregroundStyle(primaryInk)
                .lineLimit(1)
                .truncationMode(.middle)
                .textSelection(.enabled)
        }
    }

    // MARK: - Launch CTA

    private var launchButton: some View {
        Button(action: openChrome) {
            HStack(spacing: 9) {
                if isLaunching {
                    ProgressView()
                        .controlSize(.small)
                        .colorScheme(.dark)
                        .scaleEffect(0.78)
                        .frame(width: 14, height: 14)
                    Text("正在构建环境并打开 Chrome…")
                        .font(.system(size: 13.5, weight: .semibold))
                } else {
                    Image(systemName: "arrow.up.right.circle.fill")
                        .font(.system(size: 14, weight: .semibold))
                    Text("打开 Chrome")
                        .font(.system(size: 13.5, weight: .semibold))
                }
            }
            .frame(maxWidth: .infinity)
            .frame(height: 42)
        }
        .buttonStyle(PrimaryLaunchButtonStyle(accent: accent, accentDeep: accentDeep))
        .keyboardShortcut(.defaultAction)
        .disabled(isLaunching)
    }

    // MARK: - Active Chrome Instances

    private var activeInstancesCard: some View {
        VStack(alignment: .leading, spacing: activeInstances.isEmpty ? 0 : 10) {
            HStack(spacing: 8) {
                Label("运行中的 Chrome", systemImage: "macwindow.on.rectangle")
                    .font(.system(size: 12, weight: .semibold))
                    .foregroundStyle(primaryInk)

                Text("\(activeInstances.count)")
                    .font(.system(size: 10.5, weight: .semibold, design: .monospaced))
                    .foregroundStyle(activeInstances.isEmpty ? secondaryInk : accent)
                    .padding(.horizontal, 6.5)
                    .padding(.vertical, 2)
                    .background(
                        (activeInstances.isEmpty ? secondaryInk.opacity(0.14) : accent.opacity(0.14)),
                        in: Capsule(style: .continuous)
                    )

                Spacer()

                Button(action: refreshActiveInstances) {
                    HStack(spacing: 4) {
                        Image(systemName: "arrow.clockwise")
                            .font(.system(size: 10.5, weight: .medium))
                        Text(isRefreshingInstances ? "刷新中" : "刷新")
                            .font(.system(size: 11))
                    }
                    .foregroundStyle(secondaryInk)
                }
                .buttonStyle(.plain)
                .disabled(isRefreshingInstances)
            }

            if !activeInstances.isEmpty {
                VStack(spacing: 7) {
                    ForEach(activeInstances) { instance in
                        instanceRow(instance)
                    }
                }
            }
        }
        .padding(.horizontal, 14)
        .padding(.vertical, 11)
        .background(cardBg, in: RoundedRectangle(cornerRadius: 12, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 12, style: .continuous)
                .strokeBorder(cardBorder, lineWidth: 1)
                .allowsHitTesting(false)
        }
        .shadow(color: Color.black.opacity(isDark ? 0.2 : 0.03), radius: 4, y: 1)
    }

    private func instanceRow(_ instance: ActiveInstance) -> some View {
        let localTime = IsolatedBrowserEngine.formatShortTime(timezoneId: instance.timezone)
        return HStack(spacing: 10) {
            VStack(alignment: .leading, spacing: 3) {
                HStack(spacing: 6) {
                    Circle()
                        .fill(accent)
                        .frame(width: 6, height: 6)
                    Text(instance.regionSummary)
                        .font(.system(size: 11.5, weight: .semibold))
                        .foregroundStyle(primaryInk)
                        .lineLimit(1)
                        .truncationMode(.middle)
                }

                HStack(spacing: 6) {
                    if !localTime.isEmpty {
                        Text(localTime)
                            .font(.system(size: 10.5, design: .monospaced))
                            .foregroundStyle(accent)
                    }
                    Text(instance.proxySummary)
                        .font(.system(size: 10.5, design: .monospaced))
                        .foregroundStyle(secondaryInk)
                        .lineLimit(1)
                        .truncationMode(.middle)
                    if instance.devtoolsPort > 0 {
                        Text(":\(String(instance.devtoolsPort))")
                            .font(.system(size: 10.5, design: .monospaced))
                            .foregroundStyle(secondaryInk)
                    }
                }
            }

            Spacer(minLength: 6)

            closeProfileButton(profileId: instance.profileId, title: "关闭")
        }
        .padding(.horizontal, 10)
        .padding(.vertical, 8)
        .background(fieldBg, in: RoundedRectangle(cornerRadius: 9, style: .continuous))
        .overlay {
            RoundedRectangle(cornerRadius: 9, style: .continuous)
                .strokeBorder(fieldBorder, lineWidth: 1)
                .allowsHitTesting(false)
        }
    }

    private func closeProfileButton(profileId: String, title: String) -> some View {
        let isClosing = closingProfileIds.contains(profileId)
        return Button {
            closeChrome(profileId: profileId)
        } label: {
            HStack(spacing: 4) {
                if isClosing {
                    ProgressView()
                        .controlSize(.small)
                        .scaleEffect(0.65)
                        .frame(width: 11, height: 11)
                } else {
                    Image(systemName: "xmark.circle.fill")
                        .font(.system(size: 11))
                }
                Text(isClosing ? "关闭中" : title)
                    .font(.system(size: 11, weight: .semibold))
            }
            .foregroundStyle(danger)
            .padding(.horizontal, 9)
            .padding(.vertical, 4.5)
            .background(danger.opacity(isDark ? 0.18 : 0.10), in: Capsule(style: .continuous))
            .overlay {
                Capsule(style: .continuous)
                    .strokeBorder(danger.opacity(0.32), lineWidth: 1)
                    .allowsHitTesting(false)
            }
        }
        .buttonStyle(.plain)
        .disabled(isClosing)
    }

    // MARK: - Actions

    private func resolvedTargetUrl() -> String {
        let trimmed = address.trimmingCharacters(in: .whitespacesAndNewlines)
        if trimmed.isEmpty {
            return "about:blank"
        }
        if trimmed.contains("://")
            || trimmed.hasPrefix("about:")
            || trimmed.hasPrefix("chrome:")
            || trimmed.hasPrefix("file:") {
            return trimmed
        }
        return "https://\(trimmed)"
    }

    private func openChrome() {
        guard !isLaunching else { return }

        let targetURL = resolvedTargetUrl()
        let currentMode = proxyMode
        let currentManualProxy = proxyAddress

        if currentMode == .manual {
            do {
                _ = try ProxyMode.normalizeManualProxy(currentManualProxy)
            } catch {
                withAnimation {
                    feedback = .failure(error.localizedDescription)
                }
                return
            }
        }

        withAnimation {
            feedback = nil
            isLaunching = true
        }

        DispatchQueue.global(qos: .userInitiated).async {
            let resultFeedback: LaunchFeedback
            do {
                let outcome = try IsolatedBrowserEngine.shared.launchChrome(
                    targetURL: targetURL,
                    proxyMode: currentMode,
                    manualProxy: currentManualProxy
                )
                resultFeedback = .success(outcome)
            } catch {
                resultFeedback = .failure(error.localizedDescription)
            }

            DispatchQueue.main.async {
                withAnimation(.spring(response: 0.26, dampingFraction: 0.86)) {
                    feedback = resultFeedback
                    isLaunching = false
                }
                refreshActiveInstances()
            }
        }
    }

    private func refreshActiveInstances() {
        guard !isRefreshingInstances else { return }
        isRefreshingInstances = true

        DispatchQueue.global(qos: .utility).async {
            let items = IsolatedBrowserEngine.shared.listActiveInstances()
            DispatchQueue.main.async {
                isRefreshingInstances = false
                withAnimation(.spring(response: 0.24, dampingFraction: 0.86)) {
                    activeInstances = items
                }
            }
        }
    }

    private func closeChrome(profileId: String) {
        guard !closingProfileIds.contains(profileId) else { return }
        closingProfileIds.insert(profileId)

        DispatchQueue.global(qos: .userInitiated).async {
            var closeErr: String?
            do {
                try IsolatedBrowserEngine.shared.closeInstance(profileId: profileId)
            } catch {
                closeErr = error.localizedDescription
            }

            DispatchQueue.main.async {
                closingProfileIds.remove(profileId)
                if let closeErr {
                    withAnimation { feedback = .failure(closeErr) }
                } else if case .success(let outcome) = feedback, outcome.profileId == profileId {
                    withAnimation { feedback = nil }
                }
                refreshActiveInstances()
            }
        }
    }
}

// MARK: - Button Style

private struct PrimaryLaunchButtonStyle: ButtonStyle {
    let accent: Color
    let accentDeep: Color

    @Environment(\.isEnabled) private var isEnabled
    @State private var isHovered = false

    func makeBody(configuration: Configuration) -> some View {
        configuration.label
            .foregroundStyle(.white)
            .background(
                LinearGradient(
                    colors: [accent, accentDeep],
                    startPoint: .top,
                    endPoint: .bottom
                )
                .opacity(isEnabled ? (configuration.isPressed ? 0.85 : (isHovered ? 0.94 : 1.0)) : 0.5)
            )
            .clipShape(RoundedRectangle(cornerRadius: 10, style: .continuous))
            .overlay {
                RoundedRectangle(cornerRadius: 10, style: .continuous)
                    .strokeBorder(Color.white.opacity(0.18), lineWidth: 1)
                    .allowsHitTesting(false)
            }
            .shadow(color: accent.opacity(isEnabled ? 0.25 : 0.0), radius: configuration.isPressed ? 3 : 7, y: configuration.isPressed ? 1 : 2)
            .scaleEffect(configuration.isPressed ? 0.992 : 1.0)
            .animation(.easeOut(duration: 0.12), value: configuration.isPressed)
            .onHover { hovering in
                isHovered = hovering
            }
    }
}

// MARK: - Notifications & AppDelegate

private extension Notification.Name {
    static let focusUrlField = Notification.Name("StableBrowser.focusUrlField")
    static let triggerLaunchChrome = Notification.Name("StableBrowser.triggerLaunchChrome")
}

final class AppDelegate: NSObject, NSApplicationDelegate {
    private var window: NSWindow?
    private var lastAppliedHeight: CGFloat = 0

    func applicationDidFinishLaunching(_ notification: Notification) {
        if let appIcon = Bundle.main.image(forResource: "AppIcon") {
            NSApp.applicationIconImage = appIcon
        }
        installMenu()

        let rootView = LauncherView { [weak self] newHeight in
            self?.updateWindowHeight(newHeight)
        }

        let hosting = NSHostingController(rootView: rootView)
        let window = NSWindow(contentViewController: hosting)
        window.styleMask = [.titled, .closable, .miniaturizable, .fullSizeContentView]
        window.title = "隔离浏览器"
        window.titleVisibility = .hidden
        window.titlebarAppearsTransparent = true
        window.titlebarSeparatorStyle = .none
        window.isMovableByWindowBackground = true
        window.isOpaque = true
        window.backgroundColor = NSColor(name: nil) { appearance in
            let isDark = appearance.bestMatch(from: [.darkAqua, .aqua]) == .darkAqua
            return isDark
                ? NSColor(calibratedRed: 0.082, green: 0.098, blue: 0.094, alpha: 1.0)
                : NSColor(calibratedRed: 0.957, green: 0.965, blue: 0.961, alpha: 1.0)
        }
        window.setContentSize(NSSize(width: LauncherView.windowWidth, height: 420))
        window.center()
        window.makeKeyAndOrderFront(nil)

        self.window = window
        NSApp.activate(ignoringOtherApps: true)
    }

    private func updateWindowHeight(_ contentHeight: CGFloat) {
        guard let window, contentHeight > 120 else { return }
        let rounded = ceil(contentHeight)
        guard abs(rounded - lastAppliedHeight) > 0.5 else { return }
        let isInitial = lastAppliedHeight == 0
        lastAppliedHeight = rounded

        let currentFrame = window.frame
        let targetFrame = window.frameRect(forContentRect: NSRect(x: 0, y: 0, width: LauncherView.windowWidth, height: rounded))
        let newOriginY = currentFrame.maxY - targetFrame.height
        let nextFrame = NSRect(x: currentFrame.origin.x, y: newOriginY, width: targetFrame.width, height: targetFrame.height)

        if isInitial {
            window.setFrame(nextFrame, display: true)
        } else {
            NSAnimationContext.runAnimationGroup { ctx in
                ctx.duration = 0.16
                ctx.timingFunction = CAMediaTimingFunction(name: .easeInEaseOut)
                window.animator().setFrame(nextFrame, display: true)
            }
        }
    }

    func applicationShouldTerminateAfterLastWindowClosed(_ sender: NSApplication) -> Bool {
        true
    }

    @objc private func focusUrl(_ sender: Any?) {
        NotificationCenter.default.post(name: .focusUrlField, object: nil)
    }

    @objc private func launchChromeMenu(_ sender: Any?) {
        NotificationCenter.default.post(name: .triggerLaunchChrome, object: nil)
    }

    private func installMenu() {
        let bar = NSMenu()

        let appItem = NSMenuItem()
        let appMenu = NSMenu()
        appMenu.addItem(withTitle: "退出隔离浏览器", action: #selector(NSApplication.terminate(_:)), keyEquivalent: "q")
        appItem.submenu = appMenu
        bar.addItem(appItem)

        let fileItem = NSMenuItem()
        let fileMenu = NSMenu(title: "操作")
        fileMenu.addItem(withTitle: "打开 Chrome", action: #selector(launchChromeMenu(_:)), keyEquivalent: "r")
        fileMenu.addItem(withTitle: "定位地址栏", action: #selector(focusUrl(_:)), keyEquivalent: "l")
        fileItem.submenu = fileMenu
        bar.addItem(fileItem)

        let editItem = NSMenuItem()
        let editMenu = NSMenu(title: "编辑")
        editMenu.addItem(withTitle: "剪切", action: #selector(NSText.cut(_:)), keyEquivalent: "x")
        editMenu.addItem(withTitle: "复制", action: #selector(NSText.copy(_:)), keyEquivalent: "c")
        editMenu.addItem(withTitle: "粘贴", action: #selector(NSText.paste(_:)), keyEquivalent: "v")
        editMenu.addItem(withTitle: "全选", action: #selector(NSText.selectAll(_:)), keyEquivalent: "a")
        editItem.submenu = editMenu
        bar.addItem(editItem)

        let windowItem = NSMenuItem()
        let windowMenu = NSMenu(title: "窗口")
        windowMenu.addItem(withTitle: "最小化", action: #selector(NSWindow.performMiniaturize(_:)), keyEquivalent: "m")
        windowMenu.addItem(withTitle: "关闭", action: #selector(NSWindow.performClose(_:)), keyEquivalent: "w")
        windowItem.submenu = windowMenu
        bar.addItem(windowItem)

        NSApp.mainMenu = bar
    }
}

let application = NSApplication.shared
let delegate = AppDelegate()
application.delegate = delegate
application.setActivationPolicy(.regular)
application.run()
