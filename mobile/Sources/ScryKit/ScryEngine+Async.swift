// Swift async/await wrapper over the Obj-C ScryEngine. Generation becomes
// an AsyncThrowingStream of TokenEvents — the consumer awaits tokens as
// they're produced, then awaits the final result.

import Foundation
import ScryCore

public enum TokenEvent: Sendable {
    case token(text: String, signal: TokenSignal)
    case finished(GenerationResult)
}

public struct TokenSignal: Sendable, Hashable, Identifiable {
    public let id = UUID()
    public let tokenId: Int32
    public let text: String
    public let confidence: Float
    public let entropy: Float
    public let topGap: Float
    public let topK: [TokenCandidate]
    public let kvPosition: Int32

    init(_ s: ScryTokenSignal) {
        tokenId = Int32(s.tokenId)
        text = s.text
        confidence = s.confidence
        entropy = s.entropy
        topGap = s.topGap
        topK = s.topK.map(TokenCandidate.init)
        kvPosition = Int32(s.kvPosition)
    }
}

public struct TokenCandidate: Sendable, Hashable, Identifiable {
    public var id: Int32 { tokenId }
    public let tokenId: Int32
    public let text: String
    public let probability: Float

    init(_ c: ScryTokenCandidate) {
        tokenId = Int32(c.tokenId)
        text = c.text
        probability = c.probability
    }
}

public struct GenerationResult: Sendable {
    public let content: String
    public let promptTokens: Int
    public let completionTokens: Int
    public let signals: [TokenSignal]
    public let sequenceId: Int32

    // Carried to enable forks. The Obj-C result is what the engine.fork API
    // wants back — wrap it for Swift but keep the original handle.
    let raw: ScryGenerationResult

    init(_ r: ScryGenerationResult) {
        content = r.content
        promptTokens = Int(r.promptTokens)
        completionTokens = Int(r.completionTokens)
        signals = r.signals.map(TokenSignal.init)
        sequenceId = r.sequenceId
        raw = r
    }
}

public struct GenerationOptions: Sendable {
    public var temperature: Float = 0.7
    public var maxTokens: Int = 256
    public var topK: Int = 40
    public var topP: Float = 0.95
    public var minP: Float = 0.05

    public init() {}

    func toObjC() -> ScryGenerationOptions {
        let o = ScryGenerationOptions()
        o.temperature = temperature
        o.maxTokens = Int32(maxTokens)
        o.topK = Int32(topK)
        o.topP = topP
        o.minP = minP
        return o
    }
}

public struct ChatMessage: Sendable, Hashable, Identifiable {
    public enum Role: String, Sendable {
        case system, user, assistant
    }
    public let id = UUID()
    public let role: Role
    public var content: String

    public init(role: Role, content: String) {
        self.role = role
        self.content = content
    }
}

// Actor wrapping the Obj-C engine. All inference goes through here so the
// state (model loaded, vocab positions) is read-safe across the app.
public actor ScryEngineHost {
    private let engine = ScryEngine()

    public init() {}

    public var isLoaded: Bool { engine.isLoaded }
    public var modelName: String? { engine.modelName }
    public var vocabSize: Int { Int(engine.vocabSize) }

    public func loadModel(at url: URL, contextSize: Int = 2048) throws {
        var err: NSError?
        let ok = engine.loadModel(atPath: url.path,
                                   contextSize: Int32(contextSize),
                                   error: &err)
        if !ok {
            throw err ?? NSError(domain: ScryErrorDomain, code: 1)
        }
    }

    public func unload() { engine.unload() }

    public func tokenText(for tokenId: Int32) -> String? {
        engine.text(forTokenId: Int32(tokenId))
    }

    // Returns vocabSize × 6 floats (xyz + rgb). Caller must not mutate.
    // Returns nil before model load completes.
    public func vocabPositions3D() -> UnsafePointer<Float>? {
        engine.vocabPositions3D()
    }

    // Stream a chat generation. Each token arrives as `.token(...)`, the
    // last element is `.finished(GenerationResult)`. Throws on error.
    public func generate(
        messages: [ChatMessage],
        options: GenerationOptions = GenerationOptions()
    ) -> AsyncThrowingStream<TokenEvent, Error> {
        let objcMessages = messages.map {
            ScryMessage(role: $0.role.rawValue, content: $0.content)
        }
        let objcOptions = options.toObjC()
        let engine = self.engine

        return AsyncThrowingStream { continuation in
            engine.generate(withMessages: objcMessages,
                            options: objcOptions,
                            onToken: { text, signal in
                let event = TokenEvent.token(text: text,
                                             signal: TokenSignal(signal))
                continuation.yield(event)
                return true
            }, completion: { result, error in
                if let error = error {
                    continuation.finish(throwing: error)
                    return
                }
                if let result = result {
                    continuation.yield(.finished(GenerationResult(result)))
                }
                continuation.finish()
            })
        }
    }

    // Branch from a previous generation. forkPosition is the kvPosition of
    // the token being replaced; forkTokenId is the alternative.
    public func fork(
        from parent: GenerationResult,
        forkPosition: Int32,
        forkTokenId: Int32,
        options: GenerationOptions = GenerationOptions()
    ) -> AsyncThrowingStream<TokenEvent, Error> {
        let objcOptions = options.toObjC()
        let engine = self.engine
        let parentRaw = parent.raw

        return AsyncThrowingStream { continuation in
            engine.fork(fromResult: parentRaw,
                         forkPosition: Int32(forkPosition),
                         forkTokenId: Int32(forkTokenId),
                         options: objcOptions,
                         onToken: { text, signal in
                let event = TokenEvent.token(text: text,
                                             signal: TokenSignal(signal))
                continuation.yield(event)
                return true
            }, completion: { result, error in
                if let error = error {
                    continuation.finish(throwing: error)
                    return
                }
                if let result = result {
                    continuation.yield(.finished(GenerationResult(result)))
                }
                continuation.finish()
            })
        }
    }
}
