// Downloads a GGUF model with progress. Stores under Application Support;
// the same URL is reused on subsequent launches.

import Foundation

public struct CatalogModel: Sendable, Hashable, Identifiable {
    public var id: String { fileName }
    public let displayName: String
    public let fileName: String
    public let url: URL
    public let approximateBytes: Int64

    public init(displayName: String, fileName: String, url: URL, approximateBytes: Int64) {
        self.displayName = displayName
        self.fileName = fileName
        self.url = url
        self.approximateBytes = approximateBytes
    }
}

public enum ModelCatalog {
    // Curated small-and-fast set. The whole point is to ship the demo with
    // models that load instantly and don't cook the phone.
    public static let curated: [CatalogModel] = [
        CatalogModel(
            displayName: "Qwen 2.5 0.5B Instruct (Q4)",
            fileName: "qwen2.5-0.5b-instruct-q4_k_m.gguf",
            url: URL(string: "https://huggingface.co/Qwen/Qwen2.5-0.5B-Instruct-GGUF/resolve/main/qwen2.5-0.5b-instruct-q4_k_m.gguf")!,
            approximateBytes: 400_000_000
        ),
        CatalogModel(
            displayName: "Llama 3.2 1B Instruct (Q4)",
            fileName: "Llama-3.2-1B-Instruct-Q4_K_M.gguf",
            url: URL(string: "https://huggingface.co/bartowski/Llama-3.2-1B-Instruct-GGUF/resolve/main/Llama-3.2-1B-Instruct-Q4_K_M.gguf")!,
            approximateBytes: 800_000_000
        ),
    ]
}

public enum DownloadProgress: Sendable {
    case starting
    case downloading(bytesReceived: Int64, bytesExpected: Int64)
    case completed(URL)
    case failed(Error)
}

public actor ModelDownloader {
    public init() {}

    public static func modelsDirectory() throws -> URL {
        let fm = FileManager.default
        let base = try fm.url(for: .applicationSupportDirectory,
                              in: .userDomainMask,
                              appropriateFor: nil,
                              create: true)
        let dir = base.appendingPathComponent("scry-models", isDirectory: true)
        if !fm.fileExists(atPath: dir.path) {
            try fm.createDirectory(at: dir, withIntermediateDirectories: true)
        }
        return dir
    }

    public static func localURL(for model: CatalogModel) throws -> URL {
        try modelsDirectory().appendingPathComponent(model.fileName)
    }

    public static func isDownloaded(_ model: CatalogModel) -> Bool {
        guard let url = try? localURL(for: model) else { return false }
        return FileManager.default.fileExists(atPath: url.path)
    }

    public func download(_ model: CatalogModel) -> AsyncStream<DownloadProgress> {
        AsyncStream { continuation in
            let task = Task {
                continuation.yield(.starting)
                do {
                    let dest = try Self.localURL(for: model)
                    if FileManager.default.fileExists(atPath: dest.path) {
                        continuation.yield(.completed(dest))
                        continuation.finish()
                        return
                    }

                    let (bytes, response) = try await URLSession.shared.bytes(from: model.url)
                    let expected = response.expectedContentLength > 0
                        ? response.expectedContentLength
                        : model.approximateBytes

                    let tmp = dest.appendingPathExtension("part")
                    FileManager.default.createFile(atPath: tmp.path, contents: nil)
                    let handle = try FileHandle(forWritingTo: tmp)
                    defer { try? handle.close() }

                    var received: Int64 = 0
                    var buffer = Data()
                    buffer.reserveCapacity(1 << 20)

                    for try await byte in bytes {
                        buffer.append(byte)
                        if buffer.count >= (1 << 20) {
                            try handle.write(contentsOf: buffer)
                            received += Int64(buffer.count)
                            buffer.removeAll(keepingCapacity: true)
                            continuation.yield(.downloading(bytesReceived: received,
                                                            bytesExpected: expected))
                        }
                    }
                    if !buffer.isEmpty {
                        try handle.write(contentsOf: buffer)
                        received += Int64(buffer.count)
                    }
                    try handle.close()
                    try FileManager.default.moveItem(at: tmp, to: dest)

                    continuation.yield(.completed(dest))
                    continuation.finish()
                } catch {
                    continuation.yield(.failed(error))
                    continuation.finish()
                }
            }
            continuation.onTermination = { _ in task.cancel() }
        }
    }
}
